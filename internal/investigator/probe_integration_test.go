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

	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func TestReasoningGPTCapabilityProbeHasRoomForToolCalls(t *testing.T) {
	s, resources, cfg := testsupport.Services(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		// Model the relay's observed rejection of the old 512-token probe.
		if string(body["max_completion_tokens"]) != "4096" {
			http.Error(w, "insufficient reasoning budget", http.StatusBadRequest)
			return
		}
		if len(body["temperature"]) != 0 || len(body["max_tokens"]) != 0 {
			t.Error("incompatible reasoning parameters reached provider")
		}
		n := calls.Add(1)
		name, args := "inspect_test_pattern", "{}"
		if n == 2 {
			if !strings.Contains(string(body["messages"]), "data:image/png;base64,") {
				t.Error("actual probe pixels did not reach provider")
			}
			name, args = "submit_probe", `{"left":"green","right":"purple"}`
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-%d\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", n, name, args)
	}))
	defer server.Close()
	ctx := context.Background()
	provider, err := s.SaveProvider(ctx, "", store.ProviderInput{Name: "Reasoning fixture", BaseURL: server.URL, APIKey: "synthetic-key", Enabled: true, ProxyPolicy: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.SaveModel(ctx, "", store.ModelInput{ProviderID: provider.ID, DisplayName: "GPT fixture", UpstreamID: "gpt-5.6-sol", MaxOutputTokens: 128000, MaxInputChars: 652000})
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
	if err != nil || current.MaxOutputTokens != 128000 {
		t.Fatal("probe replaced the saved production output budget")
	}
}
