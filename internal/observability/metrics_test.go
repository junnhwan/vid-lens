package observability

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsCanReuseRegistryAndExposeLowCardinalityLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewMetrics(registry)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	if _, err := NewMetrics(registry); err != nil {
		t.Fatalf("duplicate NewMetrics: %v", err)
	}

	metrics.ObserveTaskStage("transcribing", "success", 1500*time.Millisecond)
	metrics.IncTaskRetry("transcribe", "timeout")
	metrics.ObserveKafkaJob("transcribe", 2*time.Second)
	metrics.ObserveASRChunk("success", 800*time.Millisecond)
	metrics.IncASRChunkReuse()
	metrics.ObserveRAG("hybrid", 30*time.Millisecond, 5, 120)
	metrics.IncRateLimit("ai", "allowed")

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	forbidden := map[string]bool{"trace_id": true, "task_id": true, "job_id": true, "user_id": true}
	foundTaskMetric := false
	for _, family := range families {
		if family.GetName() == "vidlens_task_stage_total" {
			foundTaskMetric = true
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if forbidden[label.GetName()] {
					t.Fatalf("high-cardinality label %q in %s", label.GetName(), family.GetName())
				}
			}
		}
	}
	if !foundTaskMetric {
		t.Fatal("vidlens_task_stage_total not gathered")
	}

	rr := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "vidlens_task_stage_total") {
		t.Fatalf("metrics handler code=%d body=%q", rr.Code, rr.Body.String())
	}
}

func TestAIUsageUnknownIsNotRecordedAsZeroCost(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewMetrics(registry)
	if err != nil {
		t.Fatal(err)
	}
	metrics.ObserveAICall(AICallObservation{
		Kind: "llm", Provider: "custom-byok-provider", Model: "private-model-123",
		Status: "success", Duration: time.Second,
	})

	rr := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rr.Result().Body)
	text := string(body)
	if !strings.Contains(text, `vidlens_ai_usage_unknown_total{field="tokens",kind="llm",model="other",provider="other"} 1`) {
		t.Fatalf("missing normalized unknown-token metric:\n%s", text)
	}
	if !strings.Contains(text, `vidlens_ai_usage_unknown_total{field="cost",kind="llm",model="other",provider="other"} 1`) {
		t.Fatalf("missing normalized unknown-cost metric:\n%s", text)
	}
	if strings.Contains(text, "vidlens_ai_estimated_cost_total{kind=\"llm\",model=\"other\",provider=\"other\"} 0") {
		t.Fatalf("unknown cost exported as measured zero:\n%s", text)
	}
}

func TestAIKnownUsageRecordsTokensAndCost(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, _ := NewMetrics(registry)
	prompt, completion, total := int64(10), int64(4), int64(14)
	cost := 0.002
	metrics.ObserveAICall(AICallObservation{Kind: "llm", Provider: "mimo", Model: "mimo-v2.5", Status: "failed", Duration: 2 * time.Second, PromptTokens: &prompt, CompletionTokens: &completion, TotalTokens: &total, EstimatedCost: &cost})
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var tokenMetrics, costMetrics int
	for _, family := range families {
		switch family.GetName() {
		case "vidlens_ai_tokens_total":
			tokenMetrics += len(family.Metric)
		case "vidlens_ai_estimated_cost_total":
			costMetrics += len(family.Metric)
		}
	}
	if tokenMetrics != 3 || costMetrics != 1 {
		t.Fatalf("token metrics=%d cost metrics=%d", tokenMetrics, costMetrics)
	}
}
