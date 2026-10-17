package investigator

import (
	"context"
	"strings"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestExplicitResumePreservesPacedHarnessAndObservationContract(t *testing.T) {
	db, _, cfg := testsupport.Services(t)
	ctx := context.Background()
	owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := db.Request(ctx, owner.ID, a.ID)
	c, err := db.Claim(ctx, *r.ActiveRunID, 1, "resume-policy")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	s := &session{engine: &Engine{Store: db}, claim: *c, profile: cfg.Profile, snapshot: domain.Snapshot{ID: "snapshot", DurationMS: 1000}, cp: checkpoint{SchemaVersion: 1, HarnessRevision: 5, Protocol: domain.ArticleDraftProtocol, LayoutVersion: draftLayout, Ready: true, DraftResume: true, Inspections: map[string][]string{}}}
	if err = s.resumeDraft(ctx); err != nil {
		t.Fatal(err)
	}
	if s.cp.HarnessRevision != 5 || !strings.Contains(s.cp.Messages[0].Content, "readiness.wrap_up=true") || !strings.Contains(s.cp.Messages[0].Content, "每个字段最多16项") {
		t.Fatalf("resume downgraded harness or dropped its instructions: revision=%d", s.cp.HarnessRevision)
	}
}
