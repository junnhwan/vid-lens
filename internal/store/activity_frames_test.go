package store

import (
	"fmt"
	"testing"
	"vidlens/internal/domain"
)

func TestActivitySummaryRetainsFramesOutsideRecentWindow(t *testing.T) {
	frame := domain.Frame{ID: "frame-1", TimeMS: 234067, Availability: "ready"}
	steps := []domain.Step{{ID: "pixels", Frames: []domain.Frame{frame}}}
	for i := 0; i < 12; i++ {
		steps = append(steps, domain.Step{ID: fmt.Sprint(i), Kind: "model_response"})
	}
	summary := activitySummary(steps, "available", nil, "/request", false)
	frames, ok := summary["frames"].([]domain.Frame)
	if !ok || len(frames) != 1 || frames[0].ID != frame.ID {
		t.Fatalf("refresh lost earlier screenshot: %v", summary["frames"])
	}
	if len(summary["recent_steps"].([]domain.Step)) != 8 {
		t.Fatal("frame retention expanded the activity window")
	}
}
