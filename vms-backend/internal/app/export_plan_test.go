package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
	"vms.local/backend/internal/media"
)

func TestFullDayExportControlPlanFitsBoundedRequest(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	segments := []archiveSegment{}
	for i := range 2880 {
		at := start.Add(time.Duration(i) * 30 * time.Second)
		durations := make([]uint32, 450)
		for n := range durations {
			durations[n] = 6000
		}
		index := media.SegmentIndex{Tenant: "tenant", Camera: "camera", Site: "site", Source: "source", Profile: "main", Start: at, End: at.Add(30 * time.Second), Parts: []media.PartIndex{{ArchivePart: media.ArchivePart{Width: 1920, Height: 1080, Timescale: 90000, Frames: 450, Durations: durations}}}}
		segments = append(segments, archiveSegment{ID: fmt.Sprintf("segment-%d", i), Index: index})
	}
	plan, err := buildExportPlan("tenant", "camera", start, start.Add(24*time.Hour), segments)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil || len(raw) > 4<<20 {
		t.Fatalf("control plan too large: %d %v", len(raw), err)
	}
	if len(plan.Clips) != 2880 || plan.Frames() != 1296000 {
		t.Fatal("full-day coverage lost")
	}
	for i, clip := range plan.Clips {
		if len(clip.Index.Parts) != 0 || clip.MetadataHash != media.IndexHash(segments[i].Index) {
			t.Fatal("immutable source identity lost")
		}
	}
}

func TestExportPlanUsesPresentationClockForReorderedFrames(t *testing.T) {
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	end := start.Add(200 * time.Millisecond)
	index := media.SegmentIndex{Tenant: "tenant", Camera: "camera", Site: "site", Start: start, End: end, Parts: []media.PartIndex{{ArchivePart: media.ArchivePart{Codec: "hvc1.1.6.L93.B0", Width: 640, Height: 360, Timescale: 90000, Frames: 3, Durations: []uint32{1, 3000, 14999}, PresentationDurations: []uint32{6000, 6000, 6000}}}}}
	plan, err := buildExportPlan("tenant", "camera", start, end, []archiveSegment{{ID: "segment", Index: index}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.FPS != 15 || plan.Frames() != 3 {
		t.Fatalf("presentation timing lost: fps=%d frames=%d", plan.FPS, plan.Frames())
	}
}
