package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestFocusedQuestionPublishesWithOneWriterAndOneReviewer(t *testing.T) {
	db, resources, cfg := testsupport.Services(t)
	var writers, reviews atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if respondSupportedEvidenceReview(t, w, body) {
			reviews.Add(1)
			return
		}
		writers.Add(1)
		if !strings.Contains(string(body["messages"]), "fixture text") || !strings.Contains(string(body["messages"]), "already read") {
			t.Error("writer did not receive original source on its first call")
		}
		writeReviewWireArgs(w, "submit_answer", map[string]any{"kind": "grounded", "summary": "原文给出了这句示例。", "blocks": []any{map[string]any{"id": "source", "type": "quote", "origin": "video", "text": "fixture text", "quote": nil, "citation_ids": []string{"t1"}}}, "limitations": []any{}})
	}))
	defer server.Close()
	cfg.Profile.ModelBaseURL = server.URL
	if _, err := db.SaveConfig(context.Background(), cfg.Profile, "fixture-key", ""); err != nil {
		t.Fatal(err)
	}
	_, claim := testsupport.Question(t, db)
	// The shared helper publishes a historical fixture directly. Freeze the new
	// question input at this test's worker boundary; T14 covers current admission.
	var cp checkpoint
	if err := json.Unmarshal(claim.Run.Checkpoint, &cp); err != nil {
		t.Fatal(err)
	}
	cp.ExecutionContract.InputRevision = domain.QuestionReadingV4
	claim.Run.Checkpoint = domain.JSON(cp)
	if _, err := db.Pool.Exec(context.Background(), "UPDATE job_runs SET checkpoint=$2 WHERE id=$1", claim.Run.ID, claim.Run.Checkpoint); err != nil {
		t.Fatal(err)
	}
	_, cfg.Profile, _ = db.CurrentConfig(context.Background())
	engine := Engine{Store: db, Resources: resources, Config: cfg}
	if err := engine.Run(context.Background(), claim, cfg.Profile); err != nil {
		t.Fatal(err)
	}
	turn, err := db.TurnForRun(context.Background(), claim.Run.ID)
	if err != nil || turn.Answer == nil {
		t.Fatal(err)
	}
	if writers.Load() != 1 || reviews.Load() != 1 {
		t.Fatalf("avoidable calls: writer=%d review=%d", writers.Load(), reviews.Load())
	}
	evidence := turn.Answer.Evidence
	if evidence == nil || evidence.Review != "passed" || len(evidence.ViewedFrameIDs) != 0 || len(evidence.TextSources) != 1 || evidence.TextSources[0].Text != "fixture text" {
		t.Fatalf("incorrect provenance: %+v", evidence)
	}
	var sends int
	if err := db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM model_submissions WHERE run_id=$1", claim.Run.ID).Scan(&sends); err != nil || sends != 2 {
		t.Fatalf("physical sends %d: %v", sends, err)
	}
}
