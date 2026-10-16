package investigator

// The observer sits below the SDK. It never reads ahead, stores prompts/pixels,
// or changes the bytes consumed by the model adapter.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type usageKey struct{}
type tokenMetric struct {
	Value  *int64 `json:"value"`
	Status string `json:"status"`
	Path   string `json:"source_path,omitempty"`
}
type usageReport struct {
	Schema            string      `json:"schema"`
	Input             tokenMetric `json:"input"`
	Output            tokenMetric `json:"output"`
	Total             tokenMetric `json:"total"`
	CacheRead         tokenMetric `json:"cache_read"`
	CacheWrite        tokenMetric `json:"cache_write"`
	StreamStatus      string      `json:"stream_status"`
	RequestID         string      `json:"request_id,omitempty"`
	ResponseID        string      `json:"response_id,omitempty"`
	ObservationStatus string      `json:"observation_status"`
	FinishReason      string      `json:"finish_reason,omitempty"`
	FirstResponseMS   *int64      `json:"first_response_ms"`
	TotalMS           int64       `json:"total_ms"`
	Layout            string      `json:"layout_version"`
	ToolsHash         string      `json:"tools_hash,omitempty"`
	StablePrefixHash  string      `json:"stable_prefix_hash,omitempty"`
	CachePolicy       string      `json:"cache_policy"`
	HintsSent         bool        `json:"hints_sent"`
	CostReporting     string      `json:"cost_reporting"`
}
type usageObserver struct {
	mu             sync.Mutex
	report         usageReport
	start          time.Time
	line, event    []byte
	dropping, done bool
	jsonBody       bool
	flush          func(usageReport)
}

func newUsageObserver(layout, policy string, flush func(usageReport)) *usageObserver {
	if layout == "" {
		layout = "legacy"
	}
	if policy == "" {
		policy = "provider_default"
	}
	u := tokenMetric{Status: "unknown"}
	return &usageObserver{start: time.Now(), report: usageReport{Schema: "openai_compatible", Input: u, Output: u, Total: u, CacheRead: u, CacheWrite: u, StreamStatus: "incomplete", Layout: layout, CachePolicy: policy, CostReporting: "unknown", ObservationStatus: "observed"}, flush: flush}
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (o *usageObserver) wire(req *http.Request) {
	if req.GetBody == nil {
		return
	}
	r, err := req.GetBody()
	if err != nil {
		return
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, 32<<20))
	if err != nil {
		return
	}
	var body struct {
		Messages []json.RawMessage `json:"messages"`
		Tools    json.RawMessage   `json:"tools"`
	}
	if json.Unmarshal(b, &body) != nil {
		return
	}
	o.report.ToolsHash = digest(body.Tools)
	// Layout v2 has exactly three frozen messages. Legacy checkpoints are
	// explicitly identified and do not claim a stable source prefix.
	if o.report.Layout == "source-context-v2" && len(body.Messages) >= 3 {
		prefix, _ := json.Marshal(body.Messages[:3])
		o.report.StablePrefixHash = digest(prefix)
	}
	for _, m := range body.Messages {
		var msg struct {
			Content []struct {
				Cache json.RawMessage `json:"cache_control"`
			} `json:"content"`
		}
		if json.Unmarshal(m, &msg) == nil {
			for _, p := range msg.Content {
				if len(p.Cache) > 0 {
					o.report.HintsSent = true
				}
			}
		}
	}
}
func metric(m map[string]json.RawMessage, paths ...string) tokenMetric {
	for _, path := range paths {
		parts := strings.Split(path, ".")
		current := m
		var raw json.RawMessage
		ok := false
		for i, key := range parts {
			raw, ok = current[key]
			if !ok {
				break
			}
			if i < len(parts)-1 {
				if json.Unmarshal(raw, &current) != nil {
					return tokenMetric{Status: "invalid", Path: path}
				}
			}
		}
		if !ok {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return tokenMetric{Status: "null", Path: path}
		}
		var n int64
		if json.Unmarshal(raw, &n) != nil || n < 0 {
			return tokenMetric{Status: "invalid", Path: path}
		}
		return tokenMetric{Value: &n, Status: "reported", Path: path}
	}
	return tokenMetric{Status: "unknown"}
}
func bounded(s string) string {
	if len(s) > 128 {
		return ""
	}
	for _, c := range s {
		if c < 32 {
			return ""
		}
	}
	return s
}
func (o *usageObserver) parse(b []byte) {
	var frame struct {
		ID      string                     `json:"id"`
		Usage   map[string]json.RawMessage `json:"usage"`
		Choices []struct {
			Finish string `json:"finish_reason"`
		} `json:"choices"`
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("[DONE]")) {
		o.report.StreamStatus = "complete"
		return
	}
	if json.Unmarshal(b, &frame) != nil {
		return
	}
	if id := bounded(frame.ID); id != "" {
		o.report.ResponseID = id
	}
	for _, c := range frame.Choices {
		if v := bounded(c.Finish); v != "" {
			o.report.FinishReason = v
		}
	}
	if frame.Usage == nil {
		return
	}
	// Usage frames are cumulative snapshots. Replace, never sum repeated frames.
	o.report.Input = metric(frame.Usage, "prompt_tokens", "input_tokens")
	o.report.Output = metric(frame.Usage, "completion_tokens", "output_tokens")
	o.report.Total = metric(frame.Usage, "total_tokens")
	o.report.CacheRead = metric(frame.Usage, "prompt_tokens_details.cached_tokens", "input_tokens_details.cached_tokens", "cache_read_input_tokens")
	o.report.CacheWrite = metric(frame.Usage, "cache_creation_input_tokens", "prompt_tokens_details.cache_creation_input_tokens", "prompt_tokens_details.cache_creation_tokens", "cache_creation_tokens")
	if o.report.Input.Value != nil && o.report.Output.Value != nil && o.report.Total.Value != nil && *o.report.Input.Value+*o.report.Output.Value != *o.report.Total.Value {
		o.report.Total.Status = "conflict"
	}
	if o.report.CacheRead.Value != nil && o.report.Input.Value != nil && strings.HasPrefix(o.report.CacheRead.Path, "prompt_tokens_details.") && o.report.Input.Path == "prompt_tokens" && *o.report.CacheRead.Value > *o.report.Input.Value {
		o.report.CacheRead.Status = "conflict"
	}
	if o.jsonBody && len(frame.Choices) > 0 && o.report.FinishReason != "" {
		o.report.StreamStatus = "complete"
	}
}
func (o *usageObserver) consume(b []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.done {
		return
	}
	if len(b) > 0 && o.report.FirstResponseMS == nil {
		n := time.Since(o.start).Milliseconds()
		o.report.FirstResponseMS = &n
	}
	if o.jsonBody {
		if len(o.event)+len(b) <= 64<<10 {
			o.event = append(o.event, b...)
		} else {
			o.event = nil
			o.dropping = true
			o.report.ObservationStatus = "partial_event_skipped"
		}
		return
	}
	for _, c := range b {
		if c != '\n' {
			if !o.dropping {
				if len(o.line)+len(o.event) >= 64<<10 {
					o.line = nil
					o.event = nil
					o.dropping = true
					o.report.ObservationStatus = "partial_event_skipped"
				} else {
					o.line = append(o.line, c)
				}
			}
			continue
		}
		line := bytes.TrimSuffix(o.line, []byte{'\r'})
		o.line = nil
		if len(line) == 0 {
			if !o.dropping && len(o.event) > 0 {
				o.parse(o.event)
			}
			o.event = nil
			o.dropping = false
			continue
		}
		if !o.dropping && bytes.HasPrefix(line, []byte("data:")) {
			if len(o.event) > 0 {
				o.event = append(o.event, '\n')
			}
			o.event = append(o.event, bytes.TrimPrefix(line[5:], []byte{' '})...)
		}
	}
}
func (o *usageObserver) finish(eof bool) {
	o.mu.Lock()
	if o.done {
		o.mu.Unlock()
		return
	}
	o.done = true
	if o.jsonBody && eof && !o.dropping {
		o.parse(o.event)
	}
	o.report.TotalMS = time.Since(o.start).Milliseconds()
	report := o.report
	o.line = nil
	o.event = nil
	o.mu.Unlock()
	if o.flush != nil {
		o.flush(report)
	}
}

type observedBody struct {
	io.ReadCloser
	o *usageObserver
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.o.consume(p[:n])
	if e != nil {
		b.o.finish(e == io.EOF)
	}
	return n, e
}
func (b *observedBody) Close() error { e := b.ReadCloser.Close(); b.o.finish(false); return e }
