package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVideoAgentLoopRunnerExecutesAndObservesToolActions(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoAgentLoopTool{
		definition: VideoAgentToolDefinition{Name: "inspect", Description: "test inspection"},
		output:     json.RawMessage(`{"value":"first observation"}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoAgentLoopPlanner{decisions: []VideoAgentLoopDecision{
		{Tool: "inspect", Reason: "先获取证据", Arguments: json.RawMessage(`{"query":"owner"}`)},
		{Done: true, StopReason: "证据已足够"},
	}}
	observer := &recordingVideoAgentLoopObserver{}
	runner, err := NewVideoAgentLoopRunner(registry, planner, observer, VideoAgentLoopPolicy{MaxSteps: 3, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoAgentLoopRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "验证 owner 校验的理由", VideoAgentToolRuntime{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.State.Status != VideoAgentLoopStatusCompleted || result.State.StopReason != "证据已足够" {
		t.Fatalf("state = %+v", result.State)
	}
	if result.State.CurrentStep != 1 || len(result.State.Steps) != 1 || len(result.State.Observations) != 1 {
		t.Fatalf("state steps = %+v, observations = %+v", result.State.Steps, result.State.Observations)
	}
	if tool.calls != 1 || planner.calls != 2 || observer.calls != 1 {
		t.Fatalf("calls = tool:%d planner:%d observer:%d", tool.calls, planner.calls, observer.calls)
	}
	if result.State.Steps[0].Status != VideoAgentLoopStepCompleted || result.State.Steps[0].Observation == nil {
		t.Fatalf("step = %+v", result.State.Steps[0])
	}
}

func TestVideoAgentLoopRunnerProvidesStructuredMemorySnapshotToPlanner(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	planner := &scriptedVideoAgentLoopPlanner{decisions: []VideoAgentLoopDecision{{Done: true, StopReason: "done"}}}
	runner, err := NewVideoAgentLoopRunner(registry, planner, &recordingVideoAgentLoopObserver{}, VideoAgentLoopPolicy{MaxSteps: 1, MaxReplans: 0})
	if err != nil {
		t.Fatal(err)
	}
	memory := &MemorySnapshot{
		SchemaVersion: MemorySnapshotSchemaVersion,
		Version:       MemorySnapshotSchemaVersion + ":research",
		MemoryIDs:     []string{"memory-research"},
		Items: []MemorySnapshotItem{{
			ID: "memory-research", ScopeType: "video", ScopeID: "1", Kind: "topic", Content: "trusted snapshot", SourceRef: "message:1",
		}},
	}
	result, err := runner.Run(context.Background(), "research", VideoAgentToolRuntime{MemorySnapshot: memory})
	if err != nil {
		t.Fatal(err)
	}
	if len(planner.states) != 1 || planner.states[0].Memory != memory || result.State.Memory != memory {
		t.Fatalf("research memory state = planner:%+v result:%+v", planner.states, result.State.Memory)
	}
}

func TestVideoAgentLoopRunnerStopsAtStepBudget(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoAgentLoopTool{
		definition: VideoAgentToolDefinition{Name: "inspect", Description: "test inspection"},
		output:     json.RawMessage(`{"ok":true}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoAgentLoopPlanner{decisions: []VideoAgentLoopDecision{
		{Tool: "inspect", Reason: "继续收集证据", Arguments: json.RawMessage(`{}`)},
		{Tool: "inspect", Reason: "继续收集证据", Arguments: json.RawMessage(`{}`)},
		{Tool: "inspect", Reason: "不应执行", Arguments: json.RawMessage(`{}`)},
	}}
	runner, err := NewVideoAgentLoopRunner(registry, planner, &recordingVideoAgentLoopObserver{}, VideoAgentLoopPolicy{MaxSteps: 2, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoAgentLoopRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "收集证据", VideoAgentToolRuntime{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.State.Status != VideoAgentLoopStatusStopped || result.State.StopReason != "budget_exhausted" {
		t.Fatalf("state = %+v", result.State)
	}
	if result.State.CurrentStep != 2 || len(result.State.Steps) != 2 || tool.calls != 2 || planner.calls != 2 {
		t.Fatalf("state = %+v, tool calls = %d, planner calls = %d", result.State, tool.calls, planner.calls)
	}
}

func TestVideoAgentLoopRunnerStopsAtReplanBudget(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoAgentLoopTool{
		definition: VideoAgentToolDefinition{Name: "inspect", Description: "test inspection"},
		output:     json.RawMessage(`{"ok":true}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoAgentLoopPlanner{decisions: []VideoAgentLoopDecision{
		{Tool: "inspect", Reason: "初次检索", Arguments: json.RawMessage(`{}`)},
		{Tool: "inspect", Reason: "第一次补检索", Replan: true, Arguments: json.RawMessage(`{}`)},
		{Tool: "inspect", Reason: "超出补检索上限", Replan: true, Arguments: json.RawMessage(`{}`)},
	}}
	runner, err := NewVideoAgentLoopRunner(registry, planner, &recordingVideoAgentLoopObserver{}, VideoAgentLoopPolicy{MaxSteps: 4, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoAgentLoopRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "收集证据", VideoAgentToolRuntime{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.State.Status != VideoAgentLoopStatusStopped || result.State.StopReason != "replan_limit_reached" {
		t.Fatalf("state = %+v", result.State)
	}
	if result.State.CurrentStep != 2 || result.State.ReplanCount != 1 || tool.calls != 2 {
		t.Fatalf("state = %+v, tool calls = %d", result.State, tool.calls)
	}
}

func TestVideoAgentLoopRunnerRejectsInvalidPlannerDecision(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoAgentLoopTool{
		definition: VideoAgentToolDefinition{Name: "inspect", Description: "test inspection"},
		output:     json.RawMessage(`{}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoAgentLoopPlanner{decisions: []VideoAgentLoopDecision{
		{Tool: "inspect", Arguments: json.RawMessage(`{}`)},
	}}
	runner, err := NewVideoAgentLoopRunner(registry, planner, &recordingVideoAgentLoopObserver{}, VideoAgentLoopPolicy{MaxSteps: 2, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoAgentLoopRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "收集证据", VideoAgentToolRuntime{})
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("Run() error = %v, want missing reason", err)
	}
	if result.State.Status != VideoAgentLoopStatusFailed || result.State.StopReason != "invalid_planner_decision" || tool.calls != 0 {
		t.Fatalf("state = %+v, tool calls = %d", result.State, tool.calls)
	}
}

func TestVideoAgentLoopRunnerRejectsAnswerWithUnobservedEvidence(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoAgentLoopTool{
		definition: VideoAgentToolDefinition{Name: VideoAgentToolBuildCitedAnswer, Description: "test answer"},
		output:     json.RawMessage(`{}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoAgentLoopPlanner{decisions: []VideoAgentLoopDecision{{
		Tool:      VideoAgentToolBuildCitedAnswer,
		Reason:    "直接生成回答",
		Arguments: json.RawMessage(`{"question":"owner","intermediate":"结论","citations":[{"evidence_id":"not-observed","chunk_id":1}]}`),
	}}}
	runner, err := NewVideoAgentLoopRunner(registry, planner, &recordingVideoAgentLoopObserver{}, VideoAgentLoopPolicy{MaxSteps: 2, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoAgentLoopRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "验证 owner", VideoAgentToolRuntime{})
	if err == nil || !strings.Contains(err.Error(), "未观察到的证据") {
		t.Fatalf("Run() error = %v, want unobserved evidence error", err)
	}
	if result.State.Status != VideoAgentLoopStatusFailed || result.State.CurrentStep != 0 || tool.calls != 0 {
		t.Fatalf("state = %+v, tool calls = %d", result.State, tool.calls)
	}
}

func TestDefaultVideoAgentLoopObserverAddsSearchEvidence(t *testing.T) {
	output, err := json.Marshal(SearchTranscriptResult{Citations: []RetrievedChunk{{EvidenceID: "ev-1", ChunkID: 1, Content: "owner 证据"}}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	observation, err := (DefaultVideoAgentLoopObserver{}).Observe(VideoAgentLoopState{}, VideoAgentToolResult{
		Output: output,
		Step:   VideoAgentStep{Tool: VideoAgentToolSearchTranscript},
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if len(observation.NewEvidence) != 1 || observation.NewEvidence[0].EvidenceID != "ev-1" {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestDefaultVideoAgentLoopObserverExtractsFinalAnswer(t *testing.T) {
	canonical := RetrievedChunk{TaskID: 42, EvidenceID: "ev-1", ChunkID: 1, ChunkIndex: 2, Content: "owner 证据", Source: RetrievalSourceHybrid}
	output, err := json.Marshal(BuildCitedAnswerResult{
		Answer:    "最终答案 [C1]",
		Citations: []RetrievedChunk{{TaskID: 999, EvidenceID: "ev-1", ChunkID: 999, ChunkIndex: 999, Content: "tampered", Source: "forged"}},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	observation, err := (DefaultVideoAgentLoopObserver{}).Observe(VideoAgentLoopState{Goal: "owner", Evidence: []RetrievedChunk{canonical}}, VideoAgentToolResult{
		Output: output,
		Step:   VideoAgentStep{Tool: VideoAgentToolBuildCitedAnswer},
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Answer != "最终答案" || len(observation.Citations) != 1 || observation.Citations[0].CitationID != "C1" {
		t.Fatalf("observation = %+v", observation)
	}
	if observation.Citations[0].TaskID != canonical.TaskID || observation.Citations[0].ChunkID != canonical.ChunkID || observation.Citations[0].Content != canonical.Content || observation.Citations[0].Source != canonical.Source {
		t.Fatalf("observer citations are not canonical: %+v", observation.Citations)
	}
	var persisted BuildCitedAnswerResult
	if err := json.Unmarshal(observation.Output, &persisted); err != nil || len(persisted.Citations) != 1 || persisted.Citations[0].TaskID != canonical.TaskID || persisted.Citations[0].EvidenceID != canonical.EvidenceID || persisted.Citations[0].ChunkID != canonical.ChunkID || persisted.Citations[0].Content != canonical.Content || persisted.Citations[0].Source != canonical.Source {
		t.Fatalf("canonical observation output = %+v err=%v", persisted, err)
	}
}

func TestCanonicalizeResearchCitationsRejectsCrossVideoEvidence(t *testing.T) {
	_, err := canonicalizeResearchCitations(
		[]RetrievedChunk{{TaskID: 99, EvidenceID: "cross-video", ChunkID: 1, Content: "other video"}},
		42,
		[]RetrievedChunk{{EvidenceID: "cross-video"}},
	)
	if err == nil || !strings.Contains(err.Error(), "越过当前视频边界") {
		t.Fatalf("canonicalizeResearchCitations() error = %v, want cross-video rejection", err)
	}
}

type scriptedVideoAgentLoopPlanner struct {
	decisions []VideoAgentLoopDecision
	calls     int
	states    []VideoAgentLoopState
}

func (p *scriptedVideoAgentLoopPlanner) NextDecision(_ context.Context, state VideoAgentLoopState, _ []VideoAgentToolDefinition) (VideoAgentLoopDecision, error) {
	p.calls++
	p.states = append(p.states, state)
	if len(p.decisions) == 0 {
		return VideoAgentLoopDecision{}, errors.New("no scripted planner decision")
	}
	decision := p.decisions[0]
	p.decisions = p.decisions[1:]
	return decision, nil
}

type scriptedVideoAgentLoopTool struct {
	definition VideoAgentToolDefinition
	output     json.RawMessage
	calls      int
}

func (t *scriptedVideoAgentLoopTool) Definition() VideoAgentToolDefinition {
	return t.definition
}

func (t *scriptedVideoAgentLoopTool) Execute(_ context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	t.calls++
	return VideoAgentToolResult{
		Output: append(json.RawMessage(nil), t.output...),
		Step:   VideoAgentStep{Tool: t.definition.Name, Input: map[string]any{"arguments": string(request.Arguments)}},
	}, nil
}

type recordingVideoAgentLoopObserver struct {
	calls int
}

func (o *recordingVideoAgentLoopObserver) Observe(_ VideoAgentLoopState, result VideoAgentToolResult) (VideoAgentLoopObservation, error) {
	o.calls++
	return VideoAgentLoopObservation{Tool: result.Step.Tool, Output: result.Output, Step: result.Step}, nil
}
