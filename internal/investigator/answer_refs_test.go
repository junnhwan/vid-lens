package investigator

import (
	"context"
	"encoding/json"
	"testing"

	"vidlens/internal/domain"
)

func TestCompactAnswerSchemaPreservesLegacyRequiredCitations(t *testing.T) {
	for _, revision := range []int{1, 6} {
		s := &session{turn: &domain.Turn{}, cp: checkpoint{HarnessRevision: revision}}
		if _, err := s.makeTools(); err != nil {
			t.Fatal(err)
		}
		info, err := s.tools["submit_answer"].Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		sc, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Required []string `json:"required"`
		}
		if err = json.Unmarshal(domain.JSON(sc), &value); err != nil {
			t.Fatal(err)
		}
		required := false
		for _, name := range value.Required {
			required = required || name == "citations"
		}
		if required != (revision == 1) {
			t.Fatalf("revision %d changed citation submission contract: %s", revision, domain.JSON(sc))
		}
	}
}

func TestCompactAnswerResolvesAllReferenceLocationsWithoutMutatingInput(t *testing.T) {
	catalog := map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text", SnapshotID: "owned"}, "f1": {ID: "f1", Kind: "video_frame", FrameID: "actual-frame", SnapshotID: "owned"}}
	in := compactAnswerInput{Blocks: []domain.Block{
		{Type: "list", Items: []domain.ListItem{{Content: []domain.Inline{{Type: "citation", CitationID: "t1"}}}}},
		{Type: "table", Rows: []domain.TableRow{{CitationIDs: []string{"t1"}}}},
		{Type: "figure", FrameIDs: []string{"f1"}, CitationIDs: []string{"f1"}},
	}, Limitations: []domain.Limitation{{CitationIDs: []string{"t1"}}}, Citations: map[string]domain.Citation{"web": {ID: "web", Kind: "external_reference", ReferenceID: "read-snapshot"}}}
	out, err := resolveAnswerReferences(in, catalog)
	if err != nil || len(out.Citations) != 3 || out.Blocks[2].FrameIDs[0] != "actual-frame" || in.Blocks[2].FrameIDs[0] != "f1" || out.Citations["web"].ReferenceID != "read-snapshot" {
		t.Fatalf("reference projection changed input or dropped evidence: %+v %v", out, err)
	}
}
