package store

import (
	"context"
	"testing"
)

func TestRequestHistoryIsSessionScopedAndPaginated(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, other := session(t, s), session(t, s)
	owned := make(map[string]bool)
	for i, video := range []string{
		"https://www.youtube.com/watch?v=abcdefghijk",
		"https://www.youtube.com/watch?v=bcdefghijkl",
		"https://www.youtube.com/watch?v=cdefghijklm",
	} {
		key := []string{"history-a", "history-b", "history-c"}[i]
		created, err := s.CreateRequest(ctx, owner, key, video, "", false)
		if err != nil {
			t.Fatal(err)
		}
		owned[created.ID] = true
	}
	foreign, err := s.CreateRequest(ctx, other, "foreign-history", "https://www.youtube.com/watch?v=defghijklmn", "", false)
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.RequestHistory(ctx, owner, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.RequestHistory(ctx, owner, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || !first.HasMore || len(second.Items) != 1 || second.HasMore {
		t.Fatalf("unexpected pages: first=%+v second=%+v", first, second)
	}
	seen := make(map[string]bool)
	for _, item := range append(first.Items, second.Items...) {
		seen[item.ID] = true
		if !owned[item.ID] || item.CanonicalURL == "" || item.Title != item.CanonicalURL {
			t.Fatalf("unexpected history item: %+v", item)
		}
	}
	if len(seen) != len(owned) || seen[foreign.ID] {
		t.Fatalf("session history leaked or omitted requests: %v", seen)
	}
	empty, err := s.RequestHistory(ctx, other, 20, 0)
	if err != nil || len(empty.Items) != 1 || empty.Items[0].ID != foreign.ID {
		t.Fatalf("foreign session history: %+v, %v", empty, err)
	}
}
