package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type summaryPipelineAI struct {
	calls  []string
	failAt int
}

func (s *summaryPipelineAI) Transcribe(context.Context, string) (string, error) { return "", nil }
func (s *summaryPipelineAI) TranscribeChunks(context.Context, []string) (string, error) {
	return "", nil
}
func (s *summaryPipelineAI) Summarize(_ context.Context, input string) (string, error) {
	s.calls = append(s.calls, input)
	if s.failAt == len(s.calls) {
		return "", errors.New("temporary provider failure")
	}
	return "核心摘要\n深度洞察\n原始内容精选\n领域标签", nil
}

func TestSummaryLeavesCoverCanonicalTranscriptAtASRBoundaries(t *testing.T) {
	rows := []model.VideoTranscriptionChunk{
		{ChunkIndex: 0, Status: model.TranscriptionChunkStatusCompleted, Content: "开头内容共享边界", CoreStartMS: 0, CoreEndMS: 300000, WindowEndMS: 305000},
		{ChunkIndex: 1, Status: model.TranscriptionChunkStatusCompleted, Content: "共享边界。后段内容", CoreStartMS: 300000, CoreEndMS: 600000, WindowStartMS: 295000, WindowEndMS: 600000},
	}
	full := "开头内容共享边界。后段内容"
	leaves := summaryLeaves(full, rows, 15)
	var joined strings.Builder
	for _, leaf := range leaves {
		joined.WriteString(leaf.text)
	}
	if joined.String() != full {
		t.Fatalf("summary inputs lost transcript text: %q", joined.String())
	}
	if leaves[0].endMS == 0 || leaves[len(leaves)-1].endMS != 600000 {
		t.Fatalf("time ranges missing: %+v", leaves)
	}
}

func TestSummarizeLongResumesAfterFailedPart(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 1, FileMD5: "12121212121212121212121212121212", Filename: "long.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("第一段完整文字。", 110) + strings.Repeat("第二段完整文字。", 110)
	if err := repos.TranscriptionChunk.UpsertCompletedWithRange(task.ID, 0, "a", strings.Repeat("第一段完整文字。", 110), 0, 300); err != nil {
		t.Fatal(err)
	}
	if err := repos.TranscriptionChunk.UpsertCompletedWithRange(task.ID, 1, "b", strings.Repeat("第二段完整文字。", 110), 300, 600); err != nil {
		t.Fatal(err)
	}
	first := &summaryPipelineAI{failAt: 2}
	c := &Consumer{repo: repos, ai: first}
	if _, err := c.summarizeLong(context.Background(), task, full, first, 1200); err == nil || !strings.Contains(err.Error(), "已覆盖 1/") {
		t.Fatalf("expected explicit partial coverage, got %v", err)
	}
	parts, err := repos.SummaryPart.List(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parts[0].Status != "completed" || parts[1].Status != "failed" {
		t.Fatalf("checkpoints = %+v", parts)
	}
	second := &summaryPipelineAI{}
	if _, err := c.summarizeLong(context.Background(), task, full, second, 1200); err != nil {
		t.Fatal(err)
	}
	if len(second.calls) == 0 || strings.Contains(second.calls[0], "第 1/") {
		t.Fatalf("completed first segment was rerun: %+v", second.calls)
	}
}

func TestSummarizeLongRequestsEnoughOutputForEveryPart(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	task := &model.VideoTask{UserID: 1, FileMD5: "34343434343434343434343434343434", Filename: "long.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		reason := "stop"
		if request.MaxTokens < 1000 {
			reason = "length"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"核心摘要\"}}]}\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", reason)
	}))
	defer server.Close()
	c := &Consumer{repo: repos}
	strategy := ai.NewOpenAICompatibleStrategy("", server.URL, "", "model")
	full := strings.Repeat("需要总结的完整转写。", 120)
	got, err := c.summarizeLong(context.Background(), task, full, strategy, 1200)
	if err != nil || got != "核心摘要" {
		t.Fatalf("summary = %q, error = %v", got, err)
	}
}
