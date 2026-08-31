package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestMarshalAgentSnapshotUsesVersionedReplayableStepEnvelope(t *testing.T) {
	result := &VideoAgentResult{
		RunID:    "run-1",
		Mode:     AgentStreamMode,
		Template: string(VideoAgentDirectQA),
		Trace: []VideoAgentStep{{
			Name: "search topic", Tool: VideoAgentToolSearchTranscript,
			Input: map[string]any{"top_k": 4}, OutputRef: "citations:2",
		}},
		Citations: []Citation{{CitationID: "C1", ChunkID: 11, ChunkIndex: 3, Content: "证据"}},
	}

	raw, err := MarshalAgentSnapshot(result)
	if err != nil {
		t.Fatalf("MarshalAgentSnapshot() error = %v", err)
	}
	var got AgentSnapshot
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal snapshot: %v; raw=%s", err, raw)
	}
	if got.Version != AgentSnapshotVersion || got.RunID != "run-1" || got.Mode != AgentStreamMode {
		t.Fatalf("snapshot identity = %+v", got)
	}
	if len(got.Steps) != 1 || len(got.Citations) != 1 {
		t.Fatalf("snapshot lengths = steps:%d citations:%d", len(got.Steps), len(got.Citations))
	}
	step := got.Steps[0]
	if step.StepID != "s1" || step.Kind != "retrieve" || step.Label != "检索转写片段" || step.Status != AgentStepStatusDone {
		t.Fatalf("snapshot step = %+v", step)
	}
	if step.Tool != VideoAgentToolSearchTranscript || step.Output != "citations:2" || step.Input["top_k"] != float64(4) {
		t.Fatalf("snapshot step details = %+v", step)
	}
	if _, err := time.Parse(time.RFC3339Nano, step.TS); err != nil {
		t.Fatalf("snapshot step ts = %q: %v", step.TS, err)
	}
	if strings.Contains(string(raw), "chain-of-thought") || strings.Contains(string(raw), "intermediate") {
		t.Fatalf("snapshot contains unsafe reasoning fields: %s", raw)
	}
}

func TestAgentSnapshotEmptyTraceStillUsesEmptyArrays(t *testing.T) {
	raw, err := MarshalAgentSnapshot(&VideoAgentResult{RunID: "run-empty", Mode: AgentStreamMode})
	if err != nil {
		t.Fatalf("MarshalAgentSnapshot() error = %v", err)
	}
	var got AgentSnapshot
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if got.Version != AgentSnapshotVersion || got.RunID != "run-empty" || got.Mode != AgentStreamMode {
		t.Fatalf("snapshot identity = %+v", got)
	}
	if got.Steps == nil || got.Citations == nil || len(got.Steps) != 0 || len(got.Citations) != 0 {
		t.Fatalf("empty snapshot arrays = steps:%#v citations:%#v", got.Steps, got.Citations)
	}
	if got.MemoryPolicy.EffectiveEnabled || got.MemoryPolicy.SessionPolicy != model.MemorySessionPolicyInherit || got.MemoryPolicy.Reason != model.MemoryPolicyReasonUnavailable {
		t.Fatalf("legacy-compatible fail-closed memory policy = %+v", got.MemoryPolicy)
	}
}

func TestVideoAgentSyncAndStreamPersistTheSameSnapshotShape(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	profile := ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model"}
	retriever := &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-unified", ChunkID: 1, ChunkIndex: 2, Score: 0.9, Content: "统一快照证据",
	}}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	agent := NewVideoAgentService(chatSvc)

	syncResult, err := agent.Ask(context.Background(), VideoAgentRequest{
		UserID: session.UserID, SessionID: session.ID, Question: "为什么要统一？", TopK: 1,
	}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"not-json", "同步回答 [C1]"}}, profile)
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	streamResult, err := agent.Stream(context.Background(), VideoAgentStreamRequest{
		UserID: session.UserID, SessionID: session.ID, Question: "为什么要统一？", TopK: 1, Mode: AgentStreamMode,
	}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"not-json", "流式回答 [C1]"}}, profile, func(AgentStreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if syncResult.RunID == "" || streamResult.RunID == "" || syncResult.Mode != AgentStreamMode || streamResult.Mode != AgentStreamMode {
		t.Fatalf("result identities = sync:%+v stream:%+v", syncResult, streamResult)
	}

	messages, err := repos.Chat.ListMessages(session.UserID, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 4 || messages[1].RetrievalSnapshot == nil || messages[3].RetrievalSnapshot == nil {
		t.Fatalf("messages = %+v", messages)
	}
	syncSnapshot, err := DecodeAgentSnapshot(*messages[1].RetrievalSnapshot)
	if err != nil {
		t.Fatalf("decode sync snapshot: %v", err)
	}
	streamSnapshot, err := DecodeAgentSnapshot(*messages[3].RetrievalSnapshot)
	if err != nil {
		t.Fatalf("decode stream snapshot: %v", err)
	}
	if syncSnapshot.Version != AgentSnapshotVersion || streamSnapshot.Version != AgentSnapshotVersion ||
		syncSnapshot.Mode != streamSnapshot.Mode || syncSnapshot.Template != streamSnapshot.Template ||
		len(syncSnapshot.Steps) != len(streamSnapshot.Steps) || len(syncSnapshot.Citations) != len(streamSnapshot.Citations) {
		t.Fatalf("snapshot shape differs: sync=%+v stream=%+v", syncSnapshot, streamSnapshot)
	}
	for i := range syncSnapshot.Steps {
		syncStep, streamStep := syncSnapshot.Steps[i], streamSnapshot.Steps[i]
		if syncStep.StepID != streamStep.StepID || syncStep.Kind != streamStep.Kind || syncStep.Label != streamStep.Label ||
			syncStep.Status != AgentStepStatusDone || streamStep.Status != AgentStepStatusDone || syncStep.Tool != streamStep.Tool {
			t.Fatalf("step[%d] differs: sync=%+v stream=%+v", i, syncStep, streamStep)
		}
	}
}

func TestDecodeAgentSnapshotKeepsLegacyRetrievalSnapshotReadable(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantSteps  int
		wantCites  int
		wantStatus string
	}{
		{
			name:      "bare citation array",
			raw:       `[{"citation_id":"C1","chunk_id":1,"chunk_index":2,"content":"legacy"}]`,
			wantCites: 1,
		},
		{
			name:       "legacy agent envelope",
			raw:        `{"template":"direct_qa","citations":[],"trace":[{"name":"search topic","tool":"search_transcript","input":{},"output_ref":"citations:1"}]}`,
			wantSteps:  1,
			wantStatus: AgentStepStatusDone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeAgentSnapshot(tt.raw)
			if err != nil {
				t.Fatalf("DecodeAgentSnapshot() error = %v", err)
			}
			if len(got.Steps) != tt.wantSteps || len(got.Citations) != tt.wantCites {
				t.Fatalf("snapshot = %+v", got)
			}
			if tt.wantSteps > 0 && (got.Steps[0].StepID != "s1" || got.Steps[0].Kind != "retrieve" || got.Steps[0].Status != tt.wantStatus) {
				t.Fatalf("legacy step = %+v", got.Steps[0])
			}
		})
	}
}

func TestAgentSnapshotMarksFailureAndCancellationAsTerminalErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  string
	}{
		{name: "failure", err: "retrieval unavailable"},
		{name: "cancellation", err: context.Canceled.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := MarshalAgentSnapshot(&VideoAgentResult{
				RunID: "run-" + tt.name, Mode: AgentStreamMode,
				Trace: []VideoAgentStep{{Name: "search topic", Tool: VideoAgentToolSearchTranscript, Error: tt.err}},
			})
			if err != nil {
				t.Fatalf("MarshalAgentSnapshot() error = %v", err)
			}
			got, err := DecodeAgentSnapshot(string(raw))
			if err != nil {
				t.Fatalf("DecodeAgentSnapshot() error = %v", err)
			}
			if len(got.Steps) != 1 || got.Steps[0].Status != AgentStepStatusError || got.Steps[0].Error != tt.err {
				t.Fatalf("terminal error step = %+v", got.Steps)
			}
		})
	}
}

func TestMarshalAgentSnapshotRejectsNilResult(t *testing.T) {
	if _, err := MarshalAgentSnapshot(nil); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("MarshalAgentSnapshot(nil) error = %v", err)
	}
}
