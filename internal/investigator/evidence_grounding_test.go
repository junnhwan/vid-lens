package investigator

import (
	"encoding/json"
	"reflect"
	"testing"

	"vidlens/internal/domain"
)

func TestFrameContextKeepsBeforeAndAfterSourcesSeparate(t *testing.T) {
	text := []domain.Segment{
		{ID: "before", StartMS: 0, EndMS: 1000, Origin: "platform_subtitles", Text: "The original report shows 33 customers."},
		{ID: "after", StartMS: 1500, EndMS: 2000, Origin: "platform_subtitles", Text: "Now refactor the reader."},
	}
	before := string(domain.JSON(text))
	catalog := map[string]domain.Citation{
		"f1": {Kind: "video_frame", FrameID: "original-output", TimeMS: domain.Ptr(int64(500))},
		"f2": {Kind: "video_frame", FrameID: "changed-code", TimeMS: domain.Ptr(int64(1750))},
	}
	contexts := frameContexts(catalog, text)
	for alias, expected := range map[string]string{"f1": "t1", "f2": "t2"} {
		value := contexts[alias].(map[string]any)
		if !reflect.DeepEqual(value["text_ids"], []string{expected}) || value["frame_id"] != catalog[alias].FrameID || value["time_ms"] != *catalog[alias].TimeMS {
			t.Fatal("a screenshot acquired another stage's narration", contexts)
		}
	}
	if string(domain.JSON(text)) != before {
		t.Fatal("source indexing modified immutable transcript text or timing")
	}
	// The exact same coarse ASR block covers both frames. The context must
	// explicitly avoid claiming a sentence-level alignment to either stage.
	asr := []domain.Segment{{ID: "coarse", StartMS: 0, EndMS: 2000, Origin: "asr", Text: "Before ... after ..."}}
	for _, at := range []int64{500, 1750} {
		hint := frameTextContext(at, asr)
		if hint["alignment"] != "coarse_audio_window_not_sentence_timing" || !reflect.DeepEqual(hint["text_ids"], []string{"t1"}) {
			t.Fatal("coarse audio was promoted to exact image/sentence alignment", hint)
		}
	}
	if frameTextContext(50000, text)["alignment"] != "no_nearby_text" {
		t.Fatal("a distant caption was attached to unrelated pixels")
	}
}

func TestFigureReferencesIncludeDisplayedPixelsWithoutRewritingClaims(t *testing.T) {
	section := domain.Section{ID: "comparison", Blocks: []domain.Block{{Type: "figure", Caption: "原始输出，不能证明后阶段结果相同。", FrameIDs: []string{"f1", "after-frame"}, CitationIDs: []string{"t1"}}}}
	catalog := map[string]domain.Citation{"f1": {Kind: "video_frame", FrameID: "before-frame"}, "f2": {Kind: "video_frame", FrameID: "after-frame"}}
	original := string(domain.JSON(section))
	linked := linkFigureFrames(section, catalog)
	if !reflect.DeepEqual(linked.Blocks[0].CitationIDs, []string{"t1", "f1", "f2"}) || linked.Blocks[0].Caption != section.Blocks[0].Caption || !reflect.DeepEqual(linked.Blocks[0].FrameIDs, section.Blocks[0].FrameIDs) {
		t.Fatal("source linking rewrote claims or omitted a displayed image", linked)
	}
	if string(domain.JSON(section)) != original {
		t.Fatal("source linking mutated the original candidate and its replay hash")
	}
	if !reflect.DeepEqual(linkFigureFrames(linked, catalog), linked) {
		t.Fatal("source links duplicated on replay")
	}
	var decoded domain.Section
	if err := json.Unmarshal(domain.JSON(linked), &decoded); err != nil || decoded.Blocks[0].Caption != section.Blocks[0].Caption {
		t.Fatal("canonical article lost its unchanged caption", err)
	}
}

func TestGroundingProtocolDoesNotUpgradeAnExistingCheckpoint(t *testing.T) {
	p, _ := domain.InterpretationPolicy("standard")
	for _, question := range []bool{false, true} {
		revision, _ := initialProtocol(domain.GenerationSpec{Version: 2, DetailPolicy: &p}, question, draftLayout, true, 0)
		if revision != 17 || !policyAt(revision).EvidenceGrounding || !policyAt(revision).ContentReview || !policyAt(revision).ReviewRecovery {
			t.Fatal("new evidence protocol was not selected", revision, question)
		}
		for _, historical := range []int{0, 3, 11, 12, 13, 14} {
			revision, _ = initialProtocol(domain.GenerationSpec{Version: 2, DetailPolicy: &p}, question, draftLayout, false, historical)
			if revision != historical || historical < 14 && policyAt(revision).EvidenceGrounding || policyAt(revision).ContentReview {
				t.Fatal("an existing checkpoint acquired a new grounding contract", historical, revision)
			}
		}
	}
}
