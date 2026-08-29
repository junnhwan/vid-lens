package ffmpeg

import (
	"reflect"
	"testing"
)

func TestMergeKeyFramesPrefersSceneAndDropsNearDuplicates(t *testing.T) {
	scene := []KeyFrame{
		{Path: "s1", TimeMs: 0, Source: "scene"},
		{Path: "s2", TimeMs: 30_000, Source: "scene"},
	}
	interval := []KeyFrame{
		{Path: "i1", TimeMs: 500, Source: "interval"}, // near s1
		{Path: "i2", TimeMs: 30_000, Source: "interval"},
		{Path: "i3", TimeMs: 60_000, Source: "interval"},
	}
	got := mergeKeyFrames(scene, interval, 10)
	wantTimes := []int64{0, 30_000, 60_000}
	if len(got) != len(wantTimes) {
		t.Fatalf("len=%d got=%#v", len(got), got)
	}
	for i, want := range wantTimes {
		if got[i].TimeMs != want {
			t.Fatalf("frame[%d].TimeMs=%d want %d", i, got[i].TimeMs, want)
		}
	}
	if got[0].Source != "scene" || got[1].Source != "scene" {
		t.Fatalf("expected scene preference, got %#v", got)
	}
	if got[2].Source != "interval" {
		t.Fatalf("expected interval filler, got %#v", got[2])
	}
}

func TestMergeKeyFramesRespectsMaxFrames(t *testing.T) {
	var frames []KeyFrame
	for i := 0; i < 20; i++ {
		frames = append(frames, KeyFrame{Path: "x", TimeMs: int64(i * 5000), Source: "interval"})
	}
	got := mergeKeyFrames(nil, frames, 5)
	if len(got) != 5 {
		t.Fatalf("len=%d want 5", len(got))
	}
}

func TestParseShowinfoPTS(t *testing.T) {
	log := `
[Parsed_showinfo_1 @ 0x1] n:   0 pts:  123 pts_time:1.230 pos:  456
[Parsed_showinfo_1 @ 0x1] n:   1 pts:  456 pts_time:30.000 pos:  789
`
	got := parseShowinfoPTS(log)
	want := []int64{1230, 30_000}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pts=%v want %v", got, want)
	}
}
