package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestQuestionRequiresExecutableActionOnFirstSubmission(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if respondSupportedEvidenceReview(t, w, body) {
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if string(body["tool_choice"]) != `"required"` {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"This plain answer cannot be published.\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		args := map[string]any{"kind": "insufficient_evidence", "summary": "视频没有提供这一细节。", "blocks": []domain.Block{{ID: "reason", Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "text", Text: "现有材料不能支持更具体的判断。"}}}}, "parts": []domain.AnswerPart{{Title: "证据不足在哪里", BlockIDs: []string{"reason"}}}, "limitations": []domain.Limitation{}}
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "answer-once", "type": "function", "function": map[string]any{"name": "submit_answer", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	cfg.Profile.ModelBaseURL = server.URL
	if _, err := db.SaveConfig(context.Background(), cfg.Profile, "fixture-key", ""); err != nil {
		t.Fatal(err)
	}
	turn, claim := testsupport.Question(t, db)
	_, cfg.Profile, _ = db.CurrentConfig(context.Background())
	engine := Engine{Store: db, Resources: res, Config: cfg}
	if err := engine.Run(context.Background(), claim, cfg.Profile); err != nil {
		t.Fatal(err)
	}
	result, err := db.TurnForRun(context.Background(), claim.Run.ID)
	if err != nil || result.Answer == nil || len(result.Answer.Parts) != 1 || calls.Load() != 1 {
		t.Fatalf("question %s needed avoidable follow-up: calls=%d error=%v", turn.ID, calls.Load(), err)
	}
	var reviews, submissions int
	if err = db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='content_review' AND status='completed'", claim.Run.ID).Scan(&reviews); err != nil || reviews != 1 {
		t.Fatal("new private answer bypassed independent review", reviews, err)
	}
	if err = db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM model_submissions WHERE run_id=$1", claim.Run.ID).Scan(&submissions); err != nil || submissions != 2 {
		t.Fatal("writer and reviewer submissions were not accounted separately", submissions, err)
	}
}
