package investigator

import (
	"context"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestQuestionPassagesReachModelWithIndividualCitationAnchors(t *testing.T) {
	s := planSession(t)
	s.article = &domain.Article{ID: "selected-article", Sections: []domain.Section{
		{ID: "first", Blocks: []domain.Block{{ID: "a", Type: "paragraph", Content: []domain.Inline{{Type: "text", Text: "第一段完整原文。"}}}}},
		{ID: "second", Blocks: []domain.Block{{ID: "b", Type: "paragraph", Content: []domain.Inline{{Type: "text", Text: "第二段完整原文。"}}}}},
	}}
	parts := []domain.Passage{{SectionID: "first", BlockID: "a", Quote: "第一段完整原文。"}, {SectionID: "second", BlockID: "b", Quote: "第二段完整原文。"}}
	s.turn = &domain.Turn{ID: domain.ID(), ConversationID: domain.ID(), TurnNo: 1, Question: "两段有什么联系？", Context: domain.QuestionContext{Passages: parts}}
	s.spec.Version = 1
	s.cp.LayoutVersion = "source-context-v2"
	s.cp.ExecutionContract = &domain.ExecutionContract{InputRevision: domain.QuestionReadingV2}
	before := string(domain.JSON(s.article))
	ctx := context.Background()
	messages, err := s.executionMessages(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var delivered strings.Builder
	for _, message := range messages {
		delivered.WriteString(message.Content)
	}
	for _, required := range []string{"context.passages", "第一段完整原文。", "第二段完整原文。", `"block_id":"a"`, `"block_id":"b"`} {
		if !strings.Contains(delivered.String(), required) {
			t.Fatalf("model lost selection content or anchors: %s", required)
		}
	}
	for _, part := range parts {
		if err = s.validateCitation(ctx, domain.Citation{Kind: "article_passage", ArticleID: s.article.ID, SectionID: part.SectionID, BlockID: part.BlockID, Quote: &part.Quote}, false); err != nil {
			t.Fatal(err)
		}
	}
	joined := parts[0].Quote + " " + parts[1].Quote
	if err = s.validateCitation(ctx, domain.Citation{Kind: "article_passage", ArticleID: s.article.ID, SectionID: "first", BlockID: "a", Quote: &joined}, false); err == nil {
		t.Fatal("combined quote was incorrectly bound to one block")
	}
	if string(domain.JSON(s.article)) != before {
		t.Fatal("selection handling rewrote the published article")
	}
}
