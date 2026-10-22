package investigator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestAnswerReadingBuildsCanonicalBodiesAndKeepsSources(t *testing.T) {
	input := []domain.ArticleReadingBlock{
		{ID: "p", Type: "paragraph", Origin: "video", Text: "Paragraph.", CitationIDs: []string{"t1"}},
		{ID: "q", Type: "quote", Origin: "video", Text: "Verbatim source.", CitationIDs: []string{"t2"}},
		{ID: "l", Type: "list", Origin: "video", Ordered: domain.Ptr(false), Items: []domain.ArticleReadingItem{{ID: "i", Text: "Own item source.", CitationIDs: []string{"t3"}}}},
		{ID: "c", Type: "code", Origin: "video", Text: "print(1)", Language: domain.Ptr("python"), CitationIDs: []string{"t4"}},
		{ID: "t", Type: "table", Origin: "video", Columns: []string{"One"}, Rows: []domain.ArticleReadingRow{{Cells: []string{"Cell."}, CitationIDs: []string{"t5"}}}},
		{ID: "f", Type: "figure", Origin: "video", Caption: "Actual pixels.", FrameIDs: []string{"f1", "f2"}, CitationIDs: []string{"f1", "f2"}},
	}
	blocks, err := answerReadingBlocks(input)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip := domain.ReadingArticle(domain.Article{Sections: []domain.Section{{Blocks: blocks}}}).Sections[0].Blocks
	if domain.ContentHash(input) != domain.ContentHash(roundtrip) || blocks[5].Layout != "sequence" {
		t.Fatalf("semantic answer conversion lost body/source meaning: %s", domain.JSON(roundtrip))
	}
	bad := input[0]
	bad.Items = input[2].Items
	if _, err = answerReadingBlocks([]domain.ArticleReadingBlock{bad}); err == nil {
		t.Fatal("a second body constructor was silently discarded")
	}
}

func TestAnswerReadingCheckedToolUsesTheDeclaredWire(t *testing.T) {
	in := readingAnswerInput{Kind: "grounded", Summary: "A short answer.", Blocks: []domain.ArticleReadingBlock{{ID: "l", Type: "list", Origin: "video", Ordered: domain.Ptr(false), Items: []domain.ArticleReadingItem{{ID: "i", Text: "Explicit item text.", CitationIDs: []string{"t1"}}}}}, Limitations: []domain.Limitation{}}
	called := false
	base, err := utils.InferEnhancedTool("submit_answer", "fixture", func(_ context.Context, got readingAnswerInput) (*schema.ToolResult, error) {
		called = domain.ContentHash(got) == domain.ContentHash(in)
		return textResult(map[string]any{"received": true}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &session{turn: &domain.Turn{}, cp: checkpoint{HarnessRevision: 16, ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV2}}}
	checked := &argumentCheckedTool{EnhancedInvokableTool: base, name: "submit_answer", session: s}
	if _, err = checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: string(domain.JSON(in))}); err != nil || !called {
		t.Fatal("outer argument validator rejected the semantic wire declared to the provider", err)
	}
	s.cp.ExecutionContract.InputRevision = domain.QuestionReadingV1
	if _, err = checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: string(domain.JSON(in))}); err == nil {
		t.Fatal("historical QA parser was silently changed to the new semantic wire")
	}
}

func TestAnswerReadingWireIsFrozenSeparatelyFromCanonicalQA(t *testing.T) {
	for _, revision := range []string{"", domain.QuestionReadingV1, domain.QuestionReadingV2} {
		s := &session{turn: &domain.Turn{}, cp: checkpoint{HarnessRevision: 16, ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: revision}}}
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
		var wire map[string]any
		if err = json.Unmarshal(domain.JSON(sc), &wire); err != nil {
			t.Fatal(err)
		}
		blocks := wire["properties"].(map[string]any)["blocks"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		_, hasContent := blocks["content"]
		if hasContent == (revision == domain.QuestionReadingV2) {
			t.Fatalf("frozen %q received the wrong answer body wire", revision)
		}
		items := blocks["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		_, itemText := items["text"]
		_, itemSources := items["citation_ids"]
		if itemText != (revision == domain.QuestionReadingV2) || itemSources != itemText {
			t.Fatal("reading answer list items do not own their text and references")
		}
	}
}
