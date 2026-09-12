package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPProductExecutor evaluates the running application's authenticated routes.
// It never retries POSTs: reconnect/replay would change the measured execution.
type HTTPProductExecutor struct {
	BaseURL, Token string
	Client         *http.Client
}

func (h HTTPProductExecutor) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(h.BaseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("Content-Type", "application/json")
	client := h.Client
	if client == nil {
		// Allow the largest supported run budget (900s) plus terminal journal
		// persistence/response overhead. The harness must not cancel a valid run.
		client = &http.Client{Timeout: 16 * time.Minute}
	}
	return client.Do(req)
}

func decodeProductResponse(resp *http.Response, target any) error {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("product HTTP status %d", resp.StatusCode)
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&envelope); err != nil {
		return err
	}
	if envelope.Code != 200 {
		return fmt.Errorf("product API code %d", envelope.Code)
	}
	return json.Unmarshal(envelope.Data, target)
}

func (h HTTPProductExecutor) Execute(ctx context.Context, turn ProductTurn) (ob ProductObservation, err error) {
	start := time.Now()
	ob = ProductObservation{SessionID: turn.SessionID, RunID: turn.RunID, Usage: ProductUsage{Source: "unknown"}}
	// A broken SSE transport does not erase the persisted execution. Read its
	// journal once, even after caller cancellation, without replaying the POST or
	// replacing the original execution/transport error with a success.
	defer func() {
		if ob.RunID != "" {
			h.readRunObservation(ctx, turn.SessionID, &ob)
		}
	}()
	path := fmt.Sprintf("/api/v1/chat/sessions/%d/messages", turn.SessionID)
	if turn.Kind == "agent" {
		path += "/agent"
	}
	if turn.Stream {
		path += "/stream"
	}
	body, _ := json.Marshal(map[string]any{"question": turn.Question, "top_k": turn.TopK, "run_id": turn.RunID})
	resp, err := h.request(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return ob, err
	}
	if !turn.Stream {
		err = decodeProductResponse(resp, &ob)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			return ob, fmt.Errorf("product stream unavailable: HTTP %d", resp.StatusCode)
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 8<<20)
		event := ""
		data := ""
		done := false
		consume := func() error {
			elapsed := float64(time.Since(start).Microseconds()) / 1000
			if event == "progress" && ob.FirstProgressMS == nil {
				ob.FirstProgressMS = &elapsed
			}
			if event == "answer" && ob.FirstAnswerMS == nil {
				ob.FirstAnswerMS = &elapsed
			}
			if event == "run_start" {
				var v struct {
					RunID string `json:"run_id"`
				}
				if e := json.Unmarshal([]byte(data), &v); e != nil {
					return e
				}
				ob.RunID = v.RunID
			}
			if event == "citations" {
				citations, e := decodeProductCitations([]byte(data))
				if e != nil {
					return e
				}
				ob.Citations = citations
			}
			if event == "done" {
				// done often contains only persistence identity and answer. Decode
				// into a copy so omitted citations retain the ordered SSE snapshot.
				result := ob
				result.Citations = append(json.RawMessage(nil), ob.Citations...)
				if e := json.Unmarshal([]byte(data), &result); e != nil {
					return e
				}
				if len(result.Citations) > 0 {
					if _, e := decodeProductCitations(result.Citations); e != nil {
						return e
					}
				}
				ob, done = result, true
				return nil
			}
			if event == "error" {
				return errors.New("product stream reported execution error")
			}
			return nil
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if e := consume(); e != nil {
					err = e
					break
				}
				event, data = "", ""
				continue
			}
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") {
				if data != "" {
					data += "\n"
				}
				data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if err == nil {
			err = scanner.Err()
		}
		if err == nil && !done {
			err = errors.New("product stream ended without saved result")
		}
	}
	if err != nil {
		return ob, err
	}
	if ob.MessageID > 0 {
		ob.Status = "completed"
	}
	return ob, nil
}

func decodeProductCitations(data []byte) (json.RawMessage, error) {
	var citations []json.RawMessage
	if err := json.Unmarshal(data, &citations); err != nil {
		return nil, errors.New("product citations event must contain a valid JSON array")
	}
	for _, citation := range citations {
		if trimmed := bytes.TrimSpace(citation); len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, errors.New("product citation must be a JSON object")
		}
	}
	return append(json.RawMessage(nil), data...), nil
}

func (h HTTPProductExecutor) readRunObservation(ctx context.Context, sessionID int64, ob *ProductObservation) {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	ob.Status, ob.StopReason = "unknown", ""
	ob.Usage = ProductUsage{Source: "unknown"}
	resp, err := h.request(readCtx, http.MethodGet, fmt.Sprintf("/api/v1/chat/sessions/%d/runs/%s", sessionID, url.PathEscape(ob.RunID)), nil)
	if err != nil {
		return
	}
	var run struct {
		Status           string `json:"status"`
		StopReason       string `json:"stop_reason"`
		TokenSource      string `json:"token_source"`
		PromptTokens     int64  `json:"prompt_tokens"`
		CompletionTokens int64  `json:"completion_tokens"`
	}
	if decodeProductResponse(resp, &run) != nil {
		return
	}
	if run.Status != "" {
		ob.Status = run.Status
	}
	ob.StopReason = run.StopReason
	if run.TokenSource != "" && run.TokenSource != "unknown" {
		ob.Usage = ProductUsage{Source: run.TokenSource, PromptTokens: &run.PromptTokens, CompletionTokens: &run.CompletionTokens}
	}
}
