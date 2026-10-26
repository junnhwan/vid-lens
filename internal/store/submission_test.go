package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestSubmissionGuardRunsOnceAfterConcurrentReplayAndBeforeWork(t *testing.T) {
	s := testStore(t)
	owner := session(t, s)
	var checks atomic.Int32
	ctx := WithSubmissionCheck(context.Background(), func(_ context.Context, kind, actor string) error {
		if kind != "generation" || actor != owner {
			t.Error("wrong actor or kind")
		}
		checks.Add(1)
		return nil
	})
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := s.CreateRequest(ctx, owner, "same-action", "https://youtube.com/watch?v=abcdefghijk", "", false)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- a.ID
		}()
	}
	wg.Wait()
	close(ids)
	var id string
	for v := range ids {
		if id != "" && id != v {
			t.Fatal("concurrent replay created another request")
		}
		id = v
	}
	if checks.Load() != 1 {
		t.Fatalf("replay consumed tokens %d times", checks.Load())
	}
	if _, err := s.CreateRequest(ctx, owner, "same-action", "https://youtube.com/watch?v=bcdefghijkl", "", false); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	if checks.Load() != 1 {
		t.Fatal("idempotency conflict consumed tokens")
	}
	blocked := WithSubmissionCheck(context.Background(), func(context.Context, string, string) error { return domain.Err(429, "RATE_LIMITED", "blocked") })
	if _, err := s.CreateRequest(blocked, owner, "new-action", "https://youtube.com/watch?v=bcdefghijkl", "", false); err == nil {
		t.Fatal("guard denial admitted work")
	}
	if replay, err := s.CreateRequest(blocked, owner, "same-action", "https://youtube.com/watch?v=abcdefghijk", "", false); err != nil || !replay.Replayed || replay.ID != id {
		t.Fatal("denied guard prevented known replay", err)
	}
	for _, table := range []string{"generation_requests", "job_runs", "task_outbox"} {
		var n int
		if err := s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("denial wrote %s: n=%d err=%v", table, n, err)
		}
	}
}

func TestQuestionSubmissionGuardPreservesReplayAndValidation(t *testing.T) {
	s := testStore(t)
	base := context.Background()
	owner := session(t, s)
	r, c := prepared(t, s, owner)
	if err := s.Publish(base, c, domain.Article{SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: "fixture", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Quality: "standard"}); err != nil {
		t.Fatal(err)
	}
	con, _, err := s.EnsureConversation(base, owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	ctx := WithSubmissionCheck(base, func(_ context.Context, kind, actor string) error {
		checks++
		if kind != "question" || actor != owner {
			t.Fatal("wrong question guard identity")
		}
		return nil
	})
	if _, err = s.CreateTurn(ctx, owner, con.ID, "invalid", domain.QuestionInput{}, ""); err == nil || checks != 0 {
		t.Fatal("invalid question consumed tokens")
	}
	in := domain.QuestionInput{Question: "explain this video"}
	a, err := s.CreateTurn(ctx, owner, con.ID, "question", in, "")
	if err != nil || checks != 1 {
		t.Fatal(err, checks)
	}
	blocked := WithSubmissionCheck(base, func(context.Context, string, string) error {
		return domain.Err(503, "SUBMISSION_GUARD_UNAVAILABLE", "unavailable")
	})
	if replay, err := s.CreateTurn(blocked, owner, con.ID, "question", in, ""); err != nil || !replay.Replayed || replay.ID != a.ID {
		t.Fatal("question replay reached unavailable guard", err)
	}
	if err := s.CancelTurn(base, owner, con.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	turn, _ := s.Turn(base, owner, con.ID, a.ID)
	if _, err := s.Claim(base, turn.RunID, 1, "cancel-fixture"); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateTurn(blocked, owner, con.ID, "next", domain.QuestionInput{Question: "next", ExpectedLastTurnID: &a.ID}, "")
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "SUBMISSION_GUARD_UNAVAILABLE" {
		t.Fatal("new question bypassed guard", err)
	}
	if _, err = s.CreateTurn(ctx, session(t, s), con.ID, "foreign", in, ""); err == nil || checks != 1 {
		t.Fatal("foreign conversation reached submission guard")
	}
	review, err := s.CreateArticleReview(ctx, owner, r.ID, "manual-review")
	if err != nil || checks != 2 {
		t.Fatal("manual review did not use interactive guard", err)
	}
	joined, err := s.CreateArticleReview(blocked, owner, r.ID, "joined-review")
	if err != nil || joined.ID != review.ID {
		t.Fatal("existing review was charged again", err)
	}
}

func TestConcurrentSameSpecSharesWorkButKeepsRequestOwnership(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	originalOwner := session(t, s)
	r, c := prepared(t, s, originalOwner)
	owners := []string{session(t, s), session(t, s), session(t, s)}
	type result struct{ owner, id string }
	results := make(chan result, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := owners[i%3]
			a, err := s.CreateRequest(ctx, owner, domain.ID(), r.InputURL, "", false)
			if err != nil {
				t.Error(err)
				return
			}
			results <- result{owner, a.ID}
		}(i)
	}
	wg.Wait()
	close(results)
	requests := []result{}
	for x := range results {
		next, err := s.Request(ctx, x.owner, x.id)
		if err != nil || next.ActiveRunID == nil || *next.ActiveRunID != c.Run.ID {
			t.Fatal("same spec fragmented work", err)
		}
		requests = append(requests, x)
	}
	if len(requests) != 12 {
		t.Fatal("not all requests admitted")
	}
	if err := s.CancelRequest(ctx, requests[0].owner, requests[0].id); err != nil {
		t.Fatal(err)
	}
	run, _ := s.Run(ctx, c.Run.ID)
	if run.Status != "running" {
		t.Fatal("one subscriber canceled shared generation")
	}
	if _, _, err := s.RequestView(ctx, originalOwner, requests[0].id); err == nil {
		t.Fatal("shared work exposed another request")
	}
	for _, in := range []domain.RequestInput{{VideoURL: r.InputURL, Purpose: "different goal"}, {VideoURL: r.InputURL, DetailLevel: domain.Ptr("detailed")}} {
		a, err := s.CreateRequestWithInput(ctx, owners[0], domain.ID(), in, "", false)
		if err != nil {
			t.Fatal(err)
		}
		next, _ := s.Request(ctx, owners[0], a.ID)
		if next.SnapshotID == nil || *next.SnapshotID != *r.SnapshotID || next.ActiveRunID == nil || *next.ActiveRunID == c.Run.ID {
			t.Fatal("different spec reused interpretation or repeated source")
		}
	}
	var sources, articles int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE kind='source.prepare'),count(*) FILTER(WHERE kind='article.generate') FROM job_runs").Scan(&sources, &articles); err != nil || sources != 1 || articles != 3 {
		t.Fatal("unexpected durable work", sources, articles, err)
	}
	if err := s.Publish(ctx, c, domain.Article{SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: "immutable result", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Quality: "standard"}); err != nil {
		t.Fatal(err)
	}
	accepted, err := s.CreateRequest(ctx, owners[1], domain.ID(), r.InputURL, "", false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Request(ctx, owners[1], accepted.ID)
	if err != nil || next.Status != "completed" || next.Reuse != "published_article" {
		t.Fatal("completed identical result was not reused", err)
	}
	var after int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM job_runs").Scan(&after)
	if after != 4 {
		t.Fatal("cache hit created new work", after)
	}
}
