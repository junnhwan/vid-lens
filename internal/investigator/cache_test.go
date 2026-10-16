package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/schema"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"vidlens/internal/config"
)

func TestFinalWireStablePrefixAndVerifiedCacheMarkers(t *testing.T) {
	wires := []json.RawMessage{}
	reports := []usageReport{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if e := json.NewDecoder(r.Body).Decode(&raw); e != nil {
			t.Error(e)
		}
		wires = append(wires, raw)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p := config.DefaultProfile()
	p.ModelBaseURL = server.URL
	p.ModelName = "fixture"
	p.CachePolicy = "qwen_explicit"
	base, e := newModelWithClient(context.Background(), p, "fixture", &http.Client{Transport: safeTransport{http.DefaultTransport}})
	if e != nil {
		t.Fatal(e)
	}
	base, e = base.WithTools([]*schema.ToolInfo{{Name: "submit_answer", Desc: "validated result"}})
	if e != nil {
		t.Fatal(e)
	}
	for i, question := range []string{"first dynamic question", "second dynamic question", "unknown endpoint must get no hints"} {
		p.CacheHintsVerified = i < 2
		o := newUsageObserver("source-context-v2", p.CachePolicy, func(r usageReport) { reports = append(reports, r) })
		ctx := context.WithValue(context.Background(), usageKey{}, o)
		_, e = completeModelResponse(ctx, base, []*schema.Message{schema.SystemMessage("fixed rules"), schema.UserMessage("entire frozen source, including the end"), schema.UserMessage("frozen purpose and article"), schema.UserMessage(question)}, cacheOptions(p)...)
		if e != nil {
			t.Fatal(e)
		}
	}
	if reports[0].StablePrefixHash == "" || reports[0].StablePrefixHash != reports[1].StablePrefixHash || reports[0].ToolsHash != reports[1].ToolsHash || !reports[0].HintsSent || reports[2].HintsSent {
		t.Fatalf("wire prefix/hints: %+v", reports)
	}
	for i, raw := range wires {
		var wire struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools []any `json:"tools"`
		}
		if json.Unmarshal(raw, &wire) != nil || len(wire.Messages) != 4 || len(wire.Tools) != 1 || wire.Messages[0].Role != "system" {
			t.Fatalf("cache marker changed protocol: %s", raw)
		}
		if i == 2 && strings.Contains(string(raw), "cache_control") {
			t.Fatal("unverified cache hints sent")
		}
	}
}
