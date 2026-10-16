package store

import (
	"context"
	"sync"
	"testing"
	"vidlens/internal/domain"
)

func TestConcurrentDetailsCoalesceSourceAndCancelIndependently(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	type result struct {
		level    string
		accepted Accepted
		err      error
	}
	results := make(chan result, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		level := []string{"concise", "standard", "detailed"}[i%3]
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := s.CreateRequestWithInput(ctx, owner, level, domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", DetailLevel: &level}, "", false)
			results <- result{level, a, e}
		}()
	}
	wg.Wait()
	close(results)
	ids := map[string]string{}
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if prev := ids[r.level]; prev != "" && prev != r.accepted.ID {
			t.Fatal("same accepted detail duplicated")
		}
		ids[r.level] = r.accepted.ID
	}
	var common string
	for _, id := range ids {
		r, e := s.Request(ctx, owner, id)
		if e != nil {
			t.Fatal(e)
		}
		if common == "" {
			common = *r.ActiveRunID
		}
		if *r.ActiveRunID != common {
			t.Fatal("concurrent detail fragmented source")
		}
	}
	if err := s.CancelRequest(ctx, owner, ids["concise"]); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(ctx, common)
	if err != nil || run.Status == "cancel_requested" {
		t.Fatal("one detail canceled source needed by others", err)
	}
	for _, level := range []string{"standard", "detailed"} {
		r, e := s.Request(ctx, owner, ids[level])
		if e != nil || r.Status != "waiting_source" {
			t.Fatal("peer detail was canceled", e)
		}
	}
}
