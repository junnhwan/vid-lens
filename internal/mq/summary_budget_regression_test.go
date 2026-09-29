package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestSummarizeLongMergesRealisticChineseOutputsWithoutLosingTailFacts(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 1, FileMD5: "60606060606060606060606060606060", Filename: "summary-budget.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}

	// The failing production calls succeeded with these character counts. Their
	// UTF-8 byte lengths exceed, or nearly fill, the default merge input budget.
	lengths := []int{1404, 1310, 1423}
	facts := []string{"首段独有事实甲", "中段独有事实乙", "尾段独有事实丙"}
	responses := make([]string, len(lengths))
	for i, length := range lengths {
		responses[i] = strings.Repeat("要", length-utf8.RuneCountInString(facts[i])) + facts[i]
	}
	calls := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream   bool             `json:"stream"`
			Messages []ai.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode summary request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if !request.Stream || len(request.Messages) == 0 {
			t.Errorf("expected streaming summary messages: %+v", request)
		}
		input := request.Messages[len(request.Messages)-1].Content
		if len(input) > 4096 {
			t.Errorf("request exceeds input budget: %d", len(input))
		}
		calls[input]++
		var answer string
		for i, fact := range facts {
			if strings.Contains(input, fact) && strings.Contains(input, fmt.Sprintf("第 %d/3 段", i+1)) && calls[input] == 1 {
				answer = responses[i]
			}
		}
		if answer == "" {
			// A compaction or merge can preserve only facts present in its input.
			// Tail markers make silent truncation of a successful result visible.
			var input strings.Builder
			for _, message := range request.Messages {
				input.WriteString(message.Content)
			}
			var retained []string
			for _, fact := range facts {
				if strings.Contains(input.String(), fact) {
					retained = append(retained, fact)
				}
			}
			answer = "核心摘要\n" + strings.Join(retained, "\n")
		}
		payload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": answer}}}})
		if err != nil {
			t.Errorf("encode summary response: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", payload)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	full := ""
	for _, fact := range facts {
		full += strings.Repeat("文", 1100) + fact
	}
	limit := summaryDefaultContextTokens - summaryOutputTokens - summaryReservedTokens
	if leaves := summaryLeaves(full, nil, limit-450); len(leaves) != len(responses) {
		t.Fatalf("fixture leaves = %d, want %d", len(leaves), len(responses))
	}
	consumer := &Consumer{repo: repos}
	strategy := ai.NewOpenAICompatibleStrategy("", server.URL, "", "model")
	got, err := consumer.summarizeLong(context.Background(), task, full, strategy, limit)
	if err != nil {
		t.Fatalf("successful Chinese part summaries must remain mergeable: %v", err)
	}
	for _, fact := range facts {
		if !strings.Contains(got, fact) {
			t.Errorf("final summary lost %q: %q", fact, got)
		}
	}
	if len(calls) != 4 {
		t.Fatalf("unique calls = %d, want three complete leaves and one final merge", len(calls))
	}
}

type oversizedSummaryAI struct{ calls int }

func (s *oversizedSummaryAI) Transcribe(context.Context, string) (string, error) { return "", nil }
func (s *oversizedSummaryAI) TranscribeChunks(context.Context, []string) (string, error) {
	return "", nil
}
func (s *oversizedSummaryAI) Summarize(context.Context, string) (string, error) {
	s.calls++
	return strings.Repeat("文", 1500), nil
}

func TestSummaryOversizeRecoveryIsBoundedAndKeepsCompletedCheckpoint(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 1, FileMD5: "61616161616161616161616161616161", Filename: "bounded.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	part := &model.SummaryPart{TaskID: task.ID, InputHash: "old-input", InputLimit: 4096, Status: "completed", Content: strings.Repeat("旧", 1500)}
	if err := repos.SummaryPart.Upsert(part); err != nil {
		t.Fatal(err)
	}
	want := part.Content
	strategy := &oversizedSummaryAI{}
	c := &Consumer{repo: repos}
	if _, err := c.completeSummaryPart(context.Background(), strategy, part, "完整原文", summaryIntermediateOutputTokens); err == nil {
		t.Fatal("oversized output must not be silently truncated or accepted")
	}
	if strategy.calls != 2 {
		t.Fatalf("calls = %d, want bounded two attempts", strategy.calls)
	}
	saved, err := repos.SummaryPart.Find(task.ID, 0, 0)
	if err != nil || saved == nil || saved.Content != want || saved.Status != "completed" {
		t.Fatalf("completed checkpoint was lost: %+v, err=%v", saved, err)
	}
}

func TestSummaryIntermediateOutputsFitNextMergeAcrossLevels(t *testing.T) {
	for _, limit := range []int{1200, 4096, 12288} {
		outputLimit, err := summaryIntermediateByteLimit(limit)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Repeat("x", outputLimit)
		inputs := []summaryInput{{text: text, startMS: 0, endMS: 300000}, {text: text, startMS: 300000, endMS: 600000}, {text: text, startMS: 600000, endMS: 900000}}
		for level := 1; len(inputs) > 1; level++ {
			groups, err := packSummaryMerge(inputs, limit, level)
			if err != nil {
				t.Fatalf("limit %d level %d: %v", limit, level, err)
			}
			next := make([]summaryInput, len(groups))
			for i, group := range groups {
				if len(summaryMergePrompt(group, level)) > limit {
					t.Fatal("merge exceeds budget")
				}
				next[i] = summaryInput{text: text, startMS: group[0].startMS, endMS: group[len(group)-1].endMS}
			}
			inputs = next
		}
	}
}

func TestSummaryRegeneratesOversizedCheckpointFromCompleteSource(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 1, FileMD5: "62626262626262626262626262626262", Filename: "resume.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	part := &model.SummaryPart{TaskID: task.ID, InputHash: "same-input", InputLimit: 4096, Status: "completed", Content: strings.Repeat("旧", 1500)}
	if err := repos.SummaryPart.Upsert(part); err != nil {
		t.Fatal(err)
	}
	strategy := &summaryPipelineAI{}
	c := &Consumer{repo: repos}
	source := "原始完整输入包含末尾独有事实"
	got, err := c.completeSummaryPart(context.Background(), strategy, part, source, summaryIntermediateOutputTokens)
	if err != nil || len(strategy.calls) != 1 || strategy.calls[0] != source {
		t.Fatalf("recovery calls=%v, err=%v", strategy.calls, err)
	}
	saved, err := repos.SummaryPart.Find(task.ID, 0, 0)
	if err != nil || saved == nil || saved.Content != got || saved.Status != "completed" {
		t.Fatalf("replacement=%+v, err=%v", saved, err)
	}
}
