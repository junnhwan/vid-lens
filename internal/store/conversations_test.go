package store

import (
	"context"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func conversationFixture(t *testing.T) (*Store, string, domain.Request, domain.Conversation) {
	t.Helper()
	s := testStore(t)
	owner := session(t, s)
	r, claim := prepared(t, s, owner)
	article := domain.Article{SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: "Synthetic source", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}}
	if err := s.Publish(context.Background(), claim, article); err != nil {
		t.Fatal(err)
	}
	c, _, err := s.EnsureConversation(context.Background(), owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, owner, r, c
}

func TestConversationCreationReplayPaginationPrivacyAndHistoryIsolation(t *testing.T) {
	s, owner, r, first := conversationFixture(t)
	ctx := context.Background()
	accepted, err := s.CreateTurn(ctx, owner, first.ID, "old-question", domain.QuestionInput{Question: "旧话题的第一问"}, "")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Turn(ctx, owner, first.ID, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A saved fixture answer exercises context ownership without calling models.
	if _, err = s.Pool.Exec(ctx, "UPDATE qa_turns SET status='completed',expires_at=$2 WHERE id=$1", old.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE qa_conversations SET active_turn_id=NULL WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateConversation(ctx, owner, r.ID, "new-thread")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.CreateConversation(ctx, owner, r.ID, "new-thread")
	if err != nil || !replayed.Replayed || replayed.ID != created.ID || created.ID == first.ID {
		t.Fatal("creation was not idempotent", replayed, err)
	}
	newest, err := s.CreateConversation(ctx, owner, r.ID, "another-thread")
	if err != nil {
		t.Fatal(err)
	}
	current, wasCreated, err := s.EnsureConversation(ctx, owner, r.ID)
	if err != nil || wasCreated || current.ID != newest.ID {
		t.Fatal("legacy open did not resolve latest thread", current, err)
	}
	seen := map[string]bool{}
	before := ""
	for {
		page, err := s.Conversations(ctx, owner, r.ID, before, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].SessionID != "" || page.Items[0].NextTurnNo != 0 || seen[page.Items[0].ID] {
			t.Fatal("pagination or public metadata invalid", page)
		}
		item := page.Items[0]
		seen[item.ID] = true
		if item.ID == first.ID && (item.Title != "旧话题的第一问" || item.TurnCount != 1) {
			t.Fatal("history metadata missing", item)
		}
		if !page.HasMore {
			break
		}
		if page.NextBefore == nil {
			t.Fatal("missing cursor")
		}
		before = *page.NextBefore
	}
	if len(seen) != 3 {
		t.Fatal("history omitted a thread", seen)
	}
	other := session(t, s)
	if _, err = s.Conversations(ctx, other, r.ID, "", 10); !notFound(err) {
		t.Fatal("foreign list visible", err)
	}
	if _, err = s.Conversation(ctx, other, created.ID); !notFound(err) {
		t.Fatal("foreign thread visible", err)
	}
	if _, err = s.CreateConversation(ctx, other, r.ID, "forbidden"); !notFound(err) {
		t.Fatal("foreign creation accepted", err)
	}
	if _, err = s.Conversations(ctx, owner, r.ID, domain.ID(), 10); !notFound(err) {
		t.Fatal("unowned cursor accepted", err)
	}
	var count int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM qa_turns WHERE request_id=$1", r.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("opening a thread started a question", count, err)
	}
	question, err := s.CreateTurn(ctx, owner, created.ID, "new-question", domain.QuestionInput{Question: "另一个话题"}, "")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.Turn(ctx, owner, created.ID, question.ID)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := s.PriorTurns(ctx, fresh)
	if err != nil || len(prior) != 0 {
		t.Fatal("another thread entered the new model context", prior, err)
	}
	old.ConversationID = first.ID
	old.TurnNo = 2
	prior, err = s.PriorTurns(ctx, old)
	if err != nil || len(prior) != 1 || prior[0].ID != accepted.ID {
		t.Fatal("old thread history lost", prior, err)
	}
}

func TestExampleQuestionAllowanceIsSharedAcrossThreads(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	example := publishExample(t, s, "Synthetic example", "Section")
	if err := s.ReplaceExampleShowcases(ctx, []string{example.articleID}); err != nil {
		t.Fatal(err)
	}
	owner := session(t, s)
	first, _, err := s.OpenExampleConversation(ctx, owner, example.articleID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ExampleQuestionLimit; i++ {
		thread, err := s.CreateConversation(ctx, owner, first.RequestID, domain.ID())
		if err != nil {
			t.Fatal(err)
		}
		x, err := s.CreateTurn(ctx, owner, thread.ID, domain.ID(), domain.QuestionInput{Question: "Synthetic question"}, "")
		if err != nil {
			t.Fatal(err)
		}
		turn, err := s.Turn(ctx, owner, thread.ID, x.ID)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := s.Claim(ctx, turn.RunID, 1, "fixture")
		if err != nil || claim == nil {
			t.Fatal(err)
		}
		if _, err = s.Fail(ctx, *claim, domain.Failure{Code: "STOPPED", CanRetry: true}, false, 0); err != nil {
			t.Fatal(err)
		}
	}
	thread, err := s.CreateConversation(ctx, owner, first.RequestID, "past-allowance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateTurn(ctx, owner, thread.ID, domain.ID(), domain.QuestionInput{Question: "Over allowance"}, ""); problemCode(err) != "EXAMPLE_QUESTION_LIMIT" {
		t.Fatal("a new thread reset the example allowance", err)
	}
}

func TestVideoCanKeepTwoIndependentConversationRecords(t *testing.T) {
	s, owner, r, first := conversationFixture(t)
	_, err := s.Pool.Exec(context.Background(), "INSERT INTO qa_conversations(id,session_id,request_id,article_id,snapshot_id) SELECT $1,session_id,request_id,article_id,snapshot_id FROM qa_conversations WHERE id=$2", domain.ID(), first.ID)
	if err != nil {
		t.Fatal("a second independent conversation is blocked", err)
	}
	var n int
	if err = s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM qa_conversations WHERE session_id=$1 AND request_id=$2", owner, r.ID).Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}
