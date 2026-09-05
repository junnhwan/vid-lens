package ffmpeg

import (
	"context"
	"strings"
	"testing"
)

func TestExtractFramesAtTimesRejectsUnboundedInputBeforeRunningFFmpeg(t *testing.T) {
	_, _, err := ExtractFramesAtTimes(context.Background(), "", "", []int64{1000})
	if err == nil || !strings.Contains(err.Error(), "input is empty") {
		t.Fatalf("error = %v, want empty input validation", err)
	}
	_, _, err = ExtractFramesAtTimes(context.Background(), "", "video.mp4", nil)
	if err == nil || !strings.Contains(err.Error(), "timestamps are empty") {
		t.Fatalf("error = %v, want empty timestamp validation", err)
	}
}
