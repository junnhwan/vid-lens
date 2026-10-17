package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestModelLedgerKeepsEveryPhysicalSendAndReplaysWithoutSending(t *testing.T) {
	db, resources, cfg := testsupport.Services(t)
	ctx := context.Background()
	var sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := sent.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	model := verifiedFixtureRoute(t, db, "ledger", server.URL, "glm-5.3-flash")
	owner, _ := db.NewSession(ctx, domain.ID(), time.Hour)
	a, err := db.CreateRequestWithInput(ctx, owner.ID, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", ModelID: model.ID, ModelRevisionID: model.RevisionID}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := db.Request(ctx, owner.ID, a.ID)
	c, err := db.Claim(ctx, *r.ActiveRunID, 1, "ledger")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	p, _ := db.Profile(ctx, c.Run.ConfigID)
	s := &session{engine: &Engine{Store: db, Resources: resources, Config: cfg}, claim: *c, profile: p}
	m := &meteredModel{session: s}
	input := []*schema.Message{schema.UserMessage("transport test")}
	result, err := m.Generate(ctx, input)
	if err != nil || result.Content != "complete" || sent.Load() != 2 {
		t.Fatalf("%v %v sends=%d", result, err, sent.Load())
	}
	if _, err = m.Generate(ctx, input); err != nil || sent.Load() != 2 {
		t.Fatal("replay resubmitted", err)
	}
	rows, err := db.Pool.Query(ctx, `SELECT step FROM (SELECT DISTINCT ON(step_id) step,seq FROM run_events WHERE run_id=$1 AND step_id IS NOT NULL ORDER BY step_id,seq DESC) x ORDER BY seq`, c.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	calls, transports, failures := 0, 0, 0
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var step domain.Step
		if err = json.Unmarshal(raw, &step); err != nil {
			t.Fatal(err)
		}
		if step.Kind == "model_response" {
			calls++
			if step.Title != "第 1 次模型调用" || step.Status != "completed" {
				t.Fatal(step)
			}
		}
		if step.Kind == "model_submission" {
			transports++
			if step.Status == "failed" {
				failures++
				if !strings.Contains(step.Outcome.Message, "完整") {
					t.Fatal(step)
				}
			}
			if step.FinishedAt == nil {
				t.Fatal("send left running")
			}
		}
	}
	if calls != 1 || transports != 2 || failures != 1 {
		t.Fatalf("calls=%d sends=%d failures=%d", calls, transports, failures)
	}
	stats, err := db.ModelExecution(ctx, c.Run.ID)
	if err != nil || stats.Calls != 1 || stats.Submissions != 2 {
		t.Fatal(stats, err)
	}
}
