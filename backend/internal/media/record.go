package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os/exec"
	"time"

	"vms.local/backend/internal/storage"
)

type Fragment struct {
	ID       string
	Sequence int
	Bytes    int
}

// RecordFragments consumes FFmpeg's fragmented MP4 stdout, never a filename
// owned by FFmpeg. Each keyframe fragment receives its initialization boxes.
// The bounded buffer is measured before capacity is admitted by the writer.
func RecordFragments(ctx context.Context, input io.Reader, store *storage.Store, tenant string, maxFragment int, closed func(Fragment)) error {
	if maxFragment < 4096 || maxFragment > 32<<20 {
		return errors.New("invalid fragment buffer limit")
	}
	var initialization, fragment []byte
	haveMoov := false
	sequence := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var header [8]byte
		_, err := io.ReadFull(input, header[:])
		if errors.Is(err, io.EOF) && len(fragment) == 0 {
			return nil
		}
		if err != nil {
			return err
		}
		size := int(binary.BigEndian.Uint32(header[:4]))
		kind := string(header[4:])
		if size < 8 || size > maxFragment {
			return errors.New("invalid or oversized MP4 box")
		}
		box := make([]byte, size)
		copy(box, header[:])
		if _, err := io.ReadFull(input, box[8:]); err != nil {
			return err
		}
		switch kind {
		case "ftyp", "moov":
			if len(fragment) > 0 || sequence > 0 || len(initialization)+len(box) > maxFragment {
				return errors.New("invalid MP4 initialization")
			}
			initialization = append(initialization, box...)
			if kind == "moov" {
				haveMoov = true
			}
		case "moof":
			if !haveMoov || len(fragment) > 0 {
				return errors.New("unexpected movie fragment")
			}
			fragment = box
		case "mdat":
			if len(fragment) == 0 || len(initialization)+len(fragment)+len(box) > maxFragment {
				return errors.New("missing or oversized movie fragment")
			}
			capacity := len(initialization) + len(fragment) + len(box)
			// Correctness baseline for the PoC: decode every fragment before it is
			// admitted or advertised. Performance of this pipeline is not P1-qualified.
			if err := validateVideo(ctx, initialization, fragment, box); err != nil {
				return err
			}
			writer, err := store.CreateCapture(tenant, int64(capacity))
			if err != nil {
				return err
			}
			for _, part := range [][]byte{initialization, fragment, box} {
				if _, err := writer.Write(part); err != nil {
					return err
				}
			}
			if err := writer.Finish(); err != nil {
				return err
			}
			sequence++
			if closed != nil {
				closed(Fragment{ID: writer.ID(), Sequence: sequence, Bytes: capacity})
			}
			fragment = nil
		case "mfra", "free":
			// End index and padding are not needed to decode a self-contained fragment.
			if len(fragment) > 0 {
				return errors.New("incomplete MP4 fragment")
			}
		default:
			return errors.New("unsupported MP4 box: " + kind)
		}
	}
}

func validateVideo(ctx context.Context, parts ...[]byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	readers := make([]io.Reader, 0, len(parts))
	for _, part := range parts {
		readers = append(readers, bytes.NewReader(part))
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-xerror", "-threads", "1", "-i", "pipe:0", "-map", "0:v:0", "-f", "null", "-")
	cmd.Stdin = io.MultiReader(readers...)
	// No unbounded stderr collection for malformed camera input.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("fragment failed video validation")
	}
	return nil
}
