package investigator

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestAuthoringContextReferencesCompleteTranscriptWithoutRepeatingMetadata(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 13
	// A platform cue can extend beyond actual media. The compact mapping must
	// still carry the canonical intersection, without editing original text.
	s.transcript[len(s.transcript)-1].EndMS = 2300
	raw := string(domain.JSON(map[string]any{"transcript": s.transcript, "duration_ms": s.snapshot.DurationMS}))
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{schema.SystemMessage("frozen"), schema.UserMessage(raw)}}
	if _, _, err := (&middleware{s: s}).BeforeModelRewriteState(context.Background(), state, nil); err != nil {
		t.Fatal(err)
	}
	if state.Messages[1].Content != raw {
		t.Fatal("context optimization changed or truncated the original transcript")
	}
	var data struct {
		Catalog map[string]map[string]json.RawMessage `json:"citation_catalog"`
		Rule    string                                `json:"citation_catalog_rule"`
	}
	for _, msg := range state.Messages {
		if msg.Name == "vidlens_draft_state" {
			if err := json.Unmarshal([]byte(msg.Content), &data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(data.Catalog) != len(s.transcript) || data.Rule == "" {
		t.Fatal("compact aliases lost transcript entries or their mapping rule")
	}
	for i := range s.transcript {
		alias := "t" + strconv.Itoa(i+1)
		hint := data.Catalog[alias]
		var index int
		if hint["transcript_index"] == nil || json.Unmarshal(hint["transcript_index"], &index) != nil || index != i {
			t.Fatalf("%s does not identify its exact source element", alias)
		}
		if hint["segment_ids"] != nil || hint["text_origin"] != nil || hint["sentence_timing_known"] != nil {
			t.Fatal("authoring catalog repeats metadata already present in complete transcript", alias)
		}
	}
	var clipped domain.Range
	if json.Unmarshal(data.Catalog["t25"]["canonical_range"], &clipped) != nil || clipped.EndMS != s.snapshot.DurationMS {
		t.Fatal("clipped real source range was lost")
	}
}

func TestAuthoringCatalogRetainsActualFrameIdentityAndRejectsUnknownSources(t *testing.T) {
	frame := domain.Citation{Kind: "video_frame", FrameID: "actual-frame", TimeMS: domain.Ptr(int64(123))}
	hints, err := authoringCatalogHints(map[string]domain.Citation{"f1": frame}, nil)
	if err != nil || string(domain.JSON(hints)) != string(domain.JSON(catalogHints(map[string]domain.Citation{"f1": frame}))) {
		t.Fatal("pixel identity or real time changed", hints, err)
	}
	for _, citation := range []domain.Citation{
		{Kind: "video_text", SegmentIDs: []string{"missing"}, Range: &domain.Range{}},
		{Kind: "video_text", SegmentIDs: []string{"one", "two"}, Range: &domain.Range{}},
	} {
		if _, err := authoringCatalogHints(map[string]domain.Citation{"t1": citation}, nil); err == nil {
			t.Fatal("compact catalog concealed an unknown source mapping")
		}
	}
}
