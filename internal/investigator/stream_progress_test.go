package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestArticleResponsePolicyLetsReasoningFinishBeyondLegacyWindow(t *testing.T) {
	s := &session{profile: config.DefaultProfile(), cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}}}
	s.initializeArticleLoop()
	limits := s.responseLimits()
	if limits.Total != 600*time.Second {
		t.Errorf("new article still has the old hard cutoff: %v", limits.Total)
	}
	limits.Total /= 1000
	limits.First /= 1000
	limits.Idle /= 1000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 24; i++ {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"private-stream-progress\"}}]}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(15 * time.Millisecond):
			}
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p := config.DefaultProfile()
	p.ModelBaseURL = server.URL
	base, err := newModelWithClient(context.Background(), p, "fixture", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := receiveModelResponse(context.Background(), base, []*schema.Message{schema.UserMessage("test")}, limits)
	if err != nil || result == nil || result.Content != "complete" {
		t.Fatalf("active reasoning was cut off: result=%v err=%v", result, err)
	}
	if strings.Contains(string(domain.JSON(result)), "private-stream-progress") {
		t.Fatal("thinking leaked into result")
	}
}

func TestStreamActivityReceiptDistinguishesThinkingFromOutput(t *testing.T) {
	var got usageReport
	o := newUsageObserver("test", "provider_default", func(r usageReport) { got = r })
	o.consume([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n: heartbeat\n\n"))
	o.consume([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"private-reasoning-body\"}}]}\n\n"))
	time.Sleep(5 * time.Millisecond)
	o.consume([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"more-private-reasoning-body\"}}]}\n\n"))
	if o.report.FirstMeaningfulMS != nil {
		t.Fatal("reasoning became visible output")
	}
	o.finish(false)
	b := domain.JSON(got)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	for _, name := range []string{"first_activity_ms", "last_activity_ms", "first_reasoning_delta_ms", "last_reasoning_delta_ms", "reasoning_delta_count"} {
		if fields[name] == nil || string(fields[name]) == "null" {
			t.Errorf("missing activity receipt %s", name)
		}
	}
	var count int
	_ = json.Unmarshal(fields["reasoning_delta_count"], &count)
	if count != 2 {
		t.Errorf("wrong reasoning increment count: %d", count)
	}
	if strings.Contains(string(b), "private-reasoning-body") {
		t.Fatal("receipt persisted private thinking")
	}
}

func TestArticleStreamBudgetIsNotAnInactivityOrNarrowingFailure(t *testing.T) {
	s := &session{profile: config.DefaultProfile(), cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}}}
	s.initializeArticleLoop()
	limits := s.responseLimits()
	limits.Total = 100 * time.Millisecond
	limits.First = time.Second
	limits.Idle = time.Second
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"private\"}}]}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}))
	defer server.Close()
	p := config.DefaultProfile()
	p.ModelBaseURL = server.URL
	base, err := newModelWithClient(context.Background(), p, "fixture", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = receiveModelResponse(context.Background(), base, []*schema.Message{schema.UserMessage("test")}, limits)
	var work *domain.WorkError
	if !errors.As(err, &work) || work.Code != "MODEL_CALL_BUDGET_EXHAUSTED" || work.Transient {
		t.Fatalf("budget mislabeled as stalled/oversized input: %v", err)
	}
}

func TestArticleResponsePolicyKeepsLegacyAndHTTPWindowsConsistent(t *testing.T) {
	s := &session{profile: config.DefaultProfile(), cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}}}
	s.initializeArticleLoop()
	if err := s.validateArticleLoopState(); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), responsePolicyKey{}, s.responsePolicy())
	if modelHTTPTimeout(ctx) != s.responseLimits().Total || modelHTTPTimeout(ctx) != 600*time.Second {
		t.Fatal("HTTP still has a shorter absolute cutoff")
	}
	s.cp.ArticleLoop.Budget.ResponsePolicy = nil
	// A new v7 checkpoint requires its response policy. The pre-policy run being
	// restored here was frozen before v7 and must retain that revision as well.
	s.cp.ArticleLoop.ReviewRevision = domain.ArticleConvergenceReviewV6
	if err := s.validateArticleLoopState(); err != nil {
		t.Fatal("historical budget rejected", err)
	}
	if s.responseLimits().Total != 300*time.Second || !s.responseLimits().NarrowOnDeadline || s.responseLimits().BudgetDeadline || modelHTTPTimeout(context.Background()) != 300*time.Second {
		t.Fatal("old frozen behavior changed")
	}
}

func TestReasoningStreamStillStopsForIdleHeartbeatAndParentCancellation(t *testing.T) {
	for _, name := range []string{"heartbeat", "reasoning_then_idle", "parent_cancel"} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if name == "reasoning_then_idle" {
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"private\"}}]}\n\n")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				for {
					if name == "heartbeat" {
						fmt.Fprint(w, ": ping\n\n")
					} else {
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"private\"}}]}\n\n")
					}
					w.(http.Flusher).Flush()
					select {
					case <-r.Context().Done():
						return
					case <-time.After(10 * time.Millisecond):
					}
				}
			}))
			defer server.Close()
			p := config.DefaultProfile()
			p.ModelBaseURL = server.URL
			base, err := newModelWithClient(context.Background(), p, "fixture", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "parent_cancel" {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			_, err = receiveModelResponse(ctx, base, []*schema.Message{schema.UserMessage("test")}, modelResponseLimits{Total: time.Second, First: 60 * time.Millisecond, Idle: 60 * time.Millisecond, BudgetDeadline: true})
			if name == "parent_cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("parent cancellation ignored", err)
				}
				return
			}
			var work *domain.WorkError
			if !errors.As(err, &work) || work.Code != "PROVIDER_UNAVAILABLE" || !work.Transient {
				t.Fatal("idle/heartbeat mislabeled as active budget", err)
			}
		})
	}
}
