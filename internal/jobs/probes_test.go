package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vidlens/internal/investigator"
	"vidlens/internal/media"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func TestWorkerRunsThreeModelChecksTogether(t *testing.T) {
	s, rm, cfg := testsupport.Services(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 3)
	release := make(chan struct{})
	var once sync.Once
	allow := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model      string          `json:"model"`
			ToolChoice json.RawMessage `json:"tool_choice"`
			Messages   json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		name, args := "inspect_model_screenshot", "{}"
		if strings.Contains(string(body.ToolChoice), "submit_probe") {
			name = "submit_probe"
			if !strings.Contains(string(body.Messages), "data:image/png;base64,") {
				t.Error("provider request did not contain screenshot pixels")
			}
			args = `{"model_name":"DeepSeek V4.1 Flash","provider":"Muyuan","upstream_id":"deepseek-v4.1-flash","status_badge":"可选用 · 默认","latest_status":"检查通过","latest_date":"2026/10/17 10:49:42","latest_submissions":2,"latest_duration_seconds":7,"previous_status":"检查通过","previous_date":"2026/10/16 23:27:17","previous_submissions":2,"previous_duration_seconds":6}`
		} else {
			started <- body.Model
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "fixture-call", "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
	}))
	defer server.Close()
	provider, err := s.SaveProvider(ctx, "", store.ProviderInput{Name: "concurrency fixture", BaseURL: server.URL, APIKey: "fixture", Enabled: true, ProxyPolicy: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	var models []store.CatalogModel
	for n := 0; n < 3; n++ {
		m, err := s.SaveModel(ctx, "", store.ModelInput{ProviderID: provider.ID, DisplayName: fmt.Sprint("model ", n), UpstreamID: fmt.Sprint("gpt-5.5-probe-", n)})
		if err != nil {
			t.Fatal(err)
		}
		models = append(models, m)
		if _, err = s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", fmt.Sprint(n)); err != nil {
			t.Fatal(err)
		}
	}
	ms, err := media.New(s, rm, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// A busy video-generation lane must not serialize independent model checks.
	const generationKey = "vidlens:resource:ai:generation:0"
	if err = rm.Redis.Set(ctx, generationKey, "probe-test-generation", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	defer rm.Redis.Del(context.Background(), generationKey)
	w, err := New(s, ms, &investigator.Engine{Store: s, Media: ms, Resources: rm, Config: cfg}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { allow(); cancel(); w.Shutdown() }()
	seen := map[string]bool{}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for len(seen) < 3 {
		select {
		case model := <-started:
			seen[model] = true
		case <-timer.C:
			t.Fatalf("checks waited for each other: only %d of 3 reached the provider", len(seen))
		}
	}
	allow()
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		complete := true
		for _, m := range models {
			checks, err := s.Probes(ctx, m.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(checks) != 1 || checks[0].Status != "succeeded" || checks[0].Submissions != 2 {
				complete = false
			}
		}
		if complete {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("concurrent checks did not persist all three validated results")
}
