package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
)

type ExportClip struct {
	ID           string       `json:"id"`
	MetadataHash string       `json:"metadataHash"`
	Index        SegmentIndex `json:"index"`
}
type ExportGap struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type ExportPlan struct {
	Requestor       string       `json:"requestor"`
	CreatedAt       time.Time    `json:"createdAt"`
	DisplayTimezone string       `json:"displayTimezone"`
	ID              string       `json:"id"`
	Tenant          string       `json:"tenant"`
	Camera          string       `json:"camera"`
	Start           time.Time    `json:"start"`
	End             time.Time    `json:"end"`
	Width           int          `json:"width"`
	Height          int          `json:"height"`
	FPS             int          `json:"fps"`
	Clips           []ExportClip `json:"clips"`
	Gaps            []ExportGap  `json:"gaps"`
}

func (p ExportPlan) Frames() int64 {
	return int64(math.Round(p.End.Sub(p.Start).Seconds() * float64(p.FPS)))
}
func (p ExportPlan) ValidateEnvelope() error {
	if p.Start.IsZero() || !p.End.After(p.Start) || p.End.Sub(p.Start) > 24*time.Hour || p.FPS < 1 || p.FPS > 120 || p.Width < 2 || p.Height < 2 || p.Width%2 != 0 || p.Height%2 != 0 || p.Width*p.Height > 8192*4320 || len(p.Clips) == 0 || len(p.Clips) > 10000 || p.Frames() < 1 {
		return errors.New("invalid export plan")
	}
	var previous time.Time
	for _, clip := range p.Clips {
		if !clip.Index.End.After(clip.Index.Start) || (!previous.IsZero() && clip.Index.Start.Before(previous)) {
			return errors.New("ambiguous export intervals")
		}
		previous = clip.Index.End
	}
	return nil
}
func (p ExportPlan) Validate() error {
	if err := p.ValidateEnvelope(); err != nil {
		return err
	}
	for _, clip := range p.Clips {
		if len(clip.Index.Parts) == 0 {
			return errors.New("missing archive frame index")
		}
		for _, part := range clip.Index.Parts {
			if part.Timescale != 90000 || part.Frames != len(part.Durations) || part.Frames == 0 {
				return errors.New("invalid archive frame index")
			}
		}
	}
	return nil
}

// CanonicalExportInput remuxes the bounded GOPs into one continuous MP4 stream.
// It supports existing per-GOP decode clocks without copying video to a temp
// file or granting FFmpeg access to the archive directory.
func CanonicalExportInput(ctx context.Context, source io.ReadSeekCloser, index SegmentIndex) io.ReadCloser {
	r, w := io.Pipe()
	go func() {
		defer source.Close()
		stop := context.AfterFunc(ctx, func() { source.Close(); w.CloseWithError(ctx.Err()) })
		defer stop()
		fail := func(err error) { w.CloseWithError(err) }
		if index.InitLength < 1 || index.InitLength > 64<<10 {
			fail(errors.New("invalid initialization"))
			return
		}
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			fail(err)
			return
		}
		if _, err := io.CopyN(w, source, index.InitLength); err != nil {
			fail(err)
			return
		}
		var ticks uint64
		for n, part := range index.Parts {
			if ctx.Err() != nil {
				fail(ctx.Err())
				return
			}
			if part.Length < 1 || part.Length > 16<<20 {
				fail(errors.New("invalid archive part"))
				return
			}
			if _, err := source.Seek(part.Offset, io.SeekStart); err != nil {
				fail(err)
				return
			}
			raw := make([]byte, part.Length)
			if _, err := io.ReadFull(source, raw); err != nil {
				fail(err)
				return
			}
			var parsed fmp4.Parts
			if err := parsed.Unmarshal(raw); err != nil || len(parsed) != 1 || len(parsed[0].Tracks) != 1 {
				fail(errors.New("invalid movie fragment"))
				return
			}
			if len(parsed[0].Tracks[0].Samples) != part.Frames {
				fail(errors.New("archive frame count changed"))
				return
			}
			parsed[0].SequenceNumber = uint32(n + 1)
			parsed[0].Tracks[0].BaseTime = ticks
			for i, sample := range parsed[0].Tracks[0].Samples {
				if sample.Duration != part.Durations[i] {
					fail(errors.New("archive duration changed"))
					return
				}
				ticks += uint64(sample.Duration)
			}
			buf := &boundedSeeker{limit: 16 << 20}
			if err := parsed[0].Marshal(buf); err != nil {
				fail(err)
				return
			}
			if _, err := w.Write(buf.data); err != nil {
				fail(err)
				return
			}
		}
		w.Close()
	}()
	return r
}

type cancelOnErrorWriter struct {
	io.Writer
	cancel context.CancelFunc
	failed chan error
}

func (w cancelOnErrorWriter) Write(p []byte) (int, error) {
	n, e := w.Writer.Write(p)
	if e != nil {
		select {
		case w.failed <- e:
		default:
		}
		w.cancel()
	}
	return n, e
}

// RenderExport samples the original frame intervals on an explicit output
// clock. Only planned absent time becomes black. Decode errors or short input
// fail the artifact; they are never silently replaced by invented frames.
func RenderExport(parent context.Context, ffmpeg string, plan ExportPlan, open func(context.Context, ExportClip) (io.ReadCloser, error), output io.Writer, progress func(int64)) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	encoder := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-f", "rawvideo", "-pixel_format", "yuv420p", "-video_size", fmt.Sprintf("%dx%d", plan.Width, plan.Height), "-framerate", fmt.Sprint(plan.FPS), "-i", "pipe:0", "-an", "-c:v", "libx264", "-threads", "2", "-preset", "veryfast", "-crf", "18", "-bf", "0", "-g", fmt.Sprint(plan.FPS*2), "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
	input, err := encoder.StdinPipe()
	if err != nil {
		return err
	}
	outputError := make(chan error, 1)
	encoder.Stdout = cancelOnErrorWriter{Writer: output, cancel: cancel, failed: outputError}
	encoder.Stderr = &boundedBuffer{limit: 64 << 10}
	process, err := StartOwned(encoder)
	if err != nil {
		input.Close()
		return errors.New("export encoder unavailable")
	}
	defer process.Stop()
	total := plan.Frames()
	size := plan.Width * plan.Height * 3 / 2
	black := make([]byte, size)
	for i := range black {
		if i < plan.Width*plan.Height {
			black[i] = 16
		} else {
			black[i] = 128
		}
	}
	var written int64
	instant := func() time.Time {
		return plan.Start.Add(time.Duration(written) * time.Second / time.Duration(plan.FPS))
	}
	write := func(frame []byte) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, e := input.Write(frame); e != nil {
			return e
		}
		written++
		if progress != nil && (written%int64(plan.FPS) == 0 || written == total) {
			progress(written)
		}
		return nil
	}
	render := func() error {
		for _, clip := range plan.Clips {
			for written < total && instant().Before(clip.Index.Start) {
				if e := write(black); e != nil {
					return e
				}
			}
			source, e := open(ctx, clip)
			if e != nil {
				return e
			}
			decodeErr := func() error {
				defer source.Close()
				decoder := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-xerror", "-nostdin", "-threads", "2", "-protocol_whitelist", "pipe", "-f", "mp4", "-i", "pipe:0", "-map", "0:v:0", "-an", "-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black,format=yuv420p", plan.Width, plan.Height, plan.Width, plan.Height), "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
				decoder.Stdin = source
				decoder.Stderr = &boundedBuffer{limit: 64 << 10}
				raw, e := decoder.StdoutPipe()
				if e != nil {
					return e
				}
				child, e := StartOwned(decoder)
				if e != nil {
					return errors.New("export decoder unavailable")
				}
				defer child.Stop()
				stop := context.AfterFunc(ctx, func() { source.Close(); child.Stop() })
				defer stop()
				frame := make([]byte, size)
				consume := func() error {
					for _, part := range clip.Index.Parts {
						var ticks int64
						durations := part.Durations
						if len(part.PresentationDurations) == part.Frames {
							durations = part.PresentationDurations
						}
						for _, duration := range durations {
							if _, e := io.ReadFull(raw, frame); e != nil {
								return errors.New("source frames unavailable")
							}
							at := part.Start.Add(rtpDuration(ticks))
							ticks += int64(duration)
							until := part.Start.Add(rtpDuration(ticks))
							for written < total && instant().Before(until) {
								if instant().Before(at.Add(-time.Microsecond)) {
									return errors.New("unexpected source gap")
								}
								if e := write(frame); e != nil {
									return e
								}
							}
						}
					}
					var extra [1]byte
					if n, e := raw.Read(extra[:]); n != 0 || e != io.EOF {
						return errors.New("source frame index mismatch")
					}
					return nil
				}
				e = consume()
				if e != nil {
					child.Stop()
					source.Close()
				}
				waitErr := decoder.Wait()
				if e != nil {
					return e
				}
				if waitErr != nil {
					return errors.New("source decode failed")
				}
				return nil
			}()
			if decodeErr != nil {
				return decodeErr
			}
		}
		for written < total {
			if e := write(black); e != nil {
				return e
			}
		}
		return nil
	}
	err = render()
	input.Close()
	if err != nil {
		cancel()
		process.Stop()
	}
	waitErr := encoder.Wait()
	select {
	case e := <-outputError:
		return e
	default:
	}
	if err != nil {
		return err
	}
	if waitErr != nil {
		return errors.New("export encoding failed")
	}
	return nil
}
