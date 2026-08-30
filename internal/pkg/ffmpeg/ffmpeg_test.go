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

func TestPlanAudioSegmentWindowsAddsBoundaryContextWithoutOverlappingCores(t *testing.T) {
	windows := planAudioSegmentWindows(650_000, 300_000, 5_000)
	want := []audioSegmentWindow{
		{WindowStartMS: 0, WindowEndMS: 305_000, CoreStartMS: 0, CoreEndMS: 300_000},
		{WindowStartMS: 295_000, WindowEndMS: 605_000, CoreStartMS: 300_000, CoreEndMS: 600_000},
		{WindowStartMS: 595_000, WindowEndMS: 650_000, CoreStartMS: 600_000, CoreEndMS: 650_000},
	}
	if !reflect.DeepEqual(windows, want) {
		t.Fatalf("windows:\nwant: %#v\ngot:  %#v", want, windows)
	}
}

func TestPlanAudioSegmentWindowsRejectsUnsafeOverlap(t *testing.T) {
	if got := planAudioSegmentWindows(600_000, 300_000, 150_000); got != nil {
		t.Fatalf("windows = %#v, want nil", got)
	}
}

func TestBuildExtractAudioWindowArgsReencodesSpeechAudio(t *testing.T) {
	args := buildExtractAudioWindowArgs("input.mp3", "output.mp3", 295_000, 605_000)
	want := []string{
		"-y", "-ss", "295.000", "-i", "input.mp3", "-t", "310.000",
		"-vn", "-ac", "1", "-ar", "16000", "-acodec", "libmp3lame", "-b:a", "32k", "output.mp3",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args:\nwant: %#v\ngot:  %#v", want, args)
	}
}

func TestCompanionFFprobePathUsesFFmpegDirectoryAndExtension(t *testing.T) {
	if got := companionFFprobePath(`D:\tools\ffmpeg\bin\ffmpeg.exe`); got != `D:\tools\ffmpeg\bin\ffprobe.exe` {
		t.Fatalf("companionFFprobePath() = %q", got)
	}
	if got := companionFFprobePath("ffmpeg"); got != "ffprobe" {
		t.Fatalf("companionFFprobePath(ffmpeg) = %q", got)
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
