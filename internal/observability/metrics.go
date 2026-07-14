package observability

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var durationBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

type Metrics struct {
	gatherer             prometheus.Gatherer
	taskStageTotal       *prometheus.CounterVec
	taskStageDuration    *prometheus.HistogramVec
	taskRetryTotal       *prometheus.CounterVec
	taskDeadTotal        *prometheus.CounterVec
	kafkaJobDuration     *prometheus.HistogramVec
	asrChunkTotal        *prometheus.CounterVec
	asrChunkDuration     prometheus.Histogram
	asrChunkReuse        prometheus.Counter
	aiCallTotal          *prometheus.CounterVec
	aiCallDuration       *prometheus.HistogramVec
	aiTokensTotal        *prometheus.CounterVec
	aiEstimatedCost      *prometheus.CounterVec
	aiUsageUnknown       *prometheus.CounterVec
	ragRetrievalDuration *prometheus.HistogramVec
	ragResultCount       *prometheus.GaugeVec
	ragContextTokens     *prometheus.GaugeVec
	rateLimitDecision    *prometheus.CounterVec
}

type AICallObservation struct {
	Kind             string
	Provider         string
	Model            string
	Status           string
	Duration         time.Duration
	PromptTokens     *int64
	CompletionTokens *int64
	TotalTokens      *int64
	EstimatedCost    *float64
}

func NewMetrics(registerer prometheus.Registerer) (*Metrics, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	gatherer, ok := registerer.(prometheus.Gatherer)
	if !ok {
		gatherer = prometheus.DefaultGatherer
	}
	m := &Metrics{gatherer: gatherer}
	var err error
	if m.taskStageTotal, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_task_stage_total", Help: "Task stage outcomes."}, []string{"stage", "status"})); err != nil {
		return nil, err
	}
	if m.taskStageDuration, err = registerHistogramVec(registerer, prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "vidlens_task_stage_duration_seconds", Help: "Task stage duration in seconds.", Buckets: durationBuckets}, []string{"stage"})); err != nil {
		return nil, err
	}
	if m.taskRetryTotal, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_task_retry_total", Help: "Retryable task failures."}, []string{"job_type", "error_code"})); err != nil {
		return nil, err
	}
	if m.taskDeadTotal, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_task_dead_total", Help: "Tasks that exhausted retries."}, []string{"job_type"})); err != nil {
		return nil, err
	}
	if m.kafkaJobDuration, err = registerHistogramVec(registerer, prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "vidlens_kafka_job_duration_seconds", Help: "Kafka job processing duration.", Buckets: durationBuckets}, []string{"job_type"})); err != nil {
		return nil, err
	}
	if m.asrChunkTotal, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_asr_chunk_total", Help: "ASR chunk outcomes."}, []string{"status"})); err != nil {
		return nil, err
	}
	if m.asrChunkDuration, err = registerHistogram(registerer, prometheus.NewHistogram(prometheus.HistogramOpts{Name: "vidlens_asr_chunk_duration_seconds", Help: "ASR chunk processing duration.", Buckets: durationBuckets})); err != nil {
		return nil, err
	}
	if m.asrChunkReuse, err = registerCounter(registerer, prometheus.NewCounter(prometheus.CounterOpts{Name: "vidlens_asr_chunk_reuse_total", Help: "Reused completed ASR chunks."})); err != nil {
		return nil, err
	}
	if m.aiCallTotal, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_ai_call_total", Help: "AI provider call outcomes."}, []string{"kind", "provider", "model", "status"})); err != nil {
		return nil, err
	}
	if m.aiCallDuration, err = registerHistogramVec(registerer, prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "vidlens_ai_call_duration_seconds", Help: "AI provider call duration.", Buckets: durationBuckets}, []string{"kind", "provider", "model"})); err != nil {
		return nil, err
	}
	if m.aiTokensTotal, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_ai_tokens_total", Help: "Provider-reported or explicitly estimated tokens."}, []string{"kind", "provider", "model", "direction"})); err != nil {
		return nil, err
	}
	if m.aiEstimatedCost, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_ai_estimated_cost_total", Help: "Estimated AI call cost; absent when price is unknown."}, []string{"kind", "provider", "model"})); err != nil {
		return nil, err
	}
	if m.aiUsageUnknown, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_ai_usage_unknown_total", Help: "AI calls whose token or cost usage is unknown."}, []string{"kind", "provider", "model", "field"})); err != nil {
		return nil, err
	}
	if m.ragRetrievalDuration, err = registerHistogramVec(registerer, prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "vidlens_rag_retrieval_duration_seconds", Help: "RAG retrieval duration.", Buckets: durationBuckets}, []string{"mode"})); err != nil {
		return nil, err
	}
	if m.ragResultCount, err = registerGaugeVec(registerer, prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "vidlens_rag_result_count", Help: "Result count of the latest RAG retrieval."}, []string{"mode"})); err != nil {
		return nil, err
	}
	if m.ragContextTokens, err = registerGaugeVec(registerer, prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "vidlens_rag_context_tokens", Help: "Context size estimate of the latest RAG retrieval."}, []string{"mode"})); err != nil {
		return nil, err
	}
	if m.rateLimitDecision, err = registerCounterVec(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{Name: "vidlens_ratelimit_decision_total", Help: "Rate-limit decisions."}, []string{"scope", "result"})); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.gatherer == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.gatherer, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveTaskStage(stage, status string, duration time.Duration) {
	if m == nil {
		return
	}
	stage = normalizeStage(stage)
	status = normalizeStatus(status)
	m.taskStageTotal.WithLabelValues(stage, status).Inc()
	m.taskStageDuration.WithLabelValues(stage).Observe(duration.Seconds())
}
func (m *Metrics) IncTaskRetry(jobType, errorCode string) {
	if m != nil {
		m.taskRetryTotal.WithLabelValues(normalizeJobType(jobType), normalizeErrorCode(errorCode)).Inc()
	}
}
func (m *Metrics) IncTaskDead(jobType string) {
	if m != nil {
		m.taskDeadTotal.WithLabelValues(normalizeJobType(jobType)).Inc()
	}
}
func (m *Metrics) ObserveKafkaJob(jobType string, duration time.Duration) {
	if m != nil {
		m.kafkaJobDuration.WithLabelValues(normalizeJobType(jobType)).Observe(duration.Seconds())
	}
}
func (m *Metrics) ObserveASRChunk(status string, duration time.Duration) {
	if m != nil {
		m.asrChunkTotal.WithLabelValues(normalizeStatus(status)).Inc()
		m.asrChunkDuration.Observe(duration.Seconds())
	}
}
func (m *Metrics) IncASRChunkReuse() {
	if m != nil {
		m.asrChunkReuse.Inc()
	}
}
func (m *Metrics) ObserveAICall(call AICallObservation) {
	if m == nil {
		return
	}
	kind, provider, modelName, status := normalizeKind(call.Kind), normalizeProvider(call.Provider), normalizeModel(call.Model), normalizeStatus(call.Status)
	labels := []string{kind, provider, modelName}
	m.aiCallTotal.WithLabelValues(kind, provider, modelName, status).Inc()
	m.aiCallDuration.WithLabelValues(labels...).Observe(call.Duration.Seconds())
	knownTokens := false
	for _, usage := range []struct {
		direction string
		value     *int64
	}{{"prompt", call.PromptTokens}, {"completion", call.CompletionTokens}, {"total", call.TotalTokens}} {
		if usage.value != nil {
			knownTokens = true
			if *usage.value >= 0 {
				m.aiTokensTotal.WithLabelValues(kind, provider, modelName, usage.direction).Add(float64(*usage.value))
			}
		}
	}
	if !knownTokens {
		m.aiUsageUnknown.WithLabelValues(kind, provider, modelName, "tokens").Inc()
	}
	if call.EstimatedCost == nil {
		m.aiUsageUnknown.WithLabelValues(kind, provider, modelName, "cost").Inc()
	} else if *call.EstimatedCost >= 0 {
		m.aiEstimatedCost.WithLabelValues(labels...).Add(*call.EstimatedCost)
	}
}
func (m *Metrics) ObserveRAG(mode string, duration time.Duration, resultCount, contextTokens int) {
	if m == nil {
		return
	}
	mode = normalizeRAGMode(mode)
	m.ragRetrievalDuration.WithLabelValues(mode).Observe(duration.Seconds())
	m.ragResultCount.WithLabelValues(mode).Set(float64(resultCount))
	m.ragContextTokens.WithLabelValues(mode).Set(float64(contextTokens))
}
func (m *Metrics) IncRateLimit(scope, result string) {
	if m != nil {
		m.rateLimitDecision.WithLabelValues(normalizeScope(scope), normalizeRateLimitResult(result)).Inc()
	}
}

var defaultMetrics atomic.Pointer[Metrics]

func SetDefaultMetrics(metrics *Metrics) { defaultMetrics.Store(metrics) }
func DefaultMetrics() *Metrics           { return defaultMetrics.Load() }
func RecordAICall(call AICallObservation) {
	if metrics := DefaultMetrics(); metrics != nil {
		metrics.ObserveAICall(call)
	}
}

func normalize(value string, allowed map[string]struct{}) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := allowed[value]; ok {
		return value
	}
	return "other"
}
func normalizeKind(value string) string { return normalize(value, set("asr", "llm", "embedding")) }
func normalizeProvider(value string) string {
	return normalize(value, set("mimo", "siliconflow", "openai_compatible"))
}
func normalizeModel(value string) string {
	value = strings.TrimSpace(value)
	allowed := map[string]struct{}{"mimo-v2.5": {}, "mimo-v2.5-asr": {}, "Qwen/Qwen3-Embedding-8B": {}, "BAAI/bge-m3": {}}
	if _, ok := allowed[value]; ok {
		return value
	}
	return "other"
}
func normalizeStatus(value string) string {
	return normalize(value, set("success", "failed", "retry", "dead", "reused", "allowed", "rejected", "fail_open"))
}
func normalizeStage(value string) string {
	return normalize(value, set("downloading", "uploaded", "transcribing", "summarizing", "indexing", "none"))
}
func normalizeJobType(value string) string {
	return normalize(value, set("download", "transcribe", "analyze", "rag_index"))
}
func normalizeErrorCode(value string) string {
	return normalize(value, set("timeout", "rate_limited", "network_error", "auth_error", "provider_unavailable", "provider_error", "retryable_error", "retry_exhausted", "non_retryable_error", "enqueue_failed"))
}
func normalizeRAGMode(value string) string {
	return normalize(value, set("vector", "bm25", "hybrid", "rerank"))
}
func normalizeScope(value string) string {
	return normalize(value, set("default", "ai", "upload", "chat"))
}
func normalizeRateLimitResult(value string) string {
	return normalize(value, set("allowed", "rejected", "fail_open"))
}
func set(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func registerCounterVec(reg prometheus.Registerer, collector *prometheus.CounterVec) (*prometheus.CounterVec, error) {
	existing, err := register(reg, collector)
	if err != nil {
		return nil, err
	}
	typed, ok := existing.(*prometheus.CounterVec)
	if !ok {
		return nil, fmt.Errorf("existing collector has type %T, want CounterVec", existing)
	}
	return typed, nil
}
func registerHistogramVec(reg prometheus.Registerer, collector *prometheus.HistogramVec) (*prometheus.HistogramVec, error) {
	existing, err := register(reg, collector)
	if err != nil {
		return nil, err
	}
	typed, ok := existing.(*prometheus.HistogramVec)
	if !ok {
		return nil, fmt.Errorf("existing collector has type %T, want HistogramVec", existing)
	}
	return typed, nil
}
func registerGaugeVec(reg prometheus.Registerer, collector *prometheus.GaugeVec) (*prometheus.GaugeVec, error) {
	existing, err := register(reg, collector)
	if err != nil {
		return nil, err
	}
	typed, ok := existing.(*prometheus.GaugeVec)
	if !ok {
		return nil, fmt.Errorf("existing collector has type %T, want GaugeVec", existing)
	}
	return typed, nil
}
func registerCounter(reg prometheus.Registerer, collector prometheus.Counter) (prometheus.Counter, error) {
	existing, err := register(reg, collector)
	if err != nil {
		return nil, err
	}
	typed, ok := existing.(prometheus.Counter)
	if !ok {
		return nil, fmt.Errorf("existing collector has type %T, want Counter", existing)
	}
	return typed, nil
}
func registerHistogram(reg prometheus.Registerer, collector prometheus.Histogram) (prometheus.Histogram, error) {
	existing, err := register(reg, collector)
	if err != nil {
		return nil, err
	}
	typed, ok := existing.(prometheus.Histogram)
	if !ok {
		return nil, fmt.Errorf("existing collector has type %T, want Histogram", existing)
	}
	return typed, nil
}
func register(reg prometheus.Registerer, collector prometheus.Collector) (prometheus.Collector, error) {
	if err := reg.Register(collector); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			return already.ExistingCollector, nil
		}
		return nil, err
	}
	return collector, nil
}
