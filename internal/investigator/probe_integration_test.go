package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func TestCapabilityProbePreservesConfiguredOutputBudget(t *testing.T) {
	for _, budget := range []int{16384, 393216} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) { testCapabilityProbeBudget(t, budget) })
	}
}

func testCapabilityProbeBudget(t *testing.T, budget int) {
	s, resources, cfg := testsupport.Services(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		// The small check bounds its own request without changing the model.
		if string(body["max_tokens"]) != fmt.Sprint(min(budget, 32768)) || len(body["max_completion_tokens"]) != 0 {
			http.Error(w, "configured output budget was changed", http.StatusBadRequest)
			return
		}
		if string(body["stream"]) != "true" || strings.Contains(string(body["tool_choice"]), "function") {
			t.Error("non-GPT supplier inherited the GPT-specific transport/tool workaround")
		}
		n := calls.Add(1)
		name, args := "inspect_model_screenshot", "{}"
		if n == 2 {
			if !strings.Contains(string(body["messages"]), "data:image/png;base64,") {
				t.Error("actual probe pixels did not reach provider")
			}
			name, args = "submit_probe", `{"model_name":"DeepSeek V4.1 Flash","provider":"Muyuan","upstream_id":"deepseek-v4.1-flash","status_badge":"可选用 · 默认","latest_status":"检查通过","latest_date":"2026/10/17 10:49:42","latest_submissions":2,"latest_duration_seconds":7,"previous_status":"检查通过","previous_date":"2026/10/16 23:27:17","previous_submissions":2,"previous_duration_seconds":6}`
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-%d\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", n, name, args)
	}))
	defer server.Close()
	ctx := context.Background()
	provider, err := s.SaveProvider(ctx, "", store.ProviderInput{Name: "Capability fixture", BaseURL: server.URL, APIKey: "synthetic-key", Enabled: true, ProxyPolicy: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.SaveModel(ctx, "", store.ModelInput{ProviderID: provider.ID, DisplayName: "GLM fixture", UpstreamID: "glm-5.3-flash", MaxOutputTokens: budget, MaxInputChars: 2000000})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", "reasoning-fixture")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := s.ClaimProbe(ctx, accepted.ID)
	if err != nil || probe == nil {
		t.Fatalf("probe claim: %v %v", probe, err)
	}
	engine := &Engine{Store: s, Resources: resources, Config: cfg}
	if err := engine.RunProbe(ctx, *probe); err != nil {
		t.Fatal(err)
	}
	checks, err := s.Probes(ctx, m.ID)
	if err != nil || len(checks) != 1 || checks[0].Status != "succeeded" || checks[0].Submissions != 2 || calls.Load() != 2 {
		t.Fatalf("capability probe failed: checks=%+v err=%v provider_calls=%d", checks, err, calls.Load())
	}
	current, err := s.Model(ctx, m.ID)
	if err != nil || current.MaxOutputTokens != budget {
		t.Fatal("probe replaced the saved production output budget")
	}
}

func TestProbeSupplierRejectionIsStoredWithoutRepeatingCalls(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
	}{
		{429, `{"error":{"code":"account_cooldown","message":"all accounts are cooling down"}}`, "PROVIDER_ACCOUNT_COOLDOWN"},
		{503, `{"error":{"code":"model_not_found","message":"No available channel"}}`, "PROVIDER_MODEL_UNAVAILABLE"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			s, rm, cfg := testsupport.Services(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			provider, err := s.SaveProvider(context.Background(), "", store.ProviderInput{Name: "failure fixture", BaseURL: server.URL, APIKey: "fixture", Enabled: true, ProxyPolicy: "direct"})
			if err != nil {
				t.Fatal(err)
			}
			m, err := s.SaveModel(context.Background(), "", store.ModelInput{ProviderID: provider.ID, DisplayName: "GPT fixture", UpstreamID: "gpt-5.5"})
			if err != nil {
				t.Fatal(err)
			}
			a, err := s.CreateProbe(context.Background(), m.ID, m.RevisionID, "capabilities", tc.code)
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.ClaimProbe(context.Background(), a.ID)
			if err != nil || p == nil {
				t.Fatal(err)
			}
			e := &Engine{Store: s, Resources: rm, Config: cfg}
			if err = e.RunProbe(context.Background(), *p); err != nil {
				t.Fatal(err)
			}
			checks, err := s.Probes(context.Background(), m.ID)
			if err != nil || checks[0].FailureCode == nil || *checks[0].FailureCode != tc.code || checks[0].Status != "failed" || calls.Load() != 1 {
				t.Fatalf("wrong failure evidence: %+v calls=%d err=%v", checks, calls.Load(), err)
			}
		})
	}
}

func TestProbeInterruptedTransportRemainsUnknown(t *testing.T) {
	s, rm, cfg := testsupport.Services(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()
	provider, err := s.SaveProvider(context.Background(), "", store.ProviderInput{Name: "timeout fixture", BaseURL: server.URL, APIKey: "fixture", Enabled: true, ProxyPolicy: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.SaveModel(context.Background(), "", store.ModelInput{ProviderID: provider.ID, DisplayName: "timeout", UpstreamID: "gpt-5.5"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateProbe(context.Background(), m.ID, m.RevisionID, "capabilities", "timeout")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ClaimProbe(context.Background(), a.ID)
	if err != nil || p == nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err = (&Engine{Store: s, Resources: rm, Config: cfg}).RunProbe(ctx, *p); err != nil {
		t.Fatal(err)
	}
	checks, err := s.Probes(context.Background(), m.ID)
	if err != nil || checks[0].Status != "unknown" || checks[0].Submissions != 1 || checks[0].FailureCode == nil || *checks[0].FailureCode != "PROBE_EXECUTION_TIMEOUT" {
		t.Fatalf("timeout lost its uncertain outcome: %+v %v", checks, err)
	}
}
