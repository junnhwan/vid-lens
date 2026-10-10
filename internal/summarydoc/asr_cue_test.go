package summarydoc

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"vid-lens/internal/textsource"
)

func nativeASRCueFixture(t *testing.T) (Document, ValidationContext) {
	t.Helper()
	const id, text = "asr-window-0:0:17", "Native ASR facts."
	source, err := textsource.Canonicalize(textsource.Snapshot{
		Kind: textsource.KindASR, Identity: textsource.Identity{Platform: "upload", MediaFingerprint: "media-1"}, ParserVersion: "asr-provenance-v1",
		Cues: []textsource.Cue{{ID: id, Order: 1, RawText: text, Text: text, StartMS: ptr[int64](120000), EndMS: ptr[int64](135000), TimingMethod: "asr_native", JoinBefore: ptr(""),
			RawRefs: []textsource.RawCueRef{{ID: id, Order: 1, RawText: text, ObservationID: "asr-window-0", ObservationOrder: 1, TextStart: ptr(0), TextEnd: ptr(17), StartMS: ptr[int64](120000), EndMS: ptr[int64](140000), TimingMethod: "asr_window",
				NativeTimings: []textsource.NativeTiming{{SegmentIndex: 0, TextStart: 0, TextEnd: 17, StartMS: 120000, EndMS: 135000, Method: "asr_native"}}}}}},
	}, textsource.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	doc, ctx := fixture()
	doc.SourceDigest, ctx.SourceDigest = source.SourceDigest, source.SourceDigest
	cue := source.Cues[0]
	doc.Blocks[0].SourceRefs = []SourceRef{{SourceID: doc.SourceID, CueIDs: []string{cue.ID}, StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod}}
	ctx.Cues = map[string]Cue{cue.ID: {StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod}}
	return doc, ctx
}

func TestNativeASRProvenanceCueParseValidateProjectionAndPatch(t *testing.T) {
	doc, ctx := nativeASRCueFixture(t)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = Validate(parsed, ctx); err != nil {
		t.Fatal(err)
	}
	markdown, err := Markdown(parsed)
	if err != nil || !strings.Contains(markdown, "02:00.000–02:15.000（asr_native）") {
		t.Fatalf("native timing projection: %q, %v", markdown, err)
	}
	digest, err := Digest(parsed)
	if err != nil {
		t.Fatal(err)
	}
	patch := Patch{BaseContentHashKind: HashKind, BaseContentHash: digest, Operations: []Operation{{Op: OpUpdateBlock, BlockID: "pool", BodyMarkdown: ptr("最大连接数调整为 30。")}}}
	patchJSON, _ := json.Marshal(patch)
	patch, err = ParsePatch(patchJSON)
	if err != nil {
		t.Fatal(err)
	}
	next, preview, err := ApplyPatch(parsed, patch, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed.Blocks[0].SourceRefs, next.Blocks[0].SourceRefs) || next.Blocks[0].ID != "pool" || preview.ContentDigest == digest || !strings.Contains(preview.Markdown, "30") || !strings.Contains(preview.Markdown, "02:00.000–02:15.000（asr_native）") {
		t.Fatal("edit changed frozen provenance or produced inconsistent projection/digest")
	}
	for _, raw := range []string{
		strings.Replace(string(data), `"cue_ids":`, `"extra":true,"cue_ids":`, 1),
		strings.Replace(string(data), `"cue_ids":["asr-window-0:0:17"]`, `"cue_ids":["asr-window-0:0:17"],"cue_ids":["asr-window-0:0:17"]`, 1),
		strings.Replace(string(data), `"cue_ids":["asr-window-0:0:17"]`, `"cue_ids":["asr-window-0:0:17","asr-window-0:0:17"]`, 1),
	} {
		if _, err = Parse([]byte(raw)); err == nil {
			t.Fatal("accepted unknown field, duplicate key, or duplicate ASR cue")
		}
	}
}

func TestOpaqueCueSyntaxDoesNotBroadenBlockOrFigureIDsOrAuthorization(t *testing.T) {
	for _, id := range []string{"", "asr window", "asr\nwindow", "asr\x00window", "asr\u00a0window", strings.Repeat("a", 129), strings.Repeat("界", 43)} {
		doc, _ := nativeASRCueFixture(t)
		doc.Blocks[0].SourceRefs[0].CueIDs = []string{id}
		data, _ := json.Marshal(doc)
		if _, err := Parse(data); err == nil {
			t.Fatalf("accepted malformed or oversized cue %q", id)
		}
	}
	doc, ctx := nativeASRCueFixture(t)
	doc.Blocks[0].SourceRefs[0].CueIDs[0] = "asr-window-0:0:18"
	if err := Validate(doc, ctx); err == nil {
		t.Fatal("accepted structurally legal cue absent from frozen source")
	}
	doc, ctx = nativeASRCueFixture(t)
	doc.Blocks[0].SourceRefs[0].StartMS = ptr[int64](120001)
	if err := Validate(doc, ctx); err == nil {
		t.Fatal("accepted interpolated ASR time")
	}
	doc, ctx = nativeASRCueFixture(t)
	doc.Blocks[0].ID = "block:0"
	if err := Validate(doc, ctx); err == nil {
		t.Fatal("colon cue syntax leaked into block IDs")
	}
	doc, ctx = illustrated()
	doc.Blocks[0].Figures[0].ID = "figure:0"
	if err := Validate(doc, ctx); err == nil {
		t.Fatal("colon cue syntax leaked into figure IDs")
	}
}
