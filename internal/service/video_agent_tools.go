package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"
)

const (
	VideoAgentToolSearchTranscript     = "search_transcript"
	VideoAgentToolGetTranscriptWindow  = "get_transcript_window"
	VideoAgentToolSummarizeSegments    = "summarize_segments"
	VideoAgentToolCompareSegments      = "compare_segments"
	VideoAgentToolBuildCitedAnswer     = "build_cited_answer"
	VideoAgentToolSearchVisualEvidence = "search_visual_evidence"
	VideoAgentToolInspectVisualWindow  = "inspect_visual_window"
	VideoAgentToolInvestigateVisual    = "investigate_visual"
)

type VideoAgentTools struct {
	emitAnswer         func(string) error
	repos              *repository.Repositories
	pipeline           *RetrievalPipeline
	chat               ai.ChatClient
	registry           *VideoAgentToolRegistry
	observer           VideoAgentStepObserver
	memory             *MemorySnapshot
	visualInvestigator VisualInvestigator
}

func NewVideoAgentTools(repos *repository.Repositories, pipeline *RetrievalPipeline, chat ai.ChatClient) *VideoAgentTools {
	tools := &VideoAgentTools{repos: repos, pipeline: pipeline, chat: chat}
	tools.registry = newVideoAgentToolRegistry(tools)
	return tools
}

// Registry exposes the allow-listed tool seam to the future planner/executor.
// Existing typed methods remain available while the template workflow is the
// compatibility baseline.
func (t *VideoAgentTools) Registry() *VideoAgentToolRegistry {
	if t == nil {
		return nil
	}
	return t.registry
}

// A tool's bounded result count takes precedence over the deployment default.
// Copy the configuration so one search cannot change subsequent tool calls.
func (t *VideoAgentTools) retrievalPipelineForTopK(topK int) *RetrievalPipeline {
	pipeline := *t.pipeline
	if topK > 0 && pipeline.Config != nil {
		cfg := *pipeline.Config
		cfg.TopK = topK
		pipeline.Config = &cfg
	}
	return &pipeline
}

// SetStepObserver attaches an optional observer to the existing typed tool
// methods. The default non-streaming Agent leaves it unset.
func (t *VideoAgentTools) SetStepObserver(observer VideoAgentStepObserver) {
	if t != nil {
		t.observer = observer
	}
}

// SetMemorySnapshot injects only a server-created, request-local snapshot.
// Planner/tool arguments never carry memory context.
func (t *VideoAgentTools) SetMemorySnapshot(snapshot *MemorySnapshot) {
	if t != nil {
		t.memory = snapshot
	}
}

// SetVisualInvestigator adds the query-time visual tool only when the server
// has wired the media materializer. Keeping it optional preserves the existing
// offline-text agent baseline and makes feature availability explicit.
func (t *VideoAgentTools) SetVisualInvestigator(investigator VisualInvestigator) {
	if t == nil {
		return
	}
	t.visualInvestigator = investigator
	if investigator == nil || t.registry == nil {
		return
	}
	if _, err := t.registry.Lookup(VideoAgentToolInvestigateVisual); err == nil {
		return
	}
	_ = t.registry.Register(&videoAgentToolAdapter{
		definition: VideoAgentToolDefinition{
			Name:        VideoAgentToolInvestigateVisual,
			Description: "在当前视频已有时间线/视觉证据定位的小窗口内，按硬预算读取少量原始帧并返回可回放的查询时视觉观察。",
		},
		execute: func(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
			var args investigateVisualToolArguments
			if err := decodeVideoAgentToolArguments(request, &args); err != nil {
				return failedVideoAgentToolResult(VideoAgentToolInvestigateVisual, "investigate visual", err)
			}
			result, step, err := t.InvestigateVisual(ctx, InvestigateVisualInput{
				UserID: request.Runtime.UserID, TaskID: request.Runtime.TaskID,
				Goal: args.Goal, RequiredFacts: args.RequiredFacts, SeedWindows: args.SeedWindows, Budget: args.Budget,
			})
			return marshalVideoAgentToolResult(result, step, err)
		},
	})
}

type SearchTranscriptInput struct {
	TaskIDs        []int64
	UserID         int64
	TaskID         int64
	Question       string
	Recent         []model.ChatMessage
	TopK           int
	EmbeddingModel string
	Embedding      ai.EmbeddingClient
}

type SearchTranscriptResult struct {
	Citations []RetrievedChunk
	Rewrite   RewriteResult
	Trace     RetrievalTrace
}

type SearchVisualEvidenceInput = SearchTranscriptInput
type SearchVisualEvidenceResult = SearchTranscriptResult

type InspectVisualWindowInput struct {
	UserID         int64
	TaskID         int64
	EmbeddingModel string
	StartMS        int64
	EndMS          int64
	MaxFrames      int
}

type InspectVisualWindowResult struct {
	StartMS  int64            `json:"start_ms"`
	EndMS    int64            `json:"end_ms"`
	Evidence []RetrievedChunk `json:"evidence"`
}

type InvestigateVisualInput struct {
	UserID        int64
	TaskID        int64
	Goal          string
	RequiredFacts []RequiredFact
	SeedWindows   []VisualTimeRange
	Budget        VisualBudget
}

type InvestigateVisualResult = Investigation

type TranscriptWindowInput struct {
	UserID         int64
	TaskID         int64
	EmbeddingModel string
	ChunkIndex     int
	Radius         int
}

type TranscriptWindowResult struct {
	StartIndex int
	EndIndex   int
	Segments   []TranscriptSegment
	Content    string
}

type TranscriptSegment struct {
	ChunkID    int64  `json:"chunk_id,omitempty"`
	ChunkIndex int    `json:"chunk_index"`
	Content    string `json:"content"`
}

type BuildCitedAnswerInput struct {
	Question     string
	Intermediate string
	Citations    []RetrievedChunk
}

type BuildCitedAnswerResult struct {
	Answer    string
	Citations []RetrievedChunk
}

func (t *VideoAgentTools) SearchTranscript(ctx context.Context, input SearchTranscriptInput) (SearchTranscriptResult, VideoAgentStep, error) {
	step := newVideoAgentStep("search topic", VideoAgentToolSearchTranscript, map[string]any{
		"question": input.Question,
		"top_k":    input.TopK,
	})
	if err := t.notifyStepStart(step); err != nil {
		return SearchTranscriptResult{}, step, err
	}
	if t == nil || t.pipeline == nil {
		step, err := t.failObservedStep(step, "当前视频尚未构建 RAG 索引")
		return SearchTranscriptResult{}, step, err
	}
	result, err := t.retrievalPipelineForTopK(input.TopK).Retrieve(ctx, RetrievalPipelineRequest{
		UserID: input.UserID, TaskID: input.TaskID, TaskIDs: input.TaskIDs, Question: input.Question, Recent: input.Recent,
		TopK: input.TopK, EmbeddingModel: input.EmbeddingModel, Embedding: input.Embedding,
	})
	if err != nil {
		step, err = t.failObservedStepWithCause(step, err)
		return SearchTranscriptResult{}, step, err
	}
	step.OutputRef = fmt.Sprintf("citations:%d", len(result.Citations))
	searchResult := SearchTranscriptResult(result)
	if err := t.notifyStepDone(step, searchResult); err != nil {
		return SearchTranscriptResult{}, step, err
	}
	return searchResult, step, nil
}

func (t *VideoAgentTools) SearchVisualEvidence(ctx context.Context, input SearchVisualEvidenceInput) (SearchVisualEvidenceResult, VideoAgentStep, error) {
	step := newVideoAgentStep("search visual evidence", VideoAgentToolSearchVisualEvidence, map[string]any{"question": input.Question, "top_k": input.TopK})
	if err := t.notifyStepStart(step); err != nil {
		return SearchVisualEvidenceResult{}, step, err
	}
	if t == nil || t.pipeline == nil {
		step, err := t.failObservedStep(step, "当前视频尚未构建视觉索引")
		return SearchVisualEvidenceResult{}, step, err
	}
	result, err := t.retrievalPipelineForTopK(input.TopK).Retrieve(ctx, RetrievalPipelineRequest{
		UserID: input.UserID, TaskID: input.TaskID, TaskIDs: input.TaskIDs, Question: input.Question, Recent: input.Recent,
		TopK: input.TopK, EmbeddingModel: input.EmbeddingModel, Embedding: input.Embedding,
		Modalities: []string{model.ChunkModalityVisualOCR, model.ChunkModalityVisualCaption},
	})
	if err != nil {
		step, err = t.failObservedStepWithCause(step, err)
		return SearchVisualEvidenceResult{}, step, err
	}
	step.OutputRef = fmt.Sprintf("visual-citations:%d", len(result.Citations))
	searchResult := SearchVisualEvidenceResult(result)
	if err := t.notifyStepDone(step, searchResult); err != nil {
		return SearchVisualEvidenceResult{}, step, err
	}
	return searchResult, step, nil
}

func (t *VideoAgentTools) InspectVisualWindow(ctx context.Context, input InspectVisualWindowInput) (InspectVisualWindowResult, VideoAgentStep, error) {
	step := newVideoAgentStep("inspect visual window", VideoAgentToolInspectVisualWindow, map[string]any{"start_ms": input.StartMS, "end_ms": input.EndMS, "max_frames": input.MaxFrames})
	if err := t.notifyStepStart(step); err != nil {
		return InspectVisualWindowResult{}, step, err
	}
	metricStatus := "failed"
	defer func() {
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveMultimodalEvidence("agent_inspect", "visual", metricStatus)
		}
	}()
	if t == nil || t.repos == nil || t.repos.VideoChunk == nil {
		step, err := t.failObservedStep(step, "visual chunk repository unavailable")
		return InspectVisualWindowResult{}, step, err
	}
	if input.StartMS < 0 || input.EndMS <= input.StartMS || input.EndMS-input.StartMS > 10*60*1000 {
		step, err := t.failObservedStep(step, "visual window 必须是十分钟内的合法半开时间范围")
		return InspectVisualWindowResult{}, step, err
	}
	maxFrames := input.MaxFrames
	if maxFrames <= 0 {
		maxFrames = 3
	}
	if maxFrames > 8 {
		maxFrames = 8
	}
	rows, err := t.repos.VideoChunk.ListVisualByTimeRange(input.UserID, input.TaskID, input.EmbeddingModel, input.StartMS, input.EndMS, maxFrames*2)
	if err != nil {
		step, err = t.failObservedStepWithCause(step, err)
		return InspectVisualWindowResult{}, step, err
	}
	evidence := make([]RetrievedChunk, 0, len(rows))
	seenFrames := make(map[string]struct{})
	for _, row := range rows {
		refs := sourceRefsForModelChunk(row)
		frameKey := row.VectorID
		if len(refs) > 0 && refs[0].StableID != "" {
			frameKey = refs[0].StableID
		}
		if _, ok := seenFrames[frameKey]; !ok && len(seenFrames) >= maxFrames {
			continue
		}
		seenFrames[frameKey] = struct{}{}
		chunk := RetrievedChunk{TaskID: row.TaskID, EvidenceID: row.VectorID, ChunkID: row.ID, ChunkIndex: row.ChunkIndex, Content: row.Content, Source: "visual_inspection"}
		applyChunkProvenance(&chunk, row)
		evidence = append(evidence, chunk)
	}
	result := InspectVisualWindowResult{StartMS: input.StartMS, EndMS: input.EndMS, Evidence: evidence}
	step.OutputRef = fmt.Sprintf("visual-window:%d-%d:%d", input.StartMS, input.EndMS, len(evidence))
	if err := t.notifyStepDone(step, result); err != nil {
		return InspectVisualWindowResult{}, step, err
	}
	metricStatus = "success"
	return result, step, nil
}

func (t *VideoAgentTools) InvestigateVisual(ctx context.Context, input InvestigateVisualInput) (InvestigateVisualResult, VideoAgentStep, error) {
	step := newVideoAgentStep("investigate visual", VideoAgentToolInvestigateVisual, map[string]any{
		"seed_windows": input.SeedWindows, "max_frames": input.Budget.MaxFrames, "max_vlm_calls": input.Budget.MaxVLMCalls,
	})
	if err := t.notifyStepStart(step); err != nil {
		return InvestigateVisualResult{}, step, err
	}
	if t == nil || t.visualInvestigator == nil {
		step, err := t.failObservedStep(step, "query-time visual investigator unavailable")
		return InvestigateVisualResult{}, step, err
	}
	result, err := t.visualInvestigator.Inspect(ctx, InspectRequest{
		UserID: input.UserID, TaskID: input.TaskID, Goal: input.Goal,
		RequiredFacts: input.RequiredFacts, SeedWindows: input.SeedWindows, Budget: input.Budget,
	})
	if err != nil {
		step, err = t.failObservedStepWithCause(step, err)
		return InvestigateVisualResult{}, step, err
	}
	step.OutputRef = fmt.Sprintf("visual-investigation:%s:%d", result.TraceRef, len(result.Observations))
	if err := t.notifyStepDone(step, result); err != nil {
		return InvestigateVisualResult{}, step, err
	}
	return result, step, nil
}

func (t *VideoAgentTools) GetTranscriptWindow(ctx context.Context, input TranscriptWindowInput) (TranscriptWindowResult, VideoAgentStep, error) {
	step := newVideoAgentStep("load transcript window", VideoAgentToolGetTranscriptWindow, map[string]any{
		"chunk_index": input.ChunkIndex,
		"radius":      input.Radius,
	})
	if err := t.notifyStepStart(step); err != nil {
		return TranscriptWindowResult{}, step, err
	}
	if t == nil || t.repos == nil || t.repos.VideoChunk == nil {
		step, err := t.failObservedStep(step, "transcript chunk repository unavailable")
		return TranscriptWindowResult{}, step, err
	}
	radius := input.Radius
	if radius < 0 {
		radius = 0
	}
	start := input.ChunkIndex - radius
	if start < 0 {
		start = 0
	}
	end := input.ChunkIndex + radius
	chunks, err := t.repos.VideoChunk.ListByIndexRange(input.UserID, input.TaskID, input.EmbeddingModel, start, end)
	if err != nil {
		step, err = t.failObservedStepWithCause(step, err)
		return TranscriptWindowResult{}, step, err
	}
	if len(chunks) == 0 {
		step, err := t.failObservedStep(step, "未找到相邻转写片段")
		return TranscriptWindowResult{}, step, err
	}
	segments := videoChunksToSegments(chunks)
	result := TranscriptWindowResult{
		StartIndex: chunks[0].ChunkIndex,
		EndIndex:   chunks[len(chunks)-1].ChunkIndex,
		Segments:   segments,
		Content:    joinTranscriptSegments(segments),
	}
	step.OutputRef = fmt.Sprintf("window:%d-%d", result.StartIndex, result.EndIndex)
	if err := t.notifyStepDone(step, result); err != nil {
		return TranscriptWindowResult{}, step, err
	}
	return result, step, nil
}

func (t *VideoAgentTools) BuildCitedAnswer(ctx context.Context, input BuildCitedAnswerInput) (BuildCitedAnswerResult, VideoAgentStep, error) {
	step := newVideoAgentStep("build cited answer", VideoAgentToolBuildCitedAnswer, map[string]any{
		"citation_count": len(input.Citations),
	})
	if err := t.notifyStepStart(step); err != nil {
		return BuildCitedAnswerResult{}, step, err
	}
	if t == nil || t.chat == nil {
		step, err := t.failObservedStep(step, "chat client 不能为空")
		return BuildCitedAnswerResult{}, step, err
	}
	messages := []ai.ChatMessage{
		{Role: "system", Content: "你是 VidLens 的视频内容回答生成工具。只能基于中间结论和引用片段回答，不能使用外部知识。引用片段包含证据模态和半开时间范围；回答具体事实必须绑定这些信息。若 transcript、visual_ocr、visual_caption 冲突，分别陈述并明确不确定性，不得擅自选择一方覆盖另一方。证据编号是内部标记。回答涉及具体事实时，请在对应事实后使用独立格式 [C1][C2] 标注证据，不要写成 [C1, C2]。系统会在展示前隐藏这些标记。"},
	}
	if memoryContext := trustedMemoryPromptContext(t.memory); memoryContext != "" {
		messages = append(messages, ai.ChatMessage{Role: "system", Content: memoryContext + "\n禁止把上述记忆作为 Claim 或引用证据；若它与当前视频片段冲突，以当前视频片段为准并说明不确定性。"})
	}
	messages = append(messages, ai.ChatMessage{Role: "user", Content: fmt.Sprintf("用户问题：%s\n\n中间结论：\n%s\n\n引用片段：\n%s\n\n请生成最终回答。", input.Question, input.Intermediate, formatRetrievedChunks(input.Citations))})
	var answer string
	var err error
	if t.emitAnswer != nil {
		err = ai.StreamResponse(ctx, t.chat, messages, func(event ai.StreamDelta) error {
			if event.Kind == "reasoning" {
				return emitReasoning(ctx, "answer", event.Text)
			}
			delta := event.Text
			if err := ctx.Err(); err != nil {
				return err
			}
			answer += delta
			return t.emitAnswer(delta)
		})
	} else {
		answer, err = t.chat.Chat(ctx, messages)
		if err == nil && t.emitAnswer != nil {
			err = t.emitAnswer(answer)
		}
	}
	if err != nil {
		step, err = t.failObservedStepWithCause(step, err)
		return BuildCitedAnswerResult{}, step, err
	}
	step.OutputRef = "answer"
	result := BuildCitedAnswerResult{
		Answer:    strings.TrimSpace(answer),
		Citations: append([]RetrievedChunk(nil), input.Citations...),
	}
	if err := t.notifyStepDone(step, result); err != nil {
		return BuildCitedAnswerResult{}, step, err
	}
	return result, step, nil
}

func trustedMemoryPromptContext(snapshot *MemorySnapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.PromptContext()
}

func (t *VideoAgentTools) notifyStepStart(step VideoAgentStep) error {
	if t == nil || t.observer == nil {
		return nil
	}
	return t.observer.StepStart(step)
}

func (t *VideoAgentTools) notifyStepDone(step VideoAgentStep, output any) error {
	if t == nil || t.observer == nil {
		return nil
	}
	return t.observer.StepDone(step, output)
}

func (t *VideoAgentTools) failObservedStep(step VideoAgentStep, message string) (VideoAgentStep, error) {
	step, err := failVideoAgentStep(step, message)
	return t.notifyObservedStepError(step, err)
}

func (t *VideoAgentTools) failObservedStepWithCause(step VideoAgentStep, cause error) (VideoAgentStep, error) {
	step.Error = safeAgentError(cause)
	return t.notifyObservedStepError(step, cause)
}

func (t *VideoAgentTools) notifyObservedStepError(step VideoAgentStep, err error) (VideoAgentStep, error) {
	if t == nil || t.observer == nil {
		return step, err
	}
	if observerErr := t.observer.StepError(step, err); observerErr != nil {
		return step, observerErr
	}
	return step, err
}

func newVideoAgentStep(name, tool string, input map[string]any) VideoAgentStep {
	return VideoAgentStep{Name: name, Tool: tool, Input: input}
}

func failVideoAgentStep(step VideoAgentStep, message string) (VideoAgentStep, error) {
	step.Error = message
	return step, errors.New(message)
}

func videoChunksToSegments(chunks []model.VideoChunk) []TranscriptSegment {
	segments := make([]TranscriptSegment, 0, len(chunks))
	for _, chunk := range chunks {
		segments = append(segments, TranscriptSegment{
			ChunkID:    chunk.ID,
			ChunkIndex: chunk.ChunkIndex,
			Content:    chunk.Content,
		})
	}
	return segments
}

func joinTranscriptSegments(segments []TranscriptSegment) string {
	lines := make([]string, 0, len(segments))
	for _, segment := range segments {
		content := strings.TrimSpace(segment.Content)
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[chunk %d] %s", segment.ChunkIndex, content))
	}
	return strings.Join(lines, "\n")
}

func formatRetrievedChunks(chunks []RetrievedChunk) string {
	lines := make([]string, 0, len(chunks))
	for index, chunk := range chunks {
		lines = append(lines, fmt.Sprintf("[C%d] (chunk %d, modality=%s, time=[%d,%d), time_status=%s) %s", index+1, chunk.ChunkIndex,
			chunk.Modality, chunk.StartMS, chunk.EndMS, chunk.TimeRangeStatus, strings.TrimSpace(chunk.Content)))
	}
	return strings.Join(lines, "\n")
}
