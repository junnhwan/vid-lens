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

func TestCompactAnswerKeepsExplicitParagraphAndQuoteReferences(t *testing.T) {
	catalog := map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text", SnapshotID: "owned"}}
	for _, kind := range []string{"paragraph", "quote"} {
		t.Run(kind, func(t *testing.T) {
			in := compactAnswerInput{Blocks: []domain.Block{{Type: kind, Origin: "video", CitationIDs: []string{"t1", "t1"}, Content: []domain.Inline{{Type: "text", Text: "An explicit reference."}}}}}
			out, err := resolveAnswerReferences(in, catalog)
			if err != nil || len(out.Blocks[0].Content) != 2 || out.Blocks[0].Content[1].CitationID != "t1" || out.Citations["t1"].SnapshotID != "owned" {
				t.Fatalf("explicit references lost in canonical conversion: %+v %v", out, err)
			}
			if len(in.Blocks[0].Content) != 1 || len(in.Blocks[0].CitationIDs) != 2 {
				t.Fatal("caller input mutated")
			}
			in.Blocks[0].Content = append(in.Blocks[0].Content, domain.Inline{Type: "citation", CitationID: "t1"})
			out, err = resolveAnswerReferences(in, catalog)
			if err != nil || len(out.Blocks[0].Content) != 2 {
				t.Fatal("existing inline reference duplicated", err)
			}
		})
	}
}

func TestCompactAnswerKeepsPlainParagraphWireText(t *testing.T) {
	in := compactAnswerInput{Blocks: []domain.Block{{ID: "p", Type: "paragraph", Origin: "video", Text: "The model emits tokens.", CitationIDs: []string{"t1"}}}}
	out, err := resolveAnswerReferences(in, map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text"}})
	if err != nil || domain.BlockText(out.Blocks[0]) != in.Blocks[0].Text {
		t.Fatal("paragraph wire text was silently discarded", err)
	}
	in.Blocks[0].Content = []domain.Inline{{Type: "text", Text: "Another body."}}
	if _, err := resolveAnswerReferences(in, nil); err == nil {
		t.Fatal("conflicting text/content body was guessed")
	}
}

func TestAnswerBlockRequiresReadableTextBeyondReferences(t *testing.T) {
	s := &session{}
	for _, content := range [][]domain.Inline{{{Type: "citation", CitationID: "t1"}}, {{Type: "text", Text: "  "}, {Type: "citation", CitationID: "t1"}}} {
		if err := s.validateBlock(context.Background(), domain.Block{Type: "paragraph", Origin: "video", Content: content}, map[string]domain.Citation{"t1": {}}); err == nil {
			t.Fatal("citation-only paragraph passed the mechanical reader gate")
		}
	}
}
