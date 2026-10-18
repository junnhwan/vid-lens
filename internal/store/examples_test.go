package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestPublicExampleFrameURLStaysOnTheExampleRoute(t *testing.T) {
	articleID := strings.Repeat("ab", 16)
	frameID := strings.Repeat("cd", 16)
	frame := publicFrame(domain.Asset{ID: frameID, MIME: "image/jpeg", TimeMS: domain.Ptr(int64(1500))}, articleID)
	if frame.SnapshotID != "" || frame.ExpiresAt != nil || frame.URLs["full"] == nil || frame.URLs["thumbnail"] == nil {
		t.Fatalf("%+v", frame)
	}
	if *frame.URLs["full"] != *frame.URLs["thumbnail"] || *frame.URLs["full"] != exampleFrameURL(articleID, frameID) {
		t.Fatalf("url %s", *frame.URLs["full"])
	}
	if strings.Contains(*frame.URLs["full"], "/api/v1/requests/") {
		t.Fatal("public frame uses a private request path")
	}
	other := publicFrame(domain.Asset{ID: strings.Repeat("ef", 16), MIME: "image/jpeg"}, strings.Repeat("01", 16))
	if *other.URLs["full"] == *frame.URLs["full"] {
		t.Fatal("frame urls alias one another")
	}
}

func TestReplaceExampleShowcasesRejectsInvalidChoicesBeforeWriting(t *testing.T) {
	s := &Store{}
	tooMany := make([]string, maxExampleShowcases+1)
	for i := range tooMany {
		tooMany[i] = domain.ID()
	}
	if e := s.ReplaceExampleShowcases(context.Background(), tooMany); !invalid(e) {
		t.Fatal(e)
	}
	if e := s.ReplaceExampleShowcases(context.Background(), []string{"not-an-article"}); !invalid(e) {
		t.Fatal(e)
	}
	id := domain.ID()
	if e := s.ReplaceExampleShowcases(context.Background(), []string{id, id}); !invalid(e) {
		t.Fatal(e)
	}
}

func TestExampleShowcasesChoosePublishedArticles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first := publishExample(t, s, "第一篇", "第一节")
	second := publishExample(t, s, "第二篇", "第二节")

	cards, e := s.PublicExamples(ctx)
	if e != nil || len(cards) != 0 {
		t.Fatalf("unpublished examples leaked: %+v %v", cards, e)
	}
	if _, e = s.PublicExample(ctx, first.articleID); !notFound(e) {
		t.Fatalf("hidden article readable: %v", e)
	}
	if _, e = s.PublicExampleFrame(ctx, first.articleID, first.frameID); !notFound(e) {
		t.Fatalf("hidden frame readable: %v", e)
	}

	if e = s.ReplaceExampleShowcases(ctx, []string{second.articleID, first.articleID}); e != nil {
		t.Fatal(e)
	}
	cards, e = s.PublicExamples(ctx)
	if e != nil || len(cards) != 2 || cards[0].ID != second.articleID || cards[1].ID != first.articleID {
		t.Fatalf("showcase order: %+v %v", cards, e)
	}
	if cards[0].Cover != exampleFrameURL(second.articleID, second.frameID) || cards[0].Description != "第二节" {
		t.Fatalf("card: %+v", cards[0])
	}
	example, e := s.PublicExample(ctx, second.articleID)
	if e != nil || example.Article.Title != "第二篇" || len(example.Frames) != 1 || len(example.Questions) != 0 || len(example.GenerationSteps) != 0 {
		t.Fatalf("public example: %+v %v", example, e)
	}
	if example.Frames[0].URLs["full"] == nil || *example.Frames[0].URLs["full"] != cards[0].Cover {
		t.Fatalf("frame url: %+v", example.Frames[0])
	}
	raw, e := json.Marshal(example)
	if e != nil {
		t.Fatal(e)
	}
	text := string(raw)
	for _, forbidden := range []string{"request_id", "storage_key", "session_id", "conversation_id", "sha256", "api_key", "config_id", second.storageKey} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public example contains %s", forbidden)
		}
	}
	asset, e := s.PublicExampleFrame(ctx, second.articleID, second.frameID)
	if e != nil || asset.ID != second.frameID || asset.StorageKey != second.storageKey {
		t.Fatalf("frame asset: %+v %v", asset, e)
	}
	if _, e = s.PublicExampleFrame(ctx, second.articleID, first.frameID); !notFound(e) {
		t.Fatalf("cross-article frame: %v", e)
	}

	if e = s.ReplaceExampleShowcases(ctx, []string{}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublicExample(ctx, second.articleID); !notFound(e) {
		t.Fatalf("cleared showcase still public: %v", e)
	}
	missing := strings.Repeat("ab", 16)
	if e = s.ReplaceExampleShowcases(ctx, []string{missing}); !notFound(e) {
		t.Fatalf("missing article: %v", e)
	}
	if cards, e = s.PublicExamples(ctx); e != nil || len(cards) != 0 {
		t.Fatalf("failed save changed showcase: %+v %v", cards, e)
	}
	tooMany := make([]string, 25)
	for i := range tooMany {
		tooMany[i] = domain.ID()
	}
	if e = s.ReplaceExampleShowcases(ctx, tooMany); !invalid(e) {
		t.Fatalf("limit: %v", e)
	}
	if e = s.ReplaceExampleShowcases(ctx, []string{first.articleID, first.articleID}); !invalid(e) {
		t.Fatalf("duplicate: %v", e)
	}
	if e = s.ReplaceExampleShowcases(ctx, []string{"not-an-article"}); !invalid(e) {
		t.Fatalf("malformed: %v", e)
	}
}

type publishedExample struct {
	articleID  string
	frameID    string
	storageKey string
}

func publishExample(t *testing.T, s *Store, title, section string) publishedExample {
	t.Helper()
	ctx := context.Background()
	owner := session(t, s)
	// Each published example needs its own source: the product correctly reuses
	// an existing article for another request with the same generation spec.
	r, c := prepared(t, s, owner, "https://youtube.com/watch?v="+domain.ID()[:11])
	frameID := domain.ID()
	storageKey := "frames/" + frameID + ".jpg"
	frame := domain.Asset{
		ID: frameID, SnapshotID: r.SnapshotID, SourceID: c.Run.SourceID, RunID: c.Run.ID,
		Kind: "frame", StorageKey: storageKey, SHA256: strings.Repeat("a", 64), MIME: "image/jpeg",
		Bytes: 12, TimeMS: domain.Ptr(int64(1500)), Width: 32, Height: 18,
	}
	if e := s.AddAsset(ctx, c, frame); e != nil {
		t.Fatal(e)
	}
	article := domain.Article{
		SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: title,
		Source:    domain.Source{Platform: "youtube", CanonicalURL: "https://youtube.com/watch?v=abcdefghijk", Title: domain.Ptr(title), DurationMS: domain.Ptr(int64(1000))},
		Sections:  []domain.Section{{ID: "section", Title: section, Blocks: []domain.Block{{ID: "figure", Type: "figure", Origin: "model", FrameIDs: []string{frameID}}}}},
		Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{},
	}
	if e := s.Publish(ctx, c, article); e != nil {
		t.Fatal(e)
	}
	r, e := s.Request(ctx, owner, r.ID)
	if e != nil || r.ArticleID == nil {
		t.Fatal(e)
	}
	return publishedExample{articleID: *r.ArticleID, frameID: frameID, storageKey: storageKey}
}

func notFound(e error) bool {
	var p *domain.Problem
	return errors.As(e, &p) && p.Status == 404
}

func invalid(e error) bool {
	var p *domain.Problem
	return errors.As(e, &p) && p.Status == 400
}
