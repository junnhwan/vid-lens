package investigator

import (
	"context"
	"testing"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestExternalCitationRequiresActualTurnSnapshotAndExactQuote(t *testing.T) {
	store, _, _ := testsupport.Services(t)
	turn, c := testsupport.Question(t, store)
	ctx := context.Background()
	r, e := store.RegisterReference(ctx, c, turn, "https://example.com/start", "candidate")
	if e != nil {
		t.Fatal(e)
	}
	s := &session{engine: &Engine{Store: store}, claim: c, turn: &turn}
	cite := domain.Citation{ID: "web", Kind: "external_reference", ReferenceID: r.ID, URL: r.URL, Title: r.Title, ContentSHA256: "hash", Quote: domain.Ptr("actual excerpt")}
	if s.validateCitation(ctx, cite, false) == nil {
		t.Fatal("unread search candidate was citeable")
	}
	if e = store.SaveReference(ctx, c, r.ID, "read", "Actual title", "https://example.com/final", "An actual excerpt in a durable snapshot.", "hash", false); e != nil {
		t.Fatal(e)
	}
	r, e = store.ReferenceForRun(ctx, c.Run.ID, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	cite.URL, cite.Title, cite.ReadAt = r.EvidenceURL(), r.Title, r.ReadAt
	if e = s.validateCitation(ctx, cite, false); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*domain.Citation){func(x *domain.Citation) { x.ContentSHA256 = "different" }, func(x *domain.Citation) { x.URL = "https://example.com/start" }, func(x *domain.Citation) { x.Quote = domain.Ptr("search snippet that never appeared") }, func(x *domain.Citation) { x.ReferenceID = domain.ID() }} {
		bad := cite
		change(&bad)
		if s.validateCitation(ctx, bad, false) == nil {
			t.Fatal("invented or cross-turn evidence was accepted")
		}
	}
	if s.validateCitation(ctx, cite, true) == nil {
		t.Fatal("private web evidence entered immutable article")
	}
	s.turn.AllowExternalSources = false
	if s.validateCitation(ctx, cite, false) == nil {
		t.Fatal("untrusted content could grant external permission")
	}
}
