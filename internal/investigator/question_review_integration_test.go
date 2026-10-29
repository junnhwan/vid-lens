package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestCompactQuestionReviewUsesPixelsAndFailsClosed(t *testing.T) {
	for _, fault := range []string{"approved", "not_assessed", "supported_with_issues", "empty"} {
		t.Run(fault, func(t *testing.T) {
			var calls atomic.Int32
			s, article := publicationReviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var input reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					return
				}
				payload := reviewWirePayload(t, input)
				var mode, id string
				var units []reviewUnit
				_ = json.Unmarshal(payload["review_mode"], &mode)
				_ = json.Unmarshal(payload["request_id"], &id)
				_ = json.Unmarshal(payload["units"], &units)
				wire := string(domain.JSON(input))
				if mode != "multimodal" || strings.Count(wire, "data:image/jpeg;") != 1 || !strings.Contains(wire, "来源事实0") || len(units) != 3 {
					t.Error("short question lost complete text/pixel/unit coverage", mode, len(units))
				}
				if fault == "empty" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"internal only\"},\"finish_reason\":\"stop\"}],\"usage\":{\"completion_tokens\":16000}}\n\ndata: [DONE]\n\n")
					return
				}
				report := reviewUnitReport{RequestID: id, Results: []reviewUnitResult{}}
				for _, unit := range units {
					result := reviewUnitResult{UnitID: unit.ID, Verdict: "supported", Issues: []reviewUnitIssue{}}
					if unit.Path == "" && strings.Contains(string(domain.JSON(unit.Value)), "合成黑色画面") && (fault == "not_assessed" || fault == "supported_with_issues") {
						if fault == "not_assessed" {
							result.Verdict = fault
						}
						result.Issues = []reviewUnitIssue{{Claim: "合成黑色画面", Reason: "这张图无法确认所述画面含义，需要补充来源。", CitationIDs: []string{"f1"}}}
					}
					report.Results = append(report.Results, result)
				}
				writeReviewWireArgs(w, reviewUnitsTool, report)
			})
			s.cp.ExecutionContract.Version = domain.ExecutionContractV9
			s.cp.ExecutionContract.InputRevision = domain.QuestionReadingV5
			s.turn = &domain.Turn{Question: "请解释这张图"}
			ctx := context.Background()
			catalog, err := s.citationCatalog(ctx)
			if err != nil {
				t.Fatal(err)
			}
			answer := domain.Answer{Kind: "grounded", Summary: "来源解释了一张画面。", Blocks: []domain.Block{
				{ID: "text", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "来源事实0。"}, {Type: "citation", CitationID: "t1"}}},
				article.Sections[0].Blocks[1],
			}, Citations: map[string]domain.Citation{"f1": catalog["f1"], "t1": catalog["t1"]}, Limitations: []domain.Limitation{}}
			err = s.reviewAnswer(ctx, answer)
			if calls.Load() != 1 {
				t.Fatal("avoidable review resend", calls.Load(), err)
			}
			if fault == "approved" {
				if err != nil {
					t.Fatal(err)
				}
				if err = s.requireAnswerPublicationReview(ctx, answer); err != nil {
					t.Fatal(err)
				}
				answer.Summary = "Changed after review"
				if err = s.requireAnswerPublicationReview(ctx, answer); err == nil {
					t.Fatal("changed answer bypassed the gate")
				}
			} else {
				if err == nil {
					t.Fatal("uncertain or empty review was approved")
				}
				if fault == "empty" {
					var problem *domain.Problem
					if !errors.As(err, &problem) || problem.Status != 502 || problem.Meta["can_retry"] != true {
						t.Fatal("empty review did not stop with an explicit retryable failure", err)
					}
				} else {
					var correction *candidateError
					if !errors.As(err, &correction) {
						t.Fatal("actionable review became a format retry", err)
					}
				}
				if err = s.requireAnswerPublicationReview(ctx, answer); err == nil {
					t.Fatal("unapproved answer bypassed the gate")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("publication made an unexpected model call")
			}
		})
	}
}
