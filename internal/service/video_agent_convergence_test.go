package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// Separates planner responses from the writer so an early finalization must
// still reach the real writer, citation validation, journal, and persistence.
type convergenceChatClient struct {
	plans        []string
	plannerCalls int
	writerCalls  int
	answer       string
}

func (c *convergenceChatClient) Chat(_ context.Context, messages []ai.ChatMessage) (string, error) {
	if strings.Contains(messages[0].Content, "计划器") {
		c.plannerCalls++
		if len(c.plans) == 0 {
			return `{"done":true,"stop_reason":"no further evidence"}`, nil
		}
		plan := c.plans[0]
		c.plans = c.plans[1:]
		return plan, nil
	}
	c.writerCalls++
	if c.answer != "" {
		return c.answer, nil
	}
	return "已有资料说明了选择和评分，具体配置未提供。[C1]", nil
}

func TestVideoAgentConvergenceStopsRewordedSearchesWithoutNewEvidence(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	retriever := &pipelineTestRetriever{}
	client := &convergenceChatClient{}
	for i := 0; i < 12; i++ {
		retriever.results = append(retriever.results, []RetrievedChunk{{
			TaskID: task.ID, ChunkID: 1, ChunkIndex: 1, EvidenceID: "same-evidence", Content: "选择与评分可组合，资料没有参数示例。",
		}})
		client.plans = append(client.plans, fmt.Sprintf(`{"tool":"search_transcript","reason":"补查配置","arguments":{"question":"选择与评分配置方法 %d"}}`, i))
	}
	svc := NewVideoAgentService(NewChatService(repos, retriever, ChatConfig{TopK: 1}))
	result, err := svc.RunAgent(context.Background(), VideoAgentLoopRequest{
		UserID: 7, SessionID: session.ID, Goal: "选择和评分怎么组合？", RunID: "convergence-reworded-search",
	}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(retriever.requests) > 4 || client.plannerCalls > 4 {
		t.Fatalf("stalled research kept planning: retrievals=%d planner_calls=%d; want at most 4", len(retriever.requests), client.plannerCalls)
	}
	if client.writerCalls != 1 || len(result.Citations) != 1 || result.Citations[0].EvidenceID != "same-evidence" {
		t.Fatalf("final writer/citations: calls=%d result=%+v", client.writerCalls, result)
	}
	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil || len(messages) != 2 || messages[1].Content != result.Answer {
		t.Fatalf("answer not persisted: messages=%+v error=%v", messages, err)
	}
	resumed, err := svc.ResumeAgent(context.Background(), 7, result.RunID, &fakeEmbeddingClient{dim: 3}, &convergenceChatClient{}, ai.Profile{EmbeddingModel: "embed"})
	if err != nil || resumed.Answer != result.Answer {
		t.Fatalf("terminal replay: result=%+v error=%v", resumed, err)
	}
}

func TestVideoAgentConvergencePlannerSeesCompletedWindowArguments(t *testing.T) {
	state := VideoAgentLoopState{Goal: "配置", Steps: []VideoAgentLoopStep{{
		Number: 1, Status: VideoAgentLoopStepCompleted,
		Action: VideoAgentLoopDecision{Tool: VideoAgentToolGetTranscriptWindow, Arguments: json.RawMessage(`{"chunk_index":1,"radius":3}`)},
	}}}
	messages, err := buildPlannerMessages(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(messages[1].Content, `"chunk_index":1`) || !strings.Contains(messages[1].Content, `"radius":3`) {
		t.Fatal("planner cannot see which transcript window was already read")
	}
}

func TestVideoAgentConvergenceEmptySearchesProduceGapAnswer(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	retriever := &pipelineTestRetriever{}
	client := &convergenceChatClient{answer: "当前检索未取得视频证据，无法确认这项配置。"}
	for i := 0; i < 8; i++ {
		retriever.results = append(retriever.results, nil)
		client.plans = append(client.plans, fmt.Sprintf(`{"tool":"search_transcript","reason":"核对缺口","arguments":{"question":"不存在的配置 %d"}}`, i))
	}
	svc := NewVideoAgentService(NewChatService(repos, retriever, ChatConfig{TopK: 1}))
	result, err := svc.RunAgent(context.Background(), VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "这项配置是什么？", RunID: "convergence-empty-search"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed"})
	if err != nil || len(retriever.requests) != 3 || client.plannerCalls != 3 || client.writerCalls != 1 || len(result.Citations) != 0 || result.Answer != client.answer {
		t.Fatalf("empty search finalization: result=%+v retrievals=%d planner=%d writer=%d error=%v", result, len(retriever.requests), client.plannerCalls, client.writerCalls, err)
	}
}

func TestVideoAgentConvergenceKeepsDifferentQueriesAndSourceWindows(t *testing.T) {
	prefix := strings.Repeat("已有的背景信息", 40)
	first, _ := json.Marshal(map[string]string{"question": prefix + "配置参数"})
	second, _ := json.Marshal(map[string]string{"question": prefix + "安全限制"})
	state := VideoAgentLoopState{Steps: []VideoAgentLoopStep{{Status: VideoAgentLoopStepCompleted, Action: VideoAgentLoopDecision{Tool: VideoAgentToolSearchTranscript, Arguments: first}, Observation: &VideoAgentLoopObservation{Tool: VideoAgentToolSearchTranscript}}}}
	if repeatedResearchAction(state, VideoAgentLoopDecision{Tool: VideoAgentToolSearchTranscript, Arguments: second}) {
		t.Fatal("display truncation collapsed distinct questions")
	}
	state.Steps[0].Action = VideoAgentLoopDecision{Tool: VideoAgentToolGetTranscriptWindow, Arguments: json.RawMessage(`{"task_id":1,"chunk_index":5,"radius":2}`)}
	for _, arguments := range []string{`{"task_id":2,"chunk_index":5}`, `{"task_id":1,"chunk_index":5,"radius":3}`, `{"task_id":1,"chunk_index":8,"radius":1}`} {
		if repeatedResearchAction(state, VideoAgentLoopDecision{Tool: VideoAgentToolGetTranscriptWindow, Arguments: json.RawMessage(arguments)}) {
			t.Fatalf("new source/window incorrectly blocked: %s", arguments)
		}
	}
}

func TestVideoAgentConvergenceCoveredWindowFinalizesWithoutReadingAgain(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	seedVideoChunks(t, repos, 7, task.ID, "embed", []string{"选择说明", "选择和评分说明"})
	client := &convergenceChatClient{plans: []string{
		`{"tool":"get_transcript_window","reason":"读取上下文","arguments":{"chunk_index":1,"radius":3}}`,
		`{"tool":"get_transcript_window","reason":"再次读取","arguments":{"radius":0,"chunk_index":1}}`,
	}}
	svc := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 1}))
	result, err := svc.RunAgent(context.Background(), VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "选择和评分说明", RunID: "convergence-covered-window"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed"})
	if err != nil {
		t.Fatal(err)
	}
	records, err := svc.executionJournal.Recover(context.Background(), 7, result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	windowCalls := 0
	for _, call := range records.ToolCalls {
		if call.ToolName == VideoAgentToolGetTranscriptWindow {
			windowCalls++
		}
	}
	if windowCalls != 1 || client.plannerCalls != 2 || client.writerCalls != 1 || records.Run.StopReason != "evidence_stalled" || records.Run.Status != model.AgentRunStatusCompleted {
		t.Fatalf("covered window executed again: windows=%d planner=%d writer=%d reason=%s", windowCalls, client.plannerCalls, client.writerCalls, records.Run.StopReason)
	}
}

func TestVideoAgentConvergenceAllowsResearchWhileEvidenceGrows(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	retriever := &pipelineTestRetriever{}
	client := &convergenceChatClient{}
	for i := 0; i < 8; i++ {
		retriever.results = append(retriever.results, []RetrievedChunk{{TaskID: task.ID, ChunkID: int64(i + 1), EvidenceID: fmt.Sprintf("e%d", i), Content: "一个新增的相关步骤"}})
		client.plans = append(client.plans, fmt.Sprintf(`{"tool":"search_transcript","reason":"核对下一个步骤","arguments":{"question":"步骤 %d"}}`, i))
	}
	svc := NewVideoAgentService(NewChatService(repos, retriever, ChatConfig{TopK: 1}))
	_, err := svc.RunAgent(context.Background(), VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "核对全部步骤", RunID: "convergence-growing-evidence"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed"})
	if err != nil || len(retriever.requests) != 8 || client.plannerCalls != 9 || client.writerCalls != 1 {
		t.Fatalf("growing research was cut short: retrievals=%d planner=%d writer=%d error=%v", len(retriever.requests), client.plannerCalls, client.writerCalls, err)
	}
}

func TestVideoAgentConvergenceCountsExpandedSourceAsProgress(t *testing.T) {
	runner := &VideoAgentLoopRunner{policy: DefaultVideoAgentLoopPolicy()}
	state := VideoAgentLoopState{}
	for _, text := range []string{"选择", "选择", "选择", "选择与评分"} {
		state.Observations = append(state.Observations, VideoAgentLoopObservation{Tool: VideoAgentToolGetTranscriptWindow, NewEvidence: []RetrievedChunk{{TaskID: 1, ChunkID: 1, EvidenceID: "e1", Content: text}}})
	}
	if runner.researchStalled(state) {
		t.Fatal("expanded content of the same source was treated as stagnation")
	}
	merged := mergeResearchProgressEvidence(state.Observations[0].NewEvidence, state.Observations[3].NewEvidence, 1)
	if len(merged) != 1 || merged[0].Content != "选择与评分" {
		t.Fatalf("writer lost the expanded evidence: %+v", merged)
	}
	legacy := mergeResearchProgressEvidence(state.Observations[0].NewEvidence, state.Observations[3].NewEvidence, 0)
	if legacy[0].Content != "选择" {
		t.Fatal("historical checkpoint behavior changed")
	}
}

func TestVideoAgentConvergenceRecoversStalledRunWithoutAnotherPlannerCall(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	policy := DefaultVideoAgentLoopPolicy()
	frozenPolicy, budget := loopAgentPolicy(1, policy)
	svc := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 1}))
	run, err := svc.ensureAgentRun(context.Background(), "convergence-inflight-recovery", 7, session, "选择和评分", string(VideoAgentLoopTemplate), "default", ai.Profile{EmbeddingModel: "embed"}, frozenPolicy, budget)
	if err != nil {
		t.Fatal(err)
	}
	evidence := []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "e1", Content: "资料只有功能说明"}}
	searchOutput, _ := json.Marshal(SearchTranscriptResult{Citations: evidence})
	answerOutput, _ := json.Marshal(BuildCitedAnswerResult{Answer: "功能说明，配置细节未提供。[C1]", Citations: evidence})
	registry := NewVideoAgentToolRegistry()
	search := &scriptedVideoAgentLoopTool{definition: VideoAgentToolDefinition{Name: VideoAgentToolSearchTranscript}, output: searchOutput}
	answer := &scriptedVideoAgentLoopTool{definition: VideoAgentToolDefinition{Name: VideoAgentToolBuildCitedAnswer}, output: answerOutput}
	for _, tool := range []VideoAgentTool{search, answer} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	planner := &scriptedVideoAgentLoopPlanner{}
	for i := 0; i < 4; i++ {
		planner.decisions = append(planner.decisions, VideoAgentLoopDecision{Tool: VideoAgentToolSearchTranscript, Reason: "核对缺口", Arguments: json.RawMessage(fmt.Sprintf(`{"question":"配置 %d"}`, i))})
	}
	runner, err := NewVideoAgentLoopRunner(registry, planner, DefaultVideoAgentLoopObserver{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.SetDurableExecution(svc.executionJournal, 7, run.ID); err != nil {
		t.Fatal(err)
	}
	state, _ := NewVideoAgentLoopState(run.Goal, policy)
	runtime := VideoAgentToolRuntime{UserID: 7, TaskID: task.ID}
	// Simulate process loss at an actual journal boundary, before the writer.
	for i := 0; i < 4; i++ {
		decision, exhausted, err := runner.nextResearchDecisionCheckpoint(context.Background(), state, runtime)
		if err != nil || exhausted {
			t.Fatalf("plan checkpoint: exhausted=%v error=%v", exhausted, err)
		}
		result, observation, exhausted, err := runner.executeResearchTool(context.Background(), state, runtime, decision)
		if err != nil || exhausted {
			t.Fatalf("tool checkpoint: exhausted=%v error=%v", exhausted, err)
		}
		applyRecoveredResearchStep(&state, i+1, decision, durableResearchToolCheckpoint{Result: result, Observation: observation}, policy.ConvergenceVersion)
	}
	resumedPlanner := &scriptedVideoAgentLoopPlanner{}
	resumed, err := NewVideoAgentLoopRunner(registry, resumedPlanner, DefaultVideoAgentLoopObserver{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.SetDurableExecution(svc.executionJournal, 7, run.ID); err != nil {
		t.Fatal(err)
	}
	result, err := resumed.Run(context.Background(), run.Goal, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if resumedPlanner.calls != 0 || search.calls != 4 || answer.calls != 1 || result.State.StopReason != "evidence_stalled" {
		t.Fatalf("stagnation lost on recovery: planner=%d search=%d answer=%d reason=%s", resumedPlanner.calls, search.calls, answer.calls, result.State.StopReason)
	}
	records, err := svc.executionJournal.Recover(context.Background(), 7, run.ID)
	if err != nil || records.Run.LLMCallsUsed != 5 || records.Run.ToolCallsUsed != 5 {
		t.Fatalf("synthetic final plan charged as LLM call: records=%+v error=%v", records, err)
	}
}

func TestVideoAgentConvergenceCapturedTrace(t *testing.T) {
	path := os.Getenv("VIDLENS_AGENT_TRACE_FILE")
	if path == "" {
		t.Skip("set VIDLENS_AGENT_TRACE_FILE to replay an ignored, read-only production capture")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Checkpoint names are snake case while result structs retain their Go
	// field names; decode into a dedicated local capture view.
	var trace struct {
		Runs []struct {
			Goal string `json:"goal"`
		} `json:"runs"`
		Steps []struct {
			Kind       string `json:"kind"`
			Action     string `json:"action"`
			Checkpoint string `json:"result_checkpoint"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &trace); err != nil || len(trace.Runs) != 1 {
		t.Fatalf("invalid local trace: %v", err)
	}
	for _, version := range []int{0, 1} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			registry := NewVideoAgentToolRegistry()
			planner := &scriptedVideoAgentLoopPlanner{}
			tools := map[string]*capturedResearchTool{}
			var decision VideoAgentLoopDecision
			for _, step := range trace.Steps {
				if step.Kind == "plan" {
					var stored durableResearchDecision
					if err := json.Unmarshal([]byte(step.Checkpoint), &stored); err != nil {
						t.Fatal(err)
					}
					decision = stored.toDecision()
					planner.decisions = append(planner.decisions, decision)
					continue
				}
				var stored durableResearchToolCheckpoint
				if err := json.Unmarshal([]byte(step.Checkpoint), &stored); err != nil {
					t.Fatal(err)
				}
				tool := tools[step.Action]
				if tool == nil {
					tool = &capturedResearchTool{name: step.Action, results: map[string]VideoAgentToolResult{}}
					tools[step.Action] = tool
					if err := registry.Register(tool); err != nil {
						t.Fatal(err)
					}
				}
				tool.results[string(compactResearchArguments(step.Action, decision.Arguments))] = stored.Result
			}
			policy := DefaultVideoAgentLoopPolicy()
			policy.ConvergenceVersion = version
			runner, err := NewVideoAgentLoopRunner(registry, planner, DefaultVideoAgentLoopObserver{}, policy)
			if err != nil {
				t.Fatal(err)
			}
			result, err := runner.Run(context.Background(), trace.Runs[0].Goal, VideoAgentToolRuntime{})
			if err != nil {
				t.Fatal(err)
			}
			if result.State.Answer == "" {
				t.Fatal("trace did not reach the cited writer")
			}
			if version == 0 && result.State.CurrentStep < 20 {
				t.Fatal("baseline no longer reproduces the long loop")
			}
			if version == 1 && (result.State.CurrentStep > 8 || planner.calls > 8) {
				t.Fatalf("captured loop still repeats: tools=%d planner=%d", result.State.CurrentStep, planner.calls)
			}
			t.Logf("convergence_version=%d tool_calls=%d planner_calls=%d evidence=%d stop_reason=%s", version, result.State.CurrentStep, planner.calls, len(result.State.Evidence), result.State.StopReason)
		})
	}
}

type capturedResearchTool struct {
	name    string
	results map[string]VideoAgentToolResult
}

func (t *capturedResearchTool) Definition() VideoAgentToolDefinition {
	return VideoAgentToolDefinition{Name: t.name}
}

func (t *capturedResearchTool) Execute(_ context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	if t.name == VideoAgentToolBuildCitedAnswer {
		var input buildCitedAnswerToolArguments
		if err := json.Unmarshal(request.Arguments, &input); err != nil {
			return VideoAgentToolResult{}, err
		}
		output, err := json.Marshal(BuildCitedAnswerResult{Answer: "根据已有证据回答，未提供的细节保持未确认。[C1]", Citations: input.Citations})
		return VideoAgentToolResult{Output: output, Step: VideoAgentStep{Tool: t.name}}, err
	}
	result, ok := t.results[string(compactResearchArguments(t.name, request.Arguments))]
	if !ok {
		return VideoAgentToolResult{}, fmt.Errorf("trace has no captured result for %s", t.name)
	}
	return result, nil
}
