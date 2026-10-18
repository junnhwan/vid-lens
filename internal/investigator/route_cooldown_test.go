package investigator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestConfiguredRoutePriorityDoesNotRepeatRecentGatewayFailure(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	var primaryCalls, backupCalls atomic.Int32
	var recovered atomic.Bool
	respond := func(w http.ResponseWriter, text string) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
	}
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		if !recovered.Load() {
			w.WriteHeader(504)
			return
		}
		respond(w, "primary recovered")
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { backupCalls.Add(1); respond(w, "complete backup") }))
	defer backup.Close()
	a := verifiedFixtureRoute(t, db, "primary", primary.URL, "glm-5.3-flash")
	b := verifiedFixtureRoute(t, db, "backup", backup.URL, "z-ai/glm-5.3-flash")
	if _, err := db.Pool.Exec(ctx, "UPDATE catalog_models SET route_priority=CASE WHEN id=$1 THEN 1 ELSE 2 END WHERE id=ANY($2)", a.ID, []string{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := db.CreateRequestWithInput(ctx, owner.ID, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", ModelID: a.ID, ModelRevisionID: a.RevisionID}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, err := db.Request(ctx, owner.ID, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := db.Claim(ctx, *req.ActiveRunID, 1, "cooldown-test")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	p, err := db.Profile(ctx, claim.Run.ConfigID)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{engine: &Engine{Store: db, Resources: res, Config: cfg}, claim: *claim, profile: p}
	messages := []*schema.Message{schema.UserMessage("complete frozen source context")}
	for call := 0; call < 2; call++ {
		s.cp.Calls = call
		result, err := (&meteredModel{session: s}).Generate(ctx, messages)
		if err != nil || result.Content != "complete backup" {
			t.Fatal("healthy exact-model fallback failed", result, err)
		}
	}
	if primaryCalls.Load() != 1 || backupCalls.Load() != 2 {
		t.Fatalf("configured priority repeated the broken gateway: primary=%d backup=%d", primaryCalls.Load(), backupCalls.Load())
	}
	// Expiring the bounded cooldown re-enables the owner's preferred route.
	if _, err := db.Pool.Exec(ctx, "UPDATE model_submissions SET reporting=reporting-'route_cooldown_until' WHERE run_id=$1", claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	recovered.Store(true)
	s.cp.Calls = 2
	result, err := (&meteredModel{session: s}).Generate(ctx, messages)
	if err != nil || result.Content != "primary recovered" || primaryCalls.Load() != 2 {
		t.Fatal("temporary failure permanently changed owner priority", result, err)
	}
}
