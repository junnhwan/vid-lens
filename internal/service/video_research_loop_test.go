package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVideoResearchRunnerExecutesAndObservesToolActions(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoResearchTool{
		definition: VideoAgentToolDefinition{Name: "inspect", Description: "test inspection"},
		output:     json.RawMessage(`{"value":"first observation"}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoResearchPlanner{decisions: []VideoResearchDecision{
		{Tool: "inspect", Reason: "先获取证据", Arguments: json.RawMessage(`{"query":"owner"}`)},
		{Done: true, StopReason: "证据已足够"},
	}}
	observer := &recordingVideoResearchObserver{}
	runner, err := NewVideoResearchRunner(registry, planner, observer, VideoResearchPolicy{MaxSteps: 3, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoResearchRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "验证 owner 校验的理由", VideoAgentToolRuntime{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.State.Status != VideoResearchStatusCompleted || result.State.StopReason != "证据已足够" {
		t.Fatalf("state = %+v", result.State)
	}
	if result.State.CurrentStep != 1 || len(result.State.Steps) != 1 || len(result.State.Observations) != 1 {
		t.Fatalf("state steps = %+v, observations = %+v", result.State.Steps, result.State.Observations)
	}
	if tool.calls != 1 || planner.calls != 2 || observer.calls != 1 {
		t.Fatalf("calls = tool:%d planner:%d observer:%d", tool.calls, planner.calls, observer.calls)
	}
	if result.State.Steps[0].Status != VideoResearchStepCompleted || result.State.Steps[0].Observation == nil {
		t.Fatalf("step = %+v", result.State.Steps[0])
	}
}

func TestVideoResearchRunnerStopsAtStepBudget(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoResearchTool{
		definition: VideoAgentToolDefinition{Name: "inspect", Description: "test inspection"},
		output:     json.RawMessage(`{"ok":true}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoResearchPlanner{decisions: []VideoResearchDecision{
		{Tool: "inspect", Reason: "继续收集证据", Arguments: json.RawMessage(`{}`)},
		{Tool: "inspect", Reason: "继续收集证据", Arguments: json.RawMessage(`{}`)},
		{Tool: "inspect", Reason: "不应执行", Arguments: json.RawMessage(`{}`)},
	}}
	runner, err := NewVideoResearchRunner(registry, planner, &recordingVideoResearchObserver{}, VideoResearchPolicy{MaxSteps: 2, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoResearchRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "收集证据", VideoAgentToolRuntime{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.State.Status != VideoResearchStatusStopped || result.State.StopReason != "budget_exhausted" {
		t.Fatalf("state = %+v", result.State)
	}
	if result.State.CurrentStep != 2 || len(result.State.Steps) != 2 || tool.calls != 2 || planner.calls != 2 {
		t.Fatalf("state = %+v, tool calls = %d, planner calls = %d", result.State, tool.calls, planner.calls)
	}
}

func TestVideoResearchRunnerStopsAtReplanBudget(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoResearchTool{
		definition: VideoAgentToolDefinition{Name: "inspect", Description: "test inspection"},
		output:     json.RawMessage(`{"ok":true}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoResearchPlanner{decisions: []VideoResearchDecision{
		{Tool: "inspect", Reason: "初次检索", Arguments: json.RawMessage(`{}`)},
		{Tool: "inspect", Reason: "第一次补检索", Replan: true, Arguments: json.RawMessage(`{}`)},
		{Tool: "inspect", Reason: "超出补检索上限", Replan: true, Arguments: json.RawMessage(`{}`)},
	}}
	runner, err := NewVideoResearchRunner(registry, planner, &recordingVideoResearchObserver{}, VideoResearchPolicy{MaxSteps: 4, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoResearchRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "收集证据", VideoAgentToolRuntime{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.State.Status != VideoResearchStatusStopped || result.State.StopReason != "replan_limit_reached" {
		t.Fatalf("state = %+v", result.State)
	}
	if result.State.CurrentStep != 2 || result.State.ReplanCount != 1 || tool.calls != 2 {
		t.Fatalf("state = %+v, tool calls = %d", result.State, tool.calls)
	}
}

func TestVideoResearchRunnerRejectsInvalidPlannerDecision(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoResearchTool{
		definition: VideoAgentToolDefinition{Name: "inspect", Description: "test inspection"},
		output:     json.RawMessage(`{}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoResearchPlanner{decisions: []VideoResearchDecision{
		{Tool: "inspect", Arguments: json.RawMessage(`{}`)},
	}}
	runner, err := NewVideoResearchRunner(registry, planner, &recordingVideoResearchObserver{}, VideoResearchPolicy{MaxSteps: 2, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoResearchRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "收集证据", VideoAgentToolRuntime{})
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("Run() error = %v, want missing reason", err)
	}
	if result.State.Status != VideoResearchStatusFailed || result.State.StopReason != "invalid_planner_decision" || tool.calls != 0 {
		t.Fatalf("state = %+v, tool calls = %d", result.State, tool.calls)
	}
}

func TestVideoResearchRunnerRejectsAnswerWithUnobservedEvidence(t *testing.T) {
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoResearchTool{
		definition: VideoAgentToolDefinition{Name: VideoAgentToolBuildCitedAnswer, Description: "test answer"},
		output:     json.RawMessage(`{}`),
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	planner := &scriptedVideoResearchPlanner{decisions: []VideoResearchDecision{{
		Tool:      VideoAgentToolBuildCitedAnswer,
		Reason:    "直接生成回答",
		Arguments: json.RawMessage(`{"question":"owner","intermediate":"结论","citations":[{"evidence_id":"not-observed","chunk_id":1}]}`),
	}}}
	runner, err := NewVideoResearchRunner(registry, planner, &recordingVideoResearchObserver{}, VideoResearchPolicy{MaxSteps: 2, MaxReplans: 1})
	if err != nil {
		t.Fatalf("NewVideoResearchRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "验证 owner", VideoAgentToolRuntime{})
	if err == nil || !strings.Contains(err.Error(), "未观察到的证据") {
		t.Fatalf("Run() error = %v, want unobserved evidence error", err)
	}
	if result.State.Status != VideoResearchStatusFailed || result.State.CurrentStep != 0 || tool.calls != 0 {
		t.Fatalf("state = %+v, tool calls = %d", result.State, tool.calls)
	}
}

func TestDefaultVideoResearchObserverAddsSearchEvidence(t *testing.T) {
	output, err := json.Marshal(SearchTranscriptResult{Citations: []RetrievedChunk{{EvidenceID: "ev-1", ChunkID: 1, Content: "owner 证据"}}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	observation, err := (DefaultVideoResearchObserver{}).Observe(VideoResearchState{}, VideoAgentToolResult{
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

func TestDefaultVideoResearchObserverExtractsFinalAnswer(t *testing.T) {
	output, err := json.Marshal(BuildCitedAnswerResult{
		Answer:    "最终答案 [C1]",
		Citations: []RetrievedChunk{{EvidenceID: "ev-1", ChunkID: 1, ChunkIndex: 2, Content: "owner 证据"}},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	observation, err := (DefaultVideoResearchObserver{}).Observe(VideoResearchState{Goal: "owner"}, VideoAgentToolResult{
		Output: output,
		Step:   VideoAgentStep{Tool: VideoAgentToolBuildCitedAnswer},
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Answer != "最终答案" || len(observation.Citations) != 1 || observation.Citations[0].CitationID != "C1" {
		t.Fatalf("observation = %+v", observation)
	}
}

type scriptedVideoResearchPlanner struct {
	decisions []VideoResearchDecision
	calls     int
}

func (p *scriptedVideoResearchPlanner) NextDecision(_ context.Context, _ VideoResearchState, _ []VideoAgentToolDefinition) (VideoResearchDecision, error) {
	p.calls++
	if len(p.decisions) == 0 {
		return VideoResearchDecision{}, errors.New("no scripted planner decision")
	}
	decision := p.decisions[0]
	p.decisions = p.decisions[1:]
	return decision, nil
}

type scriptedVideoResearchTool struct {
	definition VideoAgentToolDefinition
	output     json.RawMessage
	calls      int
}

func (t *scriptedVideoResearchTool) Definition() VideoAgentToolDefinition {
	return t.definition
}

func (t *scriptedVideoResearchTool) Execute(_ context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	t.calls++
	return VideoAgentToolResult{
		Output: append(json.RawMessage(nil), t.output...),
		Step:   VideoAgentStep{Tool: t.definition.Name, Input: map[string]any{"arguments": string(request.Arguments)}},
	}, nil
}

type recordingVideoResearchObserver struct {
	calls int
}

func (o *recordingVideoResearchObserver) Observe(_ VideoResearchState, result VideoAgentToolResult) (VideoResearchObservation, error) {
	o.calls++
	return VideoResearchObservation{Tool: result.Step.Tool, Output: result.Output, Step: result.Step}, nil
}
