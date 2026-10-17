package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func verifiedFixtureRoute(t *testing.T, s *store.Store, name, endpoint, upstream string, limits ...int) store.CatalogModel {
	t.Helper()
	ctx := context.Background()
	p, err := s.SaveProvider(ctx, "", store.ProviderInput{Name: name, BaseURL: endpoint, APIKey: "fixture-" + name, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	in := store.ModelInput{ProviderID: p.ID, DisplayName: "GLM 5.3 Flash", BrandID: "glm", UpstreamID: upstream}
	if len(limits) == 2 {
		in.MaxOutputTokens, in.MaxInputChars = limits[0], limits[1]
	}
	m, err := s.SaveModel(ctx, "", in)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", domain.ID())
	if err != nil {
		t.Fatal(err)
	}
	probe, err := s.ClaimProbe(ctx, a.ID)
	if err != nil || probe == nil {
		t.Fatal(err)
	}
	if err = s.FinishProbe(ctx, *probe, map[string]bool{"synthetic_fixture_only": true}, ""); err != nil {
		t.Fatal(err)
	}
	m, err = s.SaveModel(ctx, m.ID, store.ModelInput{ProviderID: m.ProviderID, DisplayName: m.DisplayName, BrandID: m.BrandID, UpstreamID: m.UpstreamID, Enabled: true, ExpectedRevision: m.Revision, MaxOutputTokens: m.MaxOutputTokens, MaxInputChars: m.MaxInputChars, CachePolicy: m.CachePolicy, DisableThinking: m.DisableThinking})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMeteredModelFailoverKeepsDurableActualSupplier(t *testing.T) {
	for _, fault := range []string{"http_401", "http_403", "http_429", "http_503", "broken_stream", "empty_response", "caller_cancels", "output_limit", "all_fail", "resume_unknown", "backup_output_limit", "backup_context_limit"} {
		t.Run(fault, func(t *testing.T) {
			s, resources, cfg := testsupport.Services(t)
			ctx := context.Background()
			callctx, cancel := context.WithCancel(ctx)
			defer cancel()
			var primaryCalls, backupCalls, wrongCalls atomic.Int32
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				primaryCalls.Add(1)
				if r.Header.Get("Authorization") != "Bearer fixture-primary" {
					t.Error("wrong primary credential")
				}
				switch fault {
				case "empty_response":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"completion_tokens\":52}}\n\ndata: [DONE]\n\n")
				case "caller_cancels":
					cancel()
					w.WriteHeader(503)
				case "broken_stream":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"discard partial\"}}]}\n\n")
				case "output_limit":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"truncated\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
				case "http_401":
					w.WriteHeader(401)
				case "http_403":
					w.WriteHeader(403)
				case "http_429":
					w.Header().Set("Retry-After", "3600")
					w.WriteHeader(429)
				default:
					w.WriteHeader(503)
				}
			}))
			defer primary.Close()
			backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				backupCalls.Add(1)
				if r.Header.Get("Authorization") != "Bearer fixture-backup" {
					t.Error("wrong backup credential")
				}
				var body struct {
					Model     string `json:"model"`
					MaxTokens int    `json:"max_tokens"`
					Stream    bool   `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Model != "z-ai/glm-5.3-flash" || body.MaxTokens != cfg.Profile.MaxOutputTokens || !body.Stream {
					t.Errorf("fallback changed identity/budget/protocol: %+v", body)
				}
				if fault == "all_fail" {
					w.WriteHeader(503)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"backup-response\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"complete backup\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3,\"total_tokens\":13}}\n\ndata: [DONE]\n\n")
			}))
			defer backup.Close()
			wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { wrongCalls.Add(1); w.WriteHeader(500) }))
			defer wrong.Close()
			a := verifiedFixtureRoute(t, s, "primary", primary.URL, "glm-5.3-flash")
			var limits []int
			if fault == "backup_output_limit" {
				limits = []int{1024, 180000}
			} else if fault == "backup_context_limit" {
				limits = []int{10000, 10000}
			}
			b := verifiedFixtureRoute(t, s, "backup", backup.URL, "z-ai/glm-5.3-flash", limits...)
			verifiedFixtureRoute(t, s, "wrong", wrong.URL, "glm-5.4-flash")
			owner, err := s.NewSession(ctx, domain.ID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := s.CreateRequestWithInput(ctx, owner.ID, domain.ID(), domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", ModelID: a.ID, ModelRevisionID: a.RevisionID}, "", false)
			if err != nil {
				t.Fatal(err)
			}
			req, err := s.Request(ctx, owner.ID, accepted.ID)
			if err != nil {
				t.Fatal(err)
			}
			claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "model-fixture")
			if err != nil || claim == nil {
				t.Fatal(err)
			}
			// Calling at the real metering seam needs no media or generated article.
			// The claim, resource lane, ledger, SDK and HTTP transport are real.
			p, err := s.Profile(ctx, claim.Run.ConfigID)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Profile = p
			sess := &session{engine: &Engine{Store: s, Resources: resources, Config: cfg}, claim: *claim, profile: p}
			metered := &meteredModel{session: sess}
			messages := []*schema.Message{schema.UserMessage("complete context retained")}
			if fault == "resume_unknown" {
				if _, err = s.BeginModelSubmission(ctx, *claim, "model:0", stripPixels(messages), claim.Run.ConfigID); err != nil {
					t.Fatal(err)
				}
			}
			result, err := metered.Generate(callctx, messages)
			if fault == "caller_cancels" || fault == "output_limit" || fault == "backup_output_limit" || fault == "backup_context_limit" {
				if err == nil || backupCalls.Load() != 0 || primaryCalls.Load() != 1 {
					t.Fatalf("local terminal result switched suppliers: %v %d/%d", err, primaryCalls.Load(), backupCalls.Load())
				}
				if fault == "caller_cancels" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			if fault == "all_fail" {
				if err == nil || primaryCalls.Load() != 1 || backupCalls.Load() != 1 {
					t.Fatalf("did not exhaust suppliers once: %v %d/%d", err, primaryCalls.Load(), backupCalls.Load())
				}
				if _, err = (&meteredModel{session: sess}).Generate(ctx, messages); err == nil || primaryCalls.Load()+backupCalls.Load() != 3 {
					t.Fatalf("recovery reset submission budget: %v %d/%d", err, primaryCalls.Load(), backupCalls.Load())
				}
				return
			}
			if err != nil || result.Content != "complete backup" || backupCalls.Load() != 1 || wrongCalls.Load() != 0 {
				t.Fatalf("wrong failover result=%+v err=%v calls=%d/%d/%d", result, err, primaryCalls.Load(), backupCalls.Load(), wrongCalls.Load())
			}
			wantPrimary := int32(1)
			if fault == "resume_unknown" {
				wantPrimary = 0
			}
			if primaryCalls.Load() != wantPrimary {
				t.Fatal("recovery repeated an unknown primary submission")
			}
			if _, err = metered.Generate(ctx, messages); err != nil || backupCalls.Load() != 1 {
				t.Fatal("completed operation called supplier again", err)
			}
			sess.cp.Calls++
			if _, err = metered.Generate(ctx, messages); err != nil || backupCalls.Load() != 2 || primaryCalls.Load() != wantPrimary {
				t.Fatal("next Agent step lost successful backup", err)
			}
			var actual string
			if err = s.Pool.QueryRow(ctx, "SELECT reporting->>'provider_id' FROM model_submissions WHERE run_id=$1 AND operation_key='model:0' AND submission=2", claim.Run.ID).Scan(&actual); err != nil || actual != b.ProviderID {
				t.Fatalf("actual supplier audit lost: %s %v", actual, err)
			}
			if fault == "empty_response" {
				var empty bool
				var stream string
				if err = s.Pool.QueryRow(ctx, "SELECT (reporting->>'response_empty')::boolean, reporting->>'stream_status' FROM model_submissions WHERE run_id=$1 AND operation_key='model:0' AND submission=1", claim.Run.ID).Scan(&empty, &stream); err != nil || !empty || stream != "complete" || sess.cp.Repairs != 0 {
					t.Fatal("empty response lost its completed stream report or consumed content repair", empty, stream, err)
				}
			}
		})
	}
}
