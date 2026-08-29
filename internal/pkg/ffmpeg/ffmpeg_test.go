package ffmpeg

import (
	"reflect"
	"testing"
)

func TestExtractAudioArgsUseSpeechFriendlyCompression(t *testing.T) {
	args := buildExtractAudioArgs("input.mp4", "output.mp3")

	wantContains := []string{"-ac", "1", "-ar", "16000", "-b:a", "32k"}
	for _, want := range wantContains {
		if !contains(args, want) {
			t.Fatalf("expected args to contain %q, got %#v", want, args)
		}
	}
	if contains(args, "-q:a") {
		t.Fatalf("did not expect high-quality VBR option in ASR extraction args: %#v", args)
	}
}

func TestBuildSplitAudioArgsCreatesBoundedSegments(t *testing.T) {
	args := buildSplitAudioArgs("input.mp3", "chunks%03d.mp3", 300)

	want := []string{
		"-y",
		"-i", "input.mp3",
		"-f", "segment",
		"-segment_time", "300",
		"-reset_timestamps", "1",
		"-acodec", "copy",
		"chunks%03d.mp3",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected split args:\nwant: %#v\ngot:  %#v", want, args)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
