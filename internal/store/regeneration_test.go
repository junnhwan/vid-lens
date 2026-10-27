package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"vidlens/internal/domain"
)

func TestRegenerationIsIndependentAndPublishedResultCanBeReused(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, other := session(t, s), session(t, s)
	original, originalRun := prepared(t, s, owner)
	article := domain.Article{SchemaVersion: 1, SnapshotID: *original.SnapshotID, Title: "original", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}, Quality: "standard"}
	if err := s.Publish(ctx, originalRun, article); err != nil {
		t.Fatal(err)
	}
	original, _ = s.Request(ctx, owner, original.ID)
	input := domain.RequestInput{VideoURL: original.InputURL, Regenerate: true}
	first, err := s.CreateRequestWithInput(ctx, owner, "fresh", input, "", false)
	if err != nil {
		t.Fatal(err)
	}
	firstRequest, err := s.Request(ctx, owner, first.ID)
	if err != nil || firstRequest.Status != "generating" || firstRequest.ArticleID != nil || firstRequest.GenerationSpec == nil || firstRequest.GenerationSpec.FreshArticleID == "" || !sameID(firstRequest.SnapshotID, original.SnapshotID) {
		t.Fatalf("fresh request did not reuse only the source: %+v %v", firstRequest, err)
	}
	replay, err := s.CreateRequestWithInput(ctx, owner, "fresh", input, "", false)
	if err != nil || !replay.Replayed || replay.ID != first.ID {
		t.Fatalf("fresh request was not idempotent: %+v %v", replay, err)
	}
	if _, err = s.CreateRequest(ctx, owner, "fresh", original.InputURL, "", false); err == nil {
		t.Fatal("same idempotency key accepted a different generation mode")
	}

	second, err := s.CreateRequestWithInput(ctx, other, "fresh-again", input, "", false)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest, err := s.Request(ctx, other, second.ID)
	if err != nil || secondRequest.ActiveRunID == nil || firstRequest.ActiveRunID == nil || *secondRequest.ActiveRunID == *firstRequest.ActiveRunID || secondRequest.GenerationSpec.FreshArticleID == firstRequest.GenerationSpec.FreshArticleID {
		t.Fatalf("fresh requests joined the same article run: %+v %v", secondRequest, err)
	}
	ordinary, err := s.CreateRequest(ctx, other, "ordinary", original.InputURL, "", false)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryRequest, _ := s.Request(ctx, other, ordinary.ID)
	if ordinaryRequest.Status != "completed" || !sameID(ordinaryRequest.ArticleID, original.ArticleID) {
		t.Fatal("ordinary submission stopped reusing the published article")
	}

	claim, err := s.Claim(ctx, *firstRequest.ActiveRunID, 1, "fresh-article")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	article.Title = "fresh"
	if err = s.Publish(ctx, *claim, article); err != nil {
		t.Fatal(err)
	}
	firstRequest, _ = s.Request(ctx, owner, first.ID)
	if firstRequest.ArticleID == nil || sameID(firstRequest.ArticleID, original.ArticleID) {
		t.Fatal("fresh execution did not publish a separate immutable article")
	}
	later, err := s.CreateRequest(ctx, other, "reuse-fresh", original.InputURL, "", false)
	if err != nil {
		t.Fatal(err)
	}
	laterRequest, _ := s.Request(ctx, other, later.ID)
	if laterRequest.Status != "completed" || laterRequest.Reuse != "published_article" || !sameID(laterRequest.ArticleID, firstRequest.ArticleID) {
		t.Fatal("the freshly published article was not reusable by a later ordinary request")
	}

	// Historical pre-detail idempotency records must not hide a mode change.
	if _, err = s.Pool.Exec(ctx, "UPDATE idempotency SET hash=$3 WHERE scope=$1 AND key=$2", "request:"+other, "ordinary", Hash(map[string]any{"video_url": original.InputURL})); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateRequestWithInput(ctx, other, "ordinary", input, "", false)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("legacy key replayed instead of rejecting a mode change: %v", err)
	}
}

func TestConcurrentRegenerationsShareSourceButNotArticleWork(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	videoURL := "https://youtube.com/watch?v=abcdefghijk"
	regular, err := s.CreateRequest(ctx, owner, "regular", videoURL, "", false)
	if err != nil {
		t.Fatal(err)
	}
	regularRequest, _ := s.Request(ctx, owner, regular.ID)
	const count = 4
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, err := s.CreateRequestWithInput(ctx, owner, fmt.Sprintf("fresh-%d", i), domain.RequestInput{VideoURL: videoURL, Regenerate: true}, "", false)
			if err != nil {
				errs <- err
				return
			}
			ids <- a.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	requests := []string{}
	for id := range ids {
		r, err := s.Request(ctx, owner, id)
		if err != nil || !sameID(r.ActiveRunID, regularRequest.ActiveRunID) {
			t.Fatal("fresh requests did not share the source preparation", err)
		}
		requests = append(requests, id)
	}
	if len(requests) != count {
		t.Fatal("not all fresh requests were accepted")
	}
	claim, err := s.Claim(ctx, *regularRequest.ActiveRunID, 1, "shared-source")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: claim.Run.SourceID, RunID: claim.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "fresh-source", MIME: "video/mp4", Bytes: 42}
	if err = s.AddAsset(ctx, *claim, video); err != nil {
		t.Fatal(err)
	}
	snapshot := domain.Snapshot{ID: domain.ID(), SourceID: claim.Run.SourceID, VideoHash: video.SHA256, TextHash: "text", DurationMS: 1000, Metadata: []byte(`{}`)}
	if _, err = s.FinishSource(ctx, *claim, snapshot, []domain.Segment{{Ordinal: 0, StartMS: 0, EndMS: 1000, Text: "source", Origin: "platform"}}, video.ID); err != nil {
		t.Fatal(err)
	}
	runIDs := map[string]bool{}
	for _, id := range append(requests, regular.ID) {
		r, err := s.Request(ctx, owner, id)
		if err != nil || r.Status != "generating" || r.ActiveRunID == nil || r.SnapshotID == nil || *r.SnapshotID != snapshot.ID || runIDs[*r.ActiveRunID] {
			t.Fatalf("article generation was unexpectedly shared: %+v %v", r, err)
		}
		runIDs[*r.ActiveRunID] = true
	}
}

func TestRegeneratedArticleKeepsPurposePrivate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, other := session(t, s), session(t, s)
	original, originalRun := prepared(t, s, owner)
	if err := s.Publish(ctx, originalRun, domain.Article{SnapshotID: *original.SnapshotID, Title: "ordinary", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}}); err != nil {
		t.Fatal(err)
	}
	input := domain.RequestInput{VideoURL: original.InputURL, Purpose: "关注步骤", Regenerate: true}
	accepted, err := s.CreateRequestWithInput(ctx, owner, "fresh-private", input, "", false)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := s.Request(ctx, owner, accepted.ID)
	claim, err := s.Claim(ctx, *fresh.ActiveRunID, 1, "private-fresh")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	if err = s.Publish(ctx, *claim, domain.Article{SnapshotID: *fresh.SnapshotID, Title: "private focus", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}}); err != nil {
		t.Fatal(err)
	}
	fresh, _ = s.Request(ctx, owner, accepted.ID)
	input.Regenerate = false
	withinSession, err := s.CreateRequestWithInput(ctx, owner, "reuse-private", input, "", false)
	if err != nil {
		t.Fatal(err)
	}
	within, _ := s.Request(ctx, owner, withinSession.ID)
	if within.Status != "completed" || !sameID(within.ArticleID, fresh.ArticleID) {
		t.Fatal("owner could not reuse the fresh private article")
	}
	acrossSession, err := s.CreateRequestWithInput(ctx, other, "other-private", input, "", false)
	if err != nil {
		t.Fatal(err)
	}
	across, _ := s.Request(ctx, other, acrossSession.ID)
	if across.Status != "generating" || across.ArticleID != nil || !sameID(across.SnapshotID, fresh.SnapshotID) {
		t.Fatal("another session reused a private-focus article or repeated source preparation")
	}
}

func TestRegenerationRetryKeepsItsOwnSavedDraft(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	original, originalRun := prepared(t, s, owner)
	if err := s.Publish(ctx, originalRun, domain.Article{SnapshotID: *original.SnapshotID, Title: "old", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}}); err != nil {
		t.Fatal(err)
	}
	accepted, err := s.CreateRequestWithInput(ctx, owner, "fresh", domain.RequestInput{VideoURL: original.InputURL, Regenerate: true}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := s.Request(ctx, owner, accepted.ID)
	claim, err := s.Claim(ctx, *fresh.ActiveRunID, 1, "fresh-draft")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	cp := map[string]any{"schema_version": 1, "layout_version": "source-context-v3", "protocol": domain.ArticleDraftProtocol, "ready": true, "inspections": map[string][]string{}, "calls": 2}
	if err = s.Checkpoint(ctx, *claim, cp); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveArticleDraft(ctx, *claim, "first", nil, 0, map[string]any{"title": "fresh draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Fail(ctx, *claim, domain.Failure{Code: "MODEL_RESULT_INVALID", Message: "invalid", CanRetry: true}, false, 0); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateRequest(ctx, owner, "retry", "", fresh.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Request(ctx, owner, retry.ID)
	if err != nil || next.Status != "generating" || next.ArticleID != nil || next.ActiveRunID == nil || *next.ActiveRunID == claim.Run.ID || next.GenerationSpec.FreshArticleID != fresh.GenerationSpec.FreshArticleID {
		t.Fatalf("retry lost fresh identity or reused the old article: %+v %v", next, err)
	}
	draft, err := s.ArticleDraft(ctx, *next.ActiveRunID)
	if err != nil || draft.Version != 1 {
		t.Fatalf("retry did not retain its own draft: %+v %v", draft, err)
	}
}
