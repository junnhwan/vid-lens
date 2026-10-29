package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func focusedQuestionFixture() *session {
	s := &session{profile: config.DefaultProfile(), snapshot: domain.Snapshot{ID: "source", DurationMS: 40000}, turn: &domain.Turn{Question: "这一章的关键是什么？", Context: domain.QuestionContext{SectionID: "chapter"}}, cp: checkpoint{HarnessRevision: 16, ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV4}}}
	s.article = &domain.Article{Sections: []domain.Section{{ID: "chapter", Title: "中文章节", Blocks: []domain.Block{{ID: "p", Type: "paragraph", Content: []domain.Inline{{Type: "citation", CitationID: "first"}, {Type: "citation", CitationID: "last"}}}}}}, Citations: map[string]domain.Citation{}}
	for i := 0; i < 40; i++ {
		s.transcript = append(s.transcript, domain.Segment{ID: fmt.Sprint("seg", i), Ordinal: i, StartMS: int64(i * 1000), EndMS: int64((i + 1) * 1000), Origin: "platform_subtitles", Text: fmt.Sprint("English source statement ", i)})
	}
	for alias, n := range map[string]int{"first": 2, "last": 18} {
		s.article.Citations[alias] = domain.Citation{Kind: "video_text", SnapshotID: "source", SegmentIDs: []string{s.transcript[n].ID}}
	}
	return s
}

func TestFocusedQuestionPreloadsContinuousOriginalsAcrossCitationGaps(t *testing.T) {
	s := focusedQuestionFixture()
	before := string(domain.JSON(s.article))
	passages, selected := s.questionOriginals()
	if selected != 19 || len(passages) != 19 || passages[0].ID != "seg1" || passages[18].ID != "seg19" {
		t.Fatalf("lost section context: %+v / %d", passages, selected)
	}
	if passages[9].Text != s.transcript[10].Text {
		t.Fatal("sparse anchors hid the middle of the list")
	}
	if strings.Contains(string(domain.JSON(s.sourceContext())), "English source statement 35") {
		t.Fatal("unrelated chapter was inlined")
	}
	if before != string(domain.JSON(s.article)) {
		t.Fatal("published article changed")
	}
	s.turn.Context.SectionID = ""
	s.turn.Question = "「中文章节」里最关键的一点是什么？"
	if got, _ := s.questionOriginals(); len(got) != len(passages) {
		t.Fatal("typed chapter question lost its source navigation")
	}
}

func TestFocusedQuestionBoundsOriginalsAndExposesUnreadRemainder(t *testing.T) {
	s := focusedQuestionFixture()
	for i := range s.transcript {
		s.transcript[i].Text = strings.Repeat("原文", 3000)
	}
	out := s.focusedQuestionSource()
	coverage := out["source_coverage"].(map[string]any)
	if coverage["has_more"] != true || coverage["delivered_segments"].(int) >= coverage["selected_segments"].(int) {
		t.Fatal("truncated context claimed complete coverage")
	}
	rows := out["transcript"].(map[string]any)["rows"].([][5]any)
	if len(rows) == 0 || rows[0][3] != s.transcript[1].Text {
		t.Fatal("split or lost an original cue")
	}
}

func TestFocusedAnswerNormalizesOnlyEmptyLegacyQuoteField(t *testing.T) {
	called := false
	base, err := utils.InferEnhancedTool("submit_answer", "fixture", func(_ context.Context, in readingAnswerInput) (*schema.ToolResult, error) {
		called = in.Blocks[0].Text == "Original sentence."
		return textResult(map[string]bool{"ok": true}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := focusedQuestionFixture()
	checked := &argumentCheckedTool{EnhancedInvokableTool: base, name: "submit_answer", session: s}
	raw := `{"kind":"grounded","summary":"One point.","blocks":[{"id":"q","type":"quote","origin":"video","text":"Original sentence.","quote":null,"citation_ids":["t1"]}],"limitations":[]}`
	if _, err = checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: raw}); err != nil || !called {
		t.Fatal("avoidable schema repair", err)
	}
	called = false
	if _, err = checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: strings.Replace(raw, `"quote":null`, `"quote":"different text"`, 1)}); err == nil || called {
		t.Fatal("discarded conflicting content")
	}
	s.cp.ExecutionContract.InputRevision = domain.QuestionReadingV3
	if _, err = checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: raw}); err == nil {
		t.Fatal("changed frozen historical parser")
	}
}

func TestAnswerProvenanceCountsOnlyActualAuthorPixelInputs(t *testing.T) {
	p := frozenModelInput{Budget: callBudget{ImageCount: 1, Phase: "answer"}, Messages: []*schema.Message{{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}, Extra: map[string]any{"frame_id": "actual"}}, {Type: schema.ChatMessagePartTypeText, Text: "caption", Extra: map[string]any{"frame_id": "caption-only"}}}}}}
	if got := questionViewedFrames(p); len(got) != 1 || got[0] != "actual" {
		t.Fatal(got)
	}
	encoded := domain.JSON(p)
	if err := json.Unmarshal(encoded, &p); err != nil {
		t.Fatal(err)
	}
	p.Budget.Phase = "evidence_review"
	if len(questionViewedFrames(p)) != 0 {
		t.Fatal("reviewer pixels counted as author viewing")
	}
	p.Budget.Phase, p.Budget.ImageCount = "answer", 0
	if len(questionViewedFrames(p)) != 0 {
		t.Fatal("undelivered metadata counted as pixels")
	}
}
