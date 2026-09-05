package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

const (
	AgentStreamMode = "agent"

	AgentEventRunStart     = "run_start"
	AgentEventStepStart    = "step_start"
	AgentEventStepDone     = "step_done"
	AgentEventStepError    = "step_error"
	AgentEventToolCall     = "tool_call"
	AgentEventToolResult   = "tool_result"
	AgentEventRetrieveHits = "retrieve_hits"
	AgentEventAnswer       = "answer"
	AgentEventCitations    = "citations"
	AgentEventDone         = "done"
	AgentEventError        = "error"
)

type VideoAgentStreamRequest struct {
	UserID       int64
	SessionID    int64
	Question     string
	TopK         int
	Mode         string
	AgentProfile string
}

// AgentStreamEvent is the transport-neutral event passed from the service to
// the SSE handler. The event name is kept outside Data so the same model can
// be adapted to another transport later.
type AgentStreamEvent struct {
	Type string
	Data any
}

type AgentRunStartEvent struct {
	RunID           string                      `json:"run_id"`
	Mode            string                      `json:"mode"`
	ScopeType       string                      `json:"scope_type"`
	TaskID          int64                       `json:"task_id,omitempty"`
	KnowledgeBaseID int64                       `json:"kb_id,omitempty"`
	MemoryPolicy    model.EffectiveMemoryPolicy `json:"memory_policy"`
}

type AgentStepEvent struct {
	RunID     string `json:"run_id"`
	StepID    string `json:"step_id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Query     string `json:"query,omitempty"`
	Hits      int    `json:"hits,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Input     any    `json:"input,omitempty"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"ts,omitempty"`
}

type AgentToolCallEvent struct {
	RunID  string `json:"run_id"`
	StepID string `json:"step_id"`
	Tool   string `json:"tool"`
	Input  any    `json:"input,omitempty"`
}

type AgentToolResultEvent struct {
	RunID      string `json:"run_id"`
	StepID     string `json:"step_id"`
	Output     string `json:"output,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type AgentRetrieveHitPreview struct {
	TaskID     int64   `json:"task_id"`
	VideoTitle string  `json:"video_title,omitempty"`
	ChunkIndex int     `json:"chunk_index"`
	Score      float32 `json:"score"`
}

type AgentRetrieveHitsEvent struct {
	RunID         string                    `json:"run_id"`
	StepID        string                    `json:"step_id"`
	Query         string                    `json:"query"`
	Hits          int                       `json:"hits"`
	Sources       []string                  `json:"sources,omitempty"`
	ChunksPreview []AgentRetrieveHitPreview `json:"chunks_preview,omitempty"`
}

type AgentDoneEvent struct {
	RunID        string                      `json:"run_id"`
	MessageID    int64                       `json:"message_id"`
	Degraded     bool                        `json:"degraded"`
	TraceSummary AgentTraceSummary           `json:"trace_summary"`
	MemoryPolicy model.EffectiveMemoryPolicy `json:"memory_policy"`
}

type AgentTraceSummary struct {
	Steps      int `json:"steps"`
	Tools      int `json:"tools"`
	Retrievals int `json:"retrievals"`
}

type AgentErrorEvent struct {
	RunID   string `json:"run_id,omitempty"`
	Message string `json:"message"`
	StepID  string `json:"step_id,omitempty"`
}

// VideoAgentStepObserver observes actual typed tool execution. It is kept at
// the tool seam so Ask and the stream endpoint share the same agent workflow.
type VideoAgentStepObserver interface {
	StepStart(step VideoAgentStep) error
	StepDone(step VideoAgentStep, output any) error
	StepError(step VideoAgentStep, err error) error
}

type videoAgentStreamObserver struct {
	runID         string
	emit          func(AgentStreamEvent) error
	nextStep      int
	active        *observedAgentStep
	pendingAnswer *observedAgentStep
}

type observedAgentStep struct {
	id        string
	step      VideoAgentStep
	startedAt time.Time
}

func newVideoAgentStreamObserver(runID string, emit func(AgentStreamEvent) error) *videoAgentStreamObserver {
	return &videoAgentStreamObserver{runID: runID, emit: emit}
}

func (o *videoAgentStreamObserver) StepStart(step VideoAgentStep) error {
	if o == nil || o.emit == nil {
		return fmt.Errorf("agent stream observer 不能为空")
	}
	if o.active != nil {
		return fmt.Errorf("agent stream step %s 尚未结束", o.active.id)
	}
	if o.pendingAnswer != nil {
		if err := o.finishPendingAnswer(); err != nil {
			return err
		}
	}
	o.nextStep++
	observed := &observedAgentStep{
		id:        fmt.Sprintf("s%d", o.nextStep),
		step:      step,
		startedAt: time.Now(),
	}
	o.active = observed
	if err := o.emitEvent(AgentEventStepStart, o.stepEvent(observed, "running", "")); err != nil {
		return err
	}
	return o.emitEvent(AgentEventToolCall, AgentToolCallEvent{
		RunID:  o.runID,
		StepID: observed.id,
		Tool:   step.Tool,
		Input:  step.Input,
	})
}

func (o *videoAgentStreamObserver) StepDone(step VideoAgentStep, output any) error {
	if o == nil || o.active == nil {
		return fmt.Errorf("agent stream step done 没有对应的 step_start")
	}
	observed := o.active
	observed.step = step
	if err := o.emitEvent(AgentEventToolResult, AgentToolResultEvent{
		RunID:      o.runID,
		StepID:     observed.id,
		Output:     agentToolOutputRef(step),
		DurationMs: time.Since(observed.startedAt).Milliseconds(),
	}); err != nil {
		return err
	}
	if step.Tool == VideoAgentToolSearchTranscript {
		if result, ok := output.(SearchTranscriptResult); ok {
			if err := o.emitRetrieveHits(observed.id, step, result); err != nil {
				return err
			}
		}
	}
	if step.Tool == VideoAgentToolBuildCitedAnswer {
		o.pendingAnswer = observed
		o.active = nil
		return nil
	}
	if err := o.emitEvent(AgentEventStepDone, o.stepEvent(observed, "done", "")); err != nil {
		return err
	}
	o.active = nil
	return nil
}

func (o *videoAgentStreamObserver) StepError(step VideoAgentStep, err error) error {
	if o == nil || o.active == nil {
		return fmt.Errorf("agent stream step error 没有对应的 step_start")
	}
	observed := o.active
	observed.step = step
	message := "agent step failed"
	if err != nil {
		message = err.Error()
	}
	if observed.step.Error == "" {
		observed.step.Error = message
	}
	if emitErr := o.emitEvent(AgentEventStepError, o.stepEvent(observed, "error", message)); emitErr != nil {
		return emitErr
	}
	o.active = nil
	return nil
}

func (o *videoAgentStreamObserver) FinishAnswer() error {
	if o == nil {
		return nil
	}
	return o.finishPendingAnswer()
}

func (o *videoAgentStreamObserver) Abort(err error) error {
	if o == nil {
		return nil
	}
	if o.active == nil && o.pendingAnswer == nil {
		return nil
	}
	message := "agent stream aborted"
	if err != nil {
		message = err.Error()
	}
	if o.active != nil {
		return o.StepError(o.active.step, err)
	}
	pending := o.pendingAnswer
	pending.step.Error = message
	if emitErr := o.emitEvent(AgentEventStepError, o.stepEvent(pending, "error", message)); emitErr != nil {
		return emitErr
	}
	o.pendingAnswer = nil
	return nil
}

func (o *videoAgentStreamObserver) finishPendingAnswer() error {
	if o == nil || o.pendingAnswer == nil {
		return nil
	}
	pending := o.pendingAnswer
	if err := o.emitEvent(AgentEventStepDone, o.stepEvent(pending, "done", "")); err != nil {
		return err
	}
	o.pendingAnswer = nil
	return nil
}

func (o *videoAgentStreamObserver) emitRetrieveHits(stepID string, step VideoAgentStep, result SearchTranscriptResult) error {
	preview := make([]AgentRetrieveHitPreview, 0, len(result.Citations))
	sources := make([]string, 0)
	seenSources := make(map[string]struct{})
	for _, citation := range result.Citations {
		preview = append(preview, AgentRetrieveHitPreview{
			TaskID: citation.TaskID, VideoTitle: citation.VideoTitle,
			ChunkIndex: citation.ChunkIndex, Score: citation.Score,
		})
		if source := strings.TrimSpace(citation.VideoTitle); source != "" {
			if _, exists := seenSources[source]; !exists {
				seenSources[source] = struct{}{}
				sources = append(sources, source)
			}
		}
	}
	query := ""
	if step.Input != nil {
		if value, ok := step.Input["question"].(string); ok {
			query = value
		}
	}
	return o.emitEvent(AgentEventRetrieveHits, AgentRetrieveHitsEvent{
		RunID: o.runID, StepID: stepID, Query: query,
		Hits: len(result.Citations), Sources: sources, ChunksPreview: preview,
	})
}

func (o *videoAgentStreamObserver) stepEvent(observed *observedAgentStep, status, message string) AgentStepEvent {
	step := observed.step
	return AgentStepEvent{
		RunID: o.runID, StepID: observed.id, Kind: videoAgentStepKind(step.Tool),
		Label: step.Name, Status: status, Tool: step.Tool, Input: step.Input,
		Output: step.OutputRef, Error: firstNonEmpty(step.Error, message),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (o *videoAgentStreamObserver) emitEvent(eventType string, data any) error {
	return o.emit(AgentStreamEvent{Type: eventType, Data: data})
}

func agentToolOutputRef(step VideoAgentStep) string {
	if output := strings.TrimSpace(step.OutputRef); output != "" {
		return output
	}
	return "completed"
}

func videoAgentStepKind(tool string) string {
	switch tool {
	case VideoAgentToolSearchTranscript:
		return "retrieve"
	case VideoAgentToolBuildCitedAnswer:
		return "answer"
	default:
		return "tool"
	}
}

func agentTraceSummary(trace []VideoAgentStep) AgentTraceSummary {
	summary := AgentTraceSummary{Steps: len(trace)}
	for _, step := range trace {
		if strings.TrimSpace(step.Tool) != "" {
			summary.Tools++
		}
		if step.Tool == VideoAgentToolSearchTranscript {
			summary.Retrievals++
		}
	}
	return summary
}

// AskStream/Stream run the existing bounded template Agent and adapt its
// actual tool execution to SSE events. They intentionally do not expose
// research mode or knowledge-base scope in this first streaming slice.
func (s *VideoAgentService) AskStream(ctx context.Context, req VideoAgentStreamRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile, emit func(AgentStreamEvent) error) (*VideoAgentResult, error) {
	return s.Stream(ctx, req, embedding, chat, profile, emit)
}

func (s *VideoAgentService) Stream(ctx context.Context, req VideoAgentStreamRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile, emit func(AgentStreamEvent) error) (*VideoAgentResult, error) {
	if emit == nil {
		return nil, fmt.Errorf("agent stream emit 不能为空")
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	if strings.TrimSpace(req.Mode) == "" {
		req.Mode = AgentStreamMode
	}
	if req.Mode != AgentStreamMode {
		return nil, fmt.Errorf("Agent 流式接口仅支持 mode=%s", AgentStreamMode)
	}
	session, err := s.findVideoAgentSession(req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}
	memoryPolicy := s.chatSvc.effectiveMemoryPolicyForRequest(ctx, session)
	runID := uuid.NewString()
	streamEmit := func(event AgentStreamEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return emit(event)
	}
	if err := streamEmit(AgentStreamEvent{
		Type: AgentEventRunStart,
		Data: AgentRunStartEvent{
			RunID: runID, Mode: req.Mode, ScopeType: session.ScopeType,
			TaskID: session.TaskID, KnowledgeBaseID: session.KnowledgeBaseID,
			MemoryPolicy: memoryPolicy,
		},
	}); err != nil {
		return nil, err
	}

	observer := newVideoAgentStreamObserver(runID, streamEmit)
	result, err := s.ask(ctx, VideoAgentRequest{
		UserID: req.UserID, SessionID: req.SessionID, Question: req.Question, TopK: req.TopK, MemoryPolicy: &memoryPolicy,
	}, embedding, chat, profile, observer, runID, req.Mode, req.AgentProfile)
	if err != nil {
		_ = observer.Abort(err)
		return nil, err
	}
	for _, chunk := range splitAnswerForStream(result.Answer, 80) {
		if err := streamEmit(AgentStreamEvent{Type: AgentEventAnswer, Data: chunk}); err != nil {
			_ = observer.Abort(err)
			return nil, err
		}
	}
	if err := streamEmit(AgentStreamEvent{Type: AgentEventCitations, Data: result.Citations}); err != nil {
		_ = observer.Abort(err)
		return nil, err
	}
	if err := observer.FinishAnswer(); err != nil {
		_ = observer.Abort(err)
		return nil, err
	}
	if err := streamEmit(AgentStreamEvent{Type: AgentEventDone, Data: AgentDoneEvent{
		RunID: result.RunID, MessageID: result.MessageID, Degraded: result.Answer == inspectorBlockedAnswer,
		TraceSummary: agentTraceSummary(result.Trace),
		MemoryPolicy: result.MemoryPolicy,
	}}); err != nil {
		return nil, err
	}
	return result, nil
}
