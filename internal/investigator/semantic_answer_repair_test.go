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

func TestSemanticAnswerCorrectionRetainsAUsableSubmissionPath(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	var writers atomic.Int32
	var sourceQuestion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		var tools []struct{ Function struct{ Name string } }
		_ = json.Unmarshal(body["tools"], &tools)
		if len(tools) == 1 && tools[0].Function.Name == reviewUnitsTool {
			var in reviewWireRequest
			_ = json.Unmarshal(domain.JSON(body), &in)
			payload := reviewWirePayload(t, in)
			var id string
			var candidate struct{ Summary string }
			var units []reviewUnit
			_ = json.Unmarshal(payload["request_id"], &id)
			_ = json.Unmarshal(payload["candidate_context"], &candidate)
			_ = json.Unmarshal(payload["units"], &units)
			questionSeen := false
			for _, message := range in.Messages {
				var text string
				var source struct{ Question string }
				if json.Unmarshal(message.Content, &text) == nil && json.Unmarshal([]byte(text), &source) == nil {
					questionSeen = questionSeen || source.Question == sourceQuestion && sourceQuestion != ""
				}
			}
			if !questionSeen {
				t.Error("independent unit review lost the actual user's question")
			}
			report := reviewUnitReport{RequestID: id, Results: []reviewUnitResult{}}
			for _, unit := range units {
				result := reviewUnitResult{UnitID: unit.ID, Verdict: "supported", Issues: []reviewUnitIssue{}}
				if candidate.Summary == "视频证明了完全一致。" && unit.Value == candidate.Summary {
					result.Verdict = "contradicted"
					result.Issues = []reviewUnitIssue{{Claim: candidate.Summary, Reason: "原文不能支持完全一致这一结论。", CitationIDs: []string{"t1"}}}
				}
				report.Results = append(report.Results, result)
			}
			writeReviewWireArgs(w, reviewUnitsTool, report)
			return
		}
		if len(tools) == 1 && tools[0].Function.Name == "report_evidence_review" {
			var messages []struct{ Content json.RawMessage }
			_ = json.Unmarshal(body["messages"], &messages)
			questionSeen := false
			for _, message := range messages {
				var text string
				var payload struct {
					Question      string                   `json:"question"`
					CandidateHash string                   `json:"candidate_hash"`
					Candidate     struct{ Summary string } `json:"candidate"`
				}
				if json.Unmarshal(message.Content, &text) != nil || json.Unmarshal([]byte(text), &payload) != nil {
					continue
				}
				questionSeen = questionSeen || payload.Question == sourceQuestion && sourceQuestion != ""
				if payload.CandidateHash != "" && payload.Candidate.Summary == "视频证明了完全一致。" {
					if !questionSeen {
						t.Error("independent answer review lost the actual user's question")
					}
					args := evidenceReview{CandidateHash: payload.CandidateHash, Verdict: "needs_correction", Issues: []evidenceReviewIssue{{Path: "/summary", Reason: "原文不能支持完全一致这一结论。", CitationIDs: []string{"t1"}}}}
					chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "source-verdict", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
					return
				}
			}
			if !questionSeen {
				t.Error("corrected answer review lost the actual user's question")
			}
			if respondSupportedEvidenceReview(t, w, body) {
				return
			}
		}
		call := writers.Add(1)
		if call > 1 {
			found := false
			for _, tool := range tools {
				if tool.Function.Name == "repair_candidate" {
					t.Error("semantic answer correction exposed the fragile draft-field patch path")
				}
				found = found || tool.Function.Name == "submit_answer"
			}
			if !found {
				t.Error("semantic answer correction lost formal submission")
			}
		}
		summary := "视频证明了完全一致。"
		if call > 1 {
			summary = "现有材料不足以判断。"
		}
		args := map[string]any{"kind": "insufficient_evidence", "summary": summary, "blocks": []domain.Block{{ID: "reason", Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "text", Text: "现有材料不能支持更具体的判断。"}}}}, "parts": []domain.AnswerPart{}, "limitations": []domain.Limitation{}}
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "answer", "type": "function", "function": map[string]any{"name": "submit_answer", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	cfg.Profile.ModelBaseURL = server.URL
	if _, err := db.SaveConfig(context.Background(), cfg.Profile, "fixture-key", ""); err != nil {
		t.Fatal(err)
	}
	_, claim := testsupport.Question(t, db)
	question, err := db.TurnForRun(context.Background(), claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	sourceQuestion = question.Question
	_, cfg.Profile, _ = db.CurrentConfig(context.Background())
	engine := Engine{Store: db, Resources: res, Config: cfg}
	if err := engine.Run(context.Background(), claim, cfg.Profile); err != nil {
		t.Fatal(err)
	}
	turn, err := db.TurnForRun(context.Background(), claim.Run.ID)
	if err != nil || turn.Answer == nil || turn.Answer.Summary != "现有材料不足以判断。" || writers.Load() != 2 {
		t.Fatal("reviewed answer did not complete through normal publication", turn, writers.Load(), err)
	}
	var submissions int
	if err = db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM model_submissions WHERE run_id=$1", claim.Run.ID).Scan(&submissions); err != nil || submissions != 4 {
		t.Fatal("writer correction and both independent verdicts lost their separate accounting", submissions, err)
	}
}
