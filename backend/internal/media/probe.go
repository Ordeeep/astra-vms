package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ProbeResult struct {
	Codec         string  `json:"codec"`
	Profile       string  `json:"profile"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	FPS           float64 `json:"fps"`
	BFrames       int     `json:"bFrames"`
	H264Candidate bool    `json:"h264Candidate"`
}
type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("diagnostic output limit exceeded")
	}
	return b.Buffer.Write(p)
}
func Probe(ctx context.Context, source string) (ProbeResult, error) {
	u, err := url.Parse(source)
	if err != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Host == "" {
		return ProbeResult{}, errors.New("invalid RTSP source")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-rtsp_transport", "tcp", "-rw_timeout", "5000000", "-i", source, "-select_streams", "v:0", "-show_entries", "stream=codec_name,profile,width,height,avg_frame_rate,has_b_frames", "-of", "json")
	out := &boundedBuffer{limit: 1 << 20}
	cmd.Stdout = out
	p, err := StartOwned(cmd)
	if err != nil {
		return ProbeResult{}, errors.New("probe process unavailable")
	}
	defer p.Stop()
	if err = cmd.Wait(); err != nil {
		return ProbeResult{}, errors.New("camera connection or video probe failed")
	}
	var data struct {
		Streams []struct {
			Codec         string `json:"codec_name"`
			Profile       string `json:"profile"`
			Width, Height int
			Rate          string `json:"avg_frame_rate"`
			BFrames       int    `json:"has_b_frames"`
		}
	}
	if json.Unmarshal(out.Bytes(), &data) != nil || len(data.Streams) != 1 {
		return ProbeResult{}, errors.New("no video stream")
	}
	s := data.Streams[0]
	fps := 0.0
	ratio := strings.Split(s.Rate, "/")
	if len(ratio) == 2 {
		n, _ := strconv.ParseFloat(ratio[0], 64)
		d, _ := strconv.ParseFloat(ratio[1], 64)
		if d > 0 {
			fps = n / d
		}
	}
	return ProbeResult{Codec: s.Codec, Profile: s.Profile, Width: s.Width, Height: s.Height, FPS: fps, BFrames: s.BFrames, H264Candidate: s.Codec == "h264" && s.BFrames == 0 && (s.Profile == "Constrained Baseline" || s.Profile == "Baseline" || s.Profile == "Main" || s.Profile == "High")}, nil
}
