package store

import (
	"context"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestCrossParagraphQuestionsValidatePersistReplayAndStayPrivate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, other := session(t, s), session(t, s)
	r, claim := prepared(t, s, owner)
	first, second := strings.Repeat("甲", 600)+"第一段。", strings.Repeat("乙", 600)+"第二段。"
	art := domain.Article{SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: "跨段引用", Quality: "standard", Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}, Sections: []domain.Section{
		{ID: "s1", Title: "第一章", Blocks: []domain.Block{{ID: "a", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: first}}}, {ID: "b", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: second}}}}},
		{ID: "s2", Title: "第二章", Blocks: []domain.Block{{ID: "c", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "第三段。"}}}}},
	}}
	if err := s.Publish(ctx, claim, art); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := s.EnsureConversation(ctx, owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	parts := []domain.Passage{{SectionID: "s1", BlockID: "a", Quote: "第一段。"}, {SectionID: "s1", BlockID: "b", Quote: second}, {SectionID: "s2", BlockID: "c", Quote: "第三"}}
	for _, bad := range []domain.QuestionContext{
		{SectionID: "foreign"},
		{Passages: []domain.Passage{parts[0], {SectionID: "s1", BlockID: "b", Quote: "伪造文字"}}},
		{Passages: []domain.Passage{parts[0], {SectionID: "other-article", BlockID: "b", Quote: second}}},
		{Passages: []domain.Passage{parts[0], parts[0]}},
		{Passages: []domain.Passage{parts[0], parts[2]}},
		{Passages: []domain.Passage{{SectionID: "s1", BlockID: "a", Quote: first}, parts[1]}},
		{Passage: &parts[0], Passages: parts},
	} {
		if _, err = s.CreateTurn(ctx, owner, conversation.ID, domain.ID(), domain.QuestionInput{Question: "解释选文", Context: bad}, ""); problemCode(err) != "INVALID_CONTEXT" {
			t.Fatalf("invalid selection was accepted: %v", err)
		}
	}
	var count int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM qa_turns").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected selections created turns: %d %v", count, err)
	}
	input := domain.QuestionInput{Question: "这些段落之间有什么关系？", Context: domain.QuestionContext{Passages: parts}}
	accepted, err := s.CreateTurn(ctx, owner, conversation.ID, "cross-paragraph", input, "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.Turn(ctx, owner, conversation.ID, accepted.ID)
	if err != nil || len(turn.Context.Passages) != 3 || turn.Context.Passages[2].SectionID != "s2" || turn.Context.Passage != nil {
		t.Fatalf("selected context lost: %+v %v", turn.Context, err)
	}
	replayed, err := s.CreateTurn(ctx, owner, conversation.ID, "cross-paragraph", input, "")
	if err != nil || !replayed.Replayed || replayed.ID != accepted.ID {
		t.Fatalf("selection replay changed: %+v %v", replayed, err)
	}
	changed := input
	changed.Context.Passages = append([]domain.Passage(nil), parts...)
	changed.Context.Passages[2].Quote = "第三段"
	if _, err = s.CreateTurn(ctx, owner, conversation.ID, "cross-paragraph", changed, ""); problemCode(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("changed later quote escaped idempotency: %v", err)
	}
	if _, err = s.Turn(ctx, other, conversation.ID, accepted.ID); !notFound(err) {
		t.Fatalf("selection leaked across sessions: %v", err)
	}
	if err = s.ReplaceExampleShowcases(ctx, []string{conversation.ArticleID}); err != nil {
		t.Fatal(err)
	}
	public, _, err := s.OpenExampleConversation(ctx, other, conversation.ArticleID)
	if err != nil {
		t.Fatal(err)
	}
	exampleTurn, err := s.CreateTurn(ctx, other, public.ID, "example-selection", input, "")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Turn(ctx, other, public.ID, exampleTurn.ID)
	if err != nil || len(saved.Context.Passages) != 3 {
		t.Fatalf("public example lost cross-paragraph selection: %+v %v", saved.Context, err)
	}
}

func TestChapterQuestionPersistsAndBindsIdempotency(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	r, claim := prepared(t, s, owner)
	a := domain.Article{SnapshotID: *r.SnapshotID, Sections: []domain.Section{{ID: "first"}, {ID: "second"}}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}
	if err := s.Publish(ctx, claim, a); err != nil {
		t.Fatal(err)
	}
	c, _, err := s.EnsureConversation(ctx, owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := domain.QuestionInput{Question: "这一章的重点？", Context: domain.QuestionContext{SectionID: "first"}}
	created, err := s.CreateTurn(ctx, owner, c.ID, "chapter", in, "")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Turn(ctx, owner, c.ID, created.ID)
	if err != nil || saved.Context.SectionID != "first" {
		t.Fatalf("chapter lost: %+v %v", saved.Context, err)
	}
	replay, err := s.CreateTurn(ctx, owner, c.ID, "chapter", in, "")
	if err != nil || !replay.Replayed || replay.ID != created.ID {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	in.Context.SectionID = "second"
	if _, err = s.CreateTurn(ctx, owner, c.ID, "chapter", in, ""); problemCode(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("chapter escaped idempotency: %v", err)
	}
}

func TestLegacySingleParagraphQuestionStillPersists(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	r, claim := prepared(t, s, owner)
	art := domain.Article{SnapshotID: *r.SnapshotID, Sections: []domain.Section{{ID: "s", Blocks: []domain.Block{{ID: "p", Type: "paragraph", Content: []domain.Inline{{Type: "text", Text: "保留旧的单段引用。"}}}}}}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}
	if err := s.Publish(ctx, claim, art); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := s.EnsureConversation(ctx, owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := domain.QuestionInput{Question: "解释这段", Context: domain.QuestionContext{Passage: &domain.Passage{SectionID: "s", BlockID: "p", Quote: " 单段引用 "}}}
	accepted, err := s.CreateTurn(ctx, owner, conversation.ID, "legacy", input, "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.Turn(ctx, owner, conversation.ID, accepted.ID)
	if err != nil || turn.Context.Passage == nil || turn.Context.Passage.Quote != "单段引用" || len(turn.Context.Passages) != 0 {
		t.Fatalf("legacy context changed: %+v %v", turn.Context, err)
	}
}
