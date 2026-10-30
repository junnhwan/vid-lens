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

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestQuestionDeliveryPreviewAndFinalStates(t *testing.T) {
	for _, mode := range []string{"passed", "empty", "http_502", "mixed", "not_assessed", "cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			db, resources, cfg := testsupport.Services(t)
			var turn domain.Turn
			var owner string
			var writers, reviews atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
					return
				}
				isReview := len(in.Tools) == 1 && in.Tools[0].Function.Name == reviewUnitsTool
				if !isReview {
					writers.Add(1)
					blocks := []any{map[string]any{"id": "supported", "type": "quote", "origin": "video", "text": "fixture text", "citation_ids": []string{"t1"}}}
					if mode == "mixed" {
						blocks = append(blocks, map[string]any{"id": "unsupported", "type": "paragraph", "origin": "video", "text": "这段示例证明它是唯一的解决方法。", "citation_ids": []string{"t1"}})
					}
					writeReviewWireArgs(w, "submit_answer", map[string]any{"kind": "grounded", "summary": "原文给出了一条示例。", "blocks": blocks, "limitations": []any{}})
					return
				}
				reviews.Add(1)
				if !strings.Contains(string(domain.JSON(in.Messages)), "full_video_comparison") {
					t.Error("reviewer did not receive the host-owned comparison scope")
				}
				view, _, err := db.TurnView(context.Background(), owner, turn.ConversationID, turn.ID)
				if err != nil {
					t.Error(err)
					return
				}
				preview, ok := view["answer_preview"].(domain.Answer)
				if !ok || preview.Evidence == nil || preview.Evidence.Review != "pending" || !strings.Contains(preview.Summary, "参考解读") {
					t.Error("no visible pending answer before review", view)
				}
				before, err := db.TurnForRun(context.Background(), turn.RunID)
				if err != nil || before.Answer != nil {
					t.Error("preview was prematurely finalized", err)
				}
				if _, _, err = db.TurnView(context.Background(), "other-anonymous-browser", turn.ConversationID, turn.ID); err == nil {
					t.Error("private preview leaked to another browser")
				}
				switch mode {
				case "deadline":
					<-r.Context().Done()
					return
				case "http_502":
					w.WriteHeader(http.StatusBadGateway)
					fmt.Fprint(w, `{"error":{"message":"fixture review unavailable"}}`)
					return
				case "cancel":
					if err = db.CancelTurn(context.Background(), owner, turn.ConversationID, turn.ID); err != nil {
						t.Error(err)
					}
					cancel()
					return
				case "empty":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"not public\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					return
				case "mixed", "not_assessed":
					payload := reviewWirePayload(t, in)
					var requestID string
					_ = json.Unmarshal(payload["request_id"], &requestID)
					var units []reviewUnit
					if err = json.Unmarshal(payload["units"], &units); err != nil {
						t.Error(err)
						return
					}
					results := []reviewUnitResult{}
					for _, u := range units {
						v := reviewUnitResult{UnitID: u.ID, Verdict: "supported", Issues: []reviewUnitIssue{}}
						if mode == "not_assessed" {
							v.Verdict = "not_assessed"
						}
						if mode == "mixed" && strings.Contains(string(domain.JSON(u.Value)), "unsupported") {
							v.Verdict = "contradicted"
							v.Issues = []reviewUnitIssue{{Claim: "唯一的解决方法", Reason: "原文只给出示例，没有排除其他方法。", CitationIDs: []string{"t1"}}}
						}
						results = append(results, v)
					}
					writeReviewWireArgs(w, reviewUnitsTool, reviewUnitReport{RequestID: requestID, Results: results})
				default:
					reviewWireReply(t, w, in, false)
				}
			}))
			defer server.Close()
			cfg.Profile.ModelBaseURL = server.URL
			if _, err := db.SaveConfig(context.Background(), cfg.Profile, "fixture-key", ""); err != nil {
				t.Fatal(err)
			}
			var claim domain.Claim
			turn, claim = testsupport.CurrentQuestion(t, db)
			if err := db.Pool.QueryRow(context.Background(), "SELECT session_id FROM qa_conversations WHERE id=$1", turn.ConversationID).Scan(&owner); err != nil {
				t.Fatal(err)
			}
			_, cfg.Profile, _ = db.CurrentConfig(context.Background())
			engine := Engine{Store: db, Resources: resources, Config: cfg}
			if mode == "deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 2*time.Second)
				defer deadlineCancel()
			}
			err := engine.Run(ctx, claim, cfg.Profile)
			got, readErr := db.TurnForRun(context.Background(), claim.Run.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "cancel" || mode == "deadline" {
				if err == nil || got.Answer != nil {
					t.Fatal("cancellation published a fallback", err, got.Answer)
				}
				var receipts int
				if err = db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='question_delivery'", claim.Run.ID).Scan(&receipts); err != nil || receipts != 0 {
					t.Fatal("canceled review created a delivery receipt", receipts, err)
				}
			} else {
				if err != nil || got.Answer == nil {
					t.Fatal("answer lost", err)
				}
				want := "unavailable"
				if mode == "passed" {
					want = "passed"
				}
				if mode == "mixed" {
					want = "partial"
				}
				if got.Answer.Evidence == nil || got.Answer.Evidence.Review != want {
					t.Fatal("untruthful review state", got.Answer)
				}
				if len(got.Answer.Evidence.TextSources) != 1 || got.Answer.Evidence.TextSources[0].Text != "fixture text" {
					t.Fatal("original evidence lost", got.Answer.Evidence)
				}
				if mode == "mixed" && (len(got.Answer.Blocks) != 1 || got.Answer.Blocks[0].ID != "supported" || len(got.Answer.Evidence.Uncertainties) != 1 || !strings.Contains(got.Answer.Evidence.Uncertainties[0].Claim, "唯一的解决方法")) {
					t.Fatal("unsupported claim remained in confirmed body", got.Answer)
				}
			}
			expectedReviews := int32(1)
			if mode == "http_502" {
				expectedReviews = 2
			} // Existing bounded transport retry, not a new author/review loop.
			if writers.Load() != 1 || reviews.Load() != expectedReviews {
				t.Fatal("unexpected repair or extra model call", writers.Load(), reviews.Load())
			}
			var sends int
			if err = db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM model_submissions WHERE run_id=$1", claim.Run.ID).Scan(&sends); err != nil || sends != int(writers.Load()+reviews.Load()) {
				t.Fatal("unexpected physical sends", sends, err)
			}
		})
	}
}
