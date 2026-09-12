package eval

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type productDeadlineTransport func(*http.Request) (*http.Response, error)

func (f productDeadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProductHTTPDeadlineAllowsMaximumRunBudgetAndCallerCancellation(t *testing.T) {
	prior := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = prior })
	var remaining time.Duration
	http.DefaultTransport = productDeadlineTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("evaluation request has no bounded deadline")
		}
		remaining = time.Until(deadline)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	h := HTTPProductExecutor{BaseURL: "http://product.invalid"}
	resp, err := h.request(context.Background(), http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if remaining <= 15*time.Minute || remaining > 16*time.Minute {
		t.Fatalf("deadline %s cannot cover maximum run plus final persistence", remaining)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err = h.request(ctx, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if remaining > time.Second {
		t.Fatalf("caller deadline was extended: %s", remaining)
	}
}

func TestProductHTTPUsesJournalInsteadOfDoneForClassification(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing auth")
		}
		if r.Method == "POST" {
			posts++
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: reasoning\ndata: {\"delta\":\"private\"}\n\nevent: run_start\ndata: {\"run_id\":\"run-1\"}\n\nevent: answer\ndata: \"partial\"\n\nevent: done\ndata: {\"run_id\":\"run-1\",\"message_id\":2,\"answer\":\"partial\"}\n\n")
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/runs/run-1") {
			t.Error(r.URL.Path)
		}
		fmt.Fprint(w, `{"code":200,"data":{"status":"budget_exhausted","stop_reason":"max_prompt_tokens","token_source":"unknown","prompt_tokens":0}}`)
	}))
	defer server.Close()
	h := HTTPProductExecutor{BaseURL: server.URL, Token: "fixture"}
	ob, err := h.Execute(context.Background(), ProductTurn{SessionID: 1, Kind: "agent", Question: "q", Stream: true})
	if err != nil || posts != 1 || ClassifyProductObservation(ob, err) != "limited" || ob.FirstAnswerMS == nil || ob.FirstProgressMS != nil || ob.Usage.PromptTokens != nil {
		t.Fatalf("%+v %v posts=%d", ob, err, posts)
	}
}

func TestProductHTTPInterruptedStreamNeverRetries(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: answer\ndata: \"partial\"\n\n")
	}))
	defer server.Close()
	ob, err := (HTTPProductExecutor{BaseURL: server.URL}).Execute(context.Background(), ProductTurn{SessionID: 1, Kind: "agent", Question: "q", Stream: true})
	if err == nil || posts != 1 || ClassifyProductObservation(ob, err) != "failed" {
		t.Fatalf("%+v %v calls=%d", ob, err, posts)
	}
}

func TestProductHTTPSSEErrorReadsJournalWithoutHidingOriginalFailure(t *testing.T) {
	for _, status := range []string{"cancelled", "failed", "completed", "unavailable"} {
		t.Run(status, func(t *testing.T) {
			posts, gets := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "event: run_start\ndata: {\"run_id\":\"failed-run\"}\n\nevent: error\ndata: {\"message\":\"provider failure\"}\n\n")
					return
				}
				gets++
				if status == "unavailable" {
					http.Error(w, "offline", 503)
					return
				}
				fmt.Fprintf(w, `{"code":200,"data":{"status":%q,"stop_reason":"request_cancelled","token_source":"actual","prompt_tokens":123,"completion_tokens":45}}`, status)
			}))
			defer server.Close()
			ob, err := (HTTPProductExecutor{BaseURL: server.URL}).Execute(context.Background(), ProductTurn{SessionID: 1, Kind: "agent", Question: "q", Stream: true})
			if err == nil || !strings.Contains(err.Error(), "stream reported execution error") || posts != 1 || gets != 1 || ob.MessageID != 0 {
				t.Fatalf("error lost/replayed: %+v %v posts=%d gets=%d", ob, err, posts, gets)
			}
			if status == "unavailable" {
				if ob.Status != "unknown" || ob.Usage.Source != "unknown" || ob.Usage.PromptTokens != nil {
					t.Fatalf("invented journal result %+v", ob)
				}
				return
			}
			if ob.Status != status || ob.StopReason != "request_cancelled" || ob.Usage.Source != "actual" || ob.Usage.PromptTokens == nil || *ob.Usage.PromptTokens != 123 || *ob.Usage.CompletionTokens != 45 {
				t.Fatalf("missing journal %+v", ob)
			}
			if ClassifyProductObservation(ob, err) == "completed" {
				t.Fatal("journal completion hid original execution error")
			}
		})
	}
}

func TestProductHTTPCancelledCallerStillPerformsOneBoundedJournalRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	posts, gets := 0, 0
	client := &http.Client{Transport: productDeadlineTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posts++
			cancel()
			return nil, context.Canceled
		}
		gets++
		if r.Context().Err() != nil {
			t.Fatal("journal inherited cancelled context")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("journal read is not bounded")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{"status":"cancelled","stop_reason":"request_cancelled","token_source":"estimated","prompt_tokens":12,"completion_tokens":3}}`))}, nil
	})}
	ob, err := (HTTPProductExecutor{BaseURL: "http://fixture.invalid", Client: client}).Execute(ctx, ProductTurn{SessionID: 1, RunID: "known-run", Kind: "agent", Question: "q", Stream: true})
	if !errors.Is(err, context.Canceled) || posts != 1 || gets != 1 || ob.Status != "cancelled" || ob.Usage.Source != "estimated" {
		t.Fatalf("%+v %v posts=%d gets=%d", ob, err, posts, gets)
	}
}

func TestProductHTTPSSECitationsSurviveDoneWithoutCitations(t *testing.T) {
	const citations = `[{"citation_id":"C2","task_id":38,"evidence_id":"ev-second","chunk_id":2,"modality":"visual_caption","start_ms":5000,"end_ms":6000},{"citation_id":"C1","task_id":38,"evidence_id":"ev-first","chunk_id":1,"modality":"visual_caption","start_ms":1000,"end_ms":2000}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: run_start\ndata: {\"run_id\":\"citation-run\"}\n\nevent: citations\ndata: "+citations+"\n\nevent: done\ndata: {\"message_id\":94,\"answer\":\"saved answer\"}\n\n")
			return
		}
		fmt.Fprint(w, `{"code":200,"data":{"status":"completed","stop_reason":"budget_finalized","token_source":"unknown"}}`)
	}))
	defer server.Close()
	ob, err := (HTTPProductExecutor{BaseURL: server.URL}).Execute(context.Background(), ProductTurn{SessionID: 60, Kind: "agent", Question: "q", Stream: true})
	if err != nil || string(ob.Citations) != citations || ob.MessageID != 94 || ob.RunID != "citation-run" {
		t.Fatalf("citations lost/reordered: %+v %v", ob, err)
	}
}

func TestProductHTTPSSERejectsInvalidCitationPayload(t *testing.T) {
	for _, payload := range []string{`[{"citation_id":`, `{"citation_id":"not-array"}`, `["not-object"]`} {
		t.Run(payload, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: citations\ndata: "+payload+"\n\nevent: done\ndata: {\"message_id\":94,\"answer\":\"saved\"}\n\n")
			}))
			defer server.Close()
			ob, err := (HTTPProductExecutor{BaseURL: server.URL}).Execute(context.Background(), ProductTurn{SessionID: 60, Kind: "agent", Question: "q", Stream: true})
			if err == nil || len(ob.Citations) > 0 {
				t.Fatalf("invalid citations accepted: %+v %v", ob, err)
			}
		})
	}
}

func TestProductHTTPInvalidDoneCitationsCannotCorruptEarlierSnapshot(t *testing.T) {
	const citations = `[{"citation_id":"C1","task_id":38}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: citations\ndata: "+citations+"\n\nevent: done\ndata: {\"message_id\":94,\"answer\":\"saved\",\"citations\":[0]}\n\n")
	}))
	defer server.Close()
	ob, err := (HTTPProductExecutor{BaseURL: server.URL}).Execute(context.Background(), ProductTurn{SessionID: 60, Kind: "agent", Question: "q", Stream: true})
	if err == nil || string(ob.Citations) != citations {
		t.Fatalf("earlier valid citations corrupted: %s %v", ob.Citations, err)
	}
}
