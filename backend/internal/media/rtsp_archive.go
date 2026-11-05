package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/pion/rtp"
	"sort"
)

// ArchivePart is one independently decodable GOP. UTC is correlated with the
// server arrival clock; camera RTCP time is recorded separately, never trusted
// to overwrite the archive clock. All intervals are half-open.
type ArchivePart struct {
	Start                 time.Time  `json:"start"`
	End                   time.Time  `json:"end"`
	SourceNTP             *time.Time `json:"sourceNtp,omitempty"`
	TimeBasis             string     `json:"timeBasis"`
	Codec                 string     `json:"codec"`
	Width                 int        `json:"width"`
	Height                int        `json:"height"`
	Frames                int        `json:"frames"`
	Timescale             uint32     `json:"timescale"`
	Durations             []uint32   `json:"durations"`
	PresentationDurations []uint32   `json:"presentationDurations,omitempty"`
	Init                  []byte     `json:"-"`
	Data                  []byte     `json:"-"`
}

// boundedSeeker accommodates the MP4 library's header backpatching without
// exposing a disk handle or allowing an unbounded camera-controlled allocation.
type boundedSeeker struct {
	data  []byte
	pos   int64
	limit int64
}

func rtpDuration(ticks int64) time.Duration {
	return time.Duration(ticks/90000)*time.Second + time.Duration(ticks%90000)*time.Second/90000
}

func (b *boundedSeeker) Write(p []byte) (int, error) {
	end := b.pos + int64(len(p))
	if end < b.pos || end > b.limit {
		return 0, errors.New("media buffer limit")
	}
	if end > int64(len(b.data)) {
		b.data = append(b.data, make([]byte, int(end)-len(b.data))...)
	}
	copy(b.data[b.pos:end], p)
	b.pos = end
	return len(p), nil
}
func (b *boundedSeeker) Seek(n int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		n += b.pos
	case io.SeekEnd:
		n += int64(len(b.data))
	case io.SeekStart:
	default:
		return 0, errors.New("invalid seek")
	}
	if n < 0 || n > b.limit {
		return 0, errors.New("media buffer limit")
	}
	b.pos = n
	return n, nil
}

// ReceiveArchive preserves H.264/H.265 access units with independent decode and
// presentation clocks. Missing RTP,
// oversized GOPs and parameter changes terminate the epoch, rather than
// inventing continuity. The caller reconnects and records the diagnosed gap.
func ReceiveArchive(ctx context.Context, address string, emit func(ArchivePart) error, frame func(time.Time)) error {
	u, err := base.ParseURL(address)
	if err != nil {
		return errors.New("invalid RTSP source")
	}
	tcp := gortsplib.ProtocolTCP
	c := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &tcp, ReadTimeout: 4 * time.Second, WriteTimeout: 3 * time.Second}
	failed := make(chan error, 1)
	fail := func(e error) {
		select {
		case failed <- e:
		default:
		}
	}
	c.OnPacketsLost = func(n uint64) { fail(errors.New("rtp_packet_loss")) }
	c.OnDecodeError = func(error) { fail(errors.New("rtp_decode_error")) }
	if err = c.Start(); err != nil {
		return errors.New("rtsp_connect_failed")
	}
	defer c.Close()
	stopClose := context.AfterFunc(ctx, c.Close)
	defer stopClose()
	desc, _, err := c.Describe(u)
	if err != nil {
		return errors.New("rtsp_describe_failed")
	}
	var f264 *format.H264
	var f265 *format.H265
	medi := desc.FindFormat(&f264)
	var selected format.Format
	var decode func(*rtp.Packet) ([][]byte, error)
	var extract func([][]byte, int64) (int64, error)
	var vps, sps, pps []byte
	hevc := false
	if medi != nil {
		selected = f264
		d, e := f264.CreateDecoder()
		if e != nil {
			return errors.New("rtp_decoder_failed")
		}
		decode = d.Decode
		x := &h264.DTSExtractor{}
		x.Initialize()
		extract = x.Extract
		sps, pps = bytes.Clone(f264.SPS), bytes.Clone(f264.PPS)
	} else {
		medi = desc.FindFormat(&f265)
		if medi == nil {
			return errors.New("h264_or_h265_required")
		}
		hevc = true
		selected = f265
		d, e := f265.CreateDecoder()
		if e != nil {
			return errors.New("rtp_decoder_failed")
		}
		decode = d.Decode
		x := &h265.DTSExtractor{}
		x.Initialize()
		extract = x.Extract
		vps, sps, pps = bytes.Clone(f265.VPS), bytes.Clone(f265.SPS), bytes.Clone(f265.PPS)
	}
	if _, err = c.Setup(desc.BaseURL, medi, 0, 0); err != nil {
		return errors.New("rtsp_setup_failed")
	}
	var samples []*fmp4.Sample
	var pts0, lastDTS, compositionShift, partPTS, partDTS int64
	var samplePTS []int64
	var epoch, partStart time.Time
	var sourceNTP *time.Time
	var init []byte
	var codec string
	var width, height int
	var size int
	var framesAt atomic.Int64
	framesAt.Store(time.Now().UnixNano())
	flush := func(endPTS, endDTS int64) error {
		if len(samples) == 0 {
			return nil
		}
		if endDTS <= lastDTS || endDTS-lastDTS > 180000 {
			return errors.New("rtp_timestamp_discontinuity")
		}
		samples[len(samples)-1].Duration = uint32(endDTS - lastDTS)
		buf := &boundedSeeker{limit: 16 << 20}
		part := fmp4.Part{SequenceNumber: 1, Tracks: []*fmp4.PartTrack{{ID: 1, BaseTime: 0, Samples: samples}}}
		if err := part.Marshal(buf); err != nil {
			return err
		}
		durations := make([]uint32, len(samples))
		for i, s := range samples {
			durations[i] = s.Duration
		}
		display := append([]int64(nil), samplePTS...)
		sort.Slice(display, func(i, j int) bool { return display[i] < display[j] })
		presentation := make([]uint32, len(display))
		for i, p := range display {
			next := endPTS
			if i+1 < len(display) {
				next = display[i+1]
			}
			if next <= p || next-p > 180000 {
				return errors.New("invalid_presentation_clock")
			}
			presentation[i] = uint32(next - p)
		}
		out := ArchivePart{Start: partStart, End: epoch.Add(rtpDuration(endPTS - pts0)), SourceNTP: sourceNTP, TimeBasis: "server-utc/rtp-90000", Codec: codec, Width: width, Height: height, Frames: len(samples), Timescale: 90000, Durations: durations, PresentationDurations: presentation, Init: init, Data: buf.data}
		return emit(out)
	}
	c.OnPacketRTP(medi, selected, func(pkt *rtp.Packet) {
		pts, ok := c.PacketPTS(medi, pkt)
		if !ok {
			return
		}
		au, e := decode(pkt)
		if e != nil {
			if !errors.Is(e, rtph264.ErrMorePacketsNeeded) && !errors.Is(e, rtph264.ErrNonStartingPacketAndNoPrevious) && !errors.Is(e, rtph265.ErrMorePacketsNeeded) && !errors.Is(e, rtph265.ErrNonStartingPacketAndNoPrevious) {
				fail(errors.New("video_packet_decode_failed"))
			}
			return
		}
		now := time.Now()
		framesAt.Store(now.UnixNano())
		if frame != nil && hasVideoPicture(au, hevc) {
			frame(now.UTC())
		}
		key := h264.IsRandomAccess(au)
		if hevc {
			key = h265.IsRandomAccess(au)
		}
		for _, nalu := range au {
			if len(nalu) == 0 {
				continue
			}
			typ := nalu[0] & 31
			if hevc {
				typ = (nalu[0] >> 1) & 63
			}
			var parameter *[]byte
			if !hevc {
				if typ == 7 {
					parameter = &sps
				}
				if typ == 8 {
					parameter = &pps
				}
			} else {
				if typ == 32 {
					parameter = &vps
				}
				if typ == 33 {
					parameter = &sps
				}
				if typ == 34 {
					parameter = &pps
				}
			}
			if parameter != nil {
				if len(init) > 0 && !bytes.Equal(*parameter, nalu) {
					fail(errors.New("video_parameters_changed"))
					return
				}
				*parameter = bytes.Clone(nalu)
			}
		}
		if len(samples) == 0 && !key {
			return
		}
		if len(init) == 0 {
			if len(sps) < 4 || len(pps) == 0 || (hevc && len(vps) == 0) {
				return
			}
			var trackCodec codecs.Codec
			if hevc {
				var info h265.SPS
				if info.Unmarshal(sps) != nil {
					fail(errors.New("invalid_h265_sps"))
					return
				}
				width, height = info.Width(), info.Height()
				trackCodec = &codecs.H265{VPS: vps, SPS: sps, PPS: pps}
				codec = hevcCodecString(sps)
			} else {
				var info h264.SPS
				if info.Unmarshal(sps) != nil {
					fail(errors.New("invalid_h264_sps"))
					return
				}
				width, height = info.Width(), info.Height()
				trackCodec = &codecs.H264{SPS: sps, PPS: pps}
				codec = "avc1." + hex.EncodeToString(sps[1:4])
			}
			buf := &boundedSeeker{limit: 64 << 10}
			if e := (fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: 90000, Codec: trackCodec}}}).Marshal(buf); e != nil {
				fail(e)
				return
			}
			init = buf.data
			pts0 = pts
			epoch = now
		}
		// Seed out-of-band parameters because RTP does not have to repeat them.
		withParams := au
		if len(samples) == 0 {
			withParams = append([][]byte{sps, pps}, au...)
			if hevc {
				withParams = append([][]byte{vps, sps, pps}, au...)
			}
		}
		dts, e := extract(withParams, pts)
		if e != nil {
			fail(errors.New("video_decode_clock_invalid"))
			return
		}
		if len(samples) == 0 && samplePTS == nil {
			compositionShift = pts - dts
		}
		if len(samples) > 0 {
			if dts <= lastDTS || dts-lastDTS > 180000 {
				fail(errors.New("video_timestamp_discontinuity"))
				return
			}
			if key {
				if e := flush(pts, dts); e != nil {
					fail(e)
					return
				}
				samples = nil
				samplePTS = samplePTS[:0]
				size = 0
			} else {
				samples[len(samples)-1].Duration = uint32(dts - lastDTS)
			}
		}
		if len(samples) == 0 {
			partPTS, partDTS = pts, dts
			partStart = epoch.Add(rtpDuration(pts - pts0))
			sourceNTP = nil
			if ntp, ok := c.PacketNTP(medi, pkt); ok {
				sourceNTP = &ntp
			}
		}
		var sample fmp4.Sample
		if pts < partPTS || pts-dts-compositionShift > 2147483647 || pts-dts-compositionShift < -2147483648 {
			fail(errors.New("unsupported_open_gop_clock"))
			return
		}
		if hevc {
			e = sample.FillH265(int32(pts-dts-compositionShift), au)
		} else {
			e = sample.FillH264(int32(pts-dts-compositionShift), au)
		}
		if e != nil {
			fail(errors.New("invalid_h264_access_unit"))
			return
		}
		size += len(sample.Payload)
		if size > 15<<20 || len(samples) >= 300 || rtpDuration(dts-partDTS) > 10*time.Second {
			fail(errors.New("gop_exceeds_archive_buffer"))
			return
		}
		samples = append(samples, &sample)
		samplePTS = append(samplePTS, pts)
		lastDTS = dts
		// Wall-clock corrections are a new epoch; normal network jitter does not
		// stretch frame durations. Relative server/RTP drift is diagnosed.
		mapped := epoch.Add(rtpDuration(pts - pts0))
		if now.Sub(mapped) > 2*time.Second || mapped.Sub(now) > 2*time.Second {
			fail(errors.New("server_rtp_clock_discontinuity"))
		}
	})
	if _, err = c.Play(nil); err != nil {
		return errors.New("rtsp_play_failed")
	}
	go func() {
		if e := c.Wait(); e != nil {
			fail(errors.New("rtsp_stream_ended"))
		}
	}()
	watch := time.NewTicker(250 * time.Millisecond)
	defer watch.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-failed:
			return fmt.Errorf("capture stopped: %w", err)
		case <-watch.C:
			if time.Since(time.Unix(0, framesAt.Load())) >= 3*time.Second {
				return errors.New("source_frames_stalled")
			}
		}
	}
}

// hevcCodecString describes the actual SPS, including Main10 and level, for
// browser capability checks; the archive itself keeps the unmodified samples.
func hevcCodecString(sps []byte) string {
	if len(sps) < 3 {
		return "hvc1"
	}
	rbsp := h264.EmulationPreventionRemove(sps[2:])
	if len(rbsp) < 13 {
		return "hvc1"
	}
	spaces := []string{"", "A", "B", "C"}
	tier := "L"
	if rbsp[1]&0x20 != 0 {
		tier = "H"
	}
	value := fmt.Sprintf("hvc1.%s%d.%X.%s%d", spaces[rbsp[1]>>6], rbsp[1]&31, bits.Reverse32(binary.BigEndian.Uint32(rbsp[2:6])), tier, rbsp[12])
	constraints := rbsp[6:12]
	for len(constraints) > 0 && constraints[len(constraints)-1] == 0 {
		constraints = constraints[:len(constraints)-1]
	}
	for _, b := range constraints {
		value += "." + strings.ToUpper(fmt.Sprintf("%02x", b))
	}
	return value
}
