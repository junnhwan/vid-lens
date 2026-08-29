package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// VideoAgentToolDefinition is the stable, planner-facing description of a
// tool. The implementation details stay behind VideoAgentTool.
type VideoAgentToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// VideoAgentToolRuntime contains request-scoped dependencies that must not be
// serialized into a planner action. Tool arguments remain JSON so a future
// planner can produce them without knowing concrete Go input types.
type VideoAgentToolRuntime struct {
	UserID         int64
	TaskID         int64
	Recent         []model.ChatMessage
	TopK           int
	EmbeddingModel string
	Embedding      ai.EmbeddingClient
}

// VideoAgentToolRequest is the only input surface exposed by the registry.
// Implementations validate and decode Arguments themselves.
type VideoAgentToolRequest struct {
	Runtime   VideoAgentToolRuntime
	Arguments json.RawMessage
}

// VideoAgentToolResult keeps the serialized observation and the existing
// execution trace together. The serialized observation is the seam that lets
// a planner/observer evolve without exposing every concrete tool result type.
type VideoAgentToolResult struct {
	Output json.RawMessage
	Step   VideoAgentStep
}

// VideoAgentTool is intentionally small: callers discover a tool, execute it
// with request-scoped runtime data, and inspect a structured observation.
type VideoAgentTool interface {
	Definition() VideoAgentToolDefinition
	Execute(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error)
}

// VideoAgentToolRegistry is the allow-list and dispatch seam for experimental
// video-agent tools. It owns duplicate-name and unknown-tool validation so
// future planners cannot call arbitrary code paths.
type VideoAgentToolRegistry struct {
	tools map[string]VideoAgentTool
}

func NewVideoAgentToolRegistry() *VideoAgentToolRegistry {
	return &VideoAgentToolRegistry{tools: make(map[string]VideoAgentTool)}
}

func (r *VideoAgentToolRegistry) Register(tool VideoAgentTool) error {
	if r == nil {
		return errors.New("video agent tool registry 不能为空")
	}
	if tool == nil {
		return errors.New("video agent tool 不能为空")
	}
	if r.tools == nil {
		r.tools = make(map[string]VideoAgentTool)
	}
	definition := tool.Definition()
	name := strings.TrimSpace(definition.Name)
	if name == "" {
		return errors.New("video agent tool 名称不能为空")
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("video agent tool 已注册: %s", name)
	}
	definition.Name = name
	r.tools[name] = &definedVideoAgentTool{definition: definition, implementation: tool}
	return nil
}

func (r *VideoAgentToolRegistry) Lookup(name string) (VideoAgentTool, error) {
	if r == nil {
		return nil, errors.New("video agent tool registry 不能为空")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("video agent tool 名称不能为空")
	}
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("未注册的 video agent tool: %s", name)
	}
	return tool, nil
}

func (r *VideoAgentToolRegistry) Definitions() []VideoAgentToolDefinition {
	if r == nil {
		return nil
	}
	definitions := make([]VideoAgentToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		definitions = append(definitions, tool.Definition())
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *VideoAgentToolRegistry) Execute(ctx context.Context, name string, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	tool, err := r.Lookup(name)
	if err != nil {
		return VideoAgentToolResult{
			Step: VideoAgentStep{Tool: strings.TrimSpace(name), Error: err.Error()},
		}, err
	}
	result, err := tool.Execute(ctx, request)
	if result.Step.Tool == "" {
		result.Step.Tool = tool.Definition().Name
	}
	return result, err
}

type definedVideoAgentTool struct {
	definition     VideoAgentToolDefinition
	implementation VideoAgentTool
}

func (t *definedVideoAgentTool) Definition() VideoAgentToolDefinition {
	return t.definition
}

func (t *definedVideoAgentTool) Execute(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	return t.implementation.Execute(ctx, request)
}

type videoAgentToolAdapter struct {
	definition VideoAgentToolDefinition
	execute    func(context.Context, VideoAgentToolRequest) (VideoAgentToolResult, error)
}

func (a *videoAgentToolAdapter) Definition() VideoAgentToolDefinition {
	return a.definition
}

func (a *videoAgentToolAdapter) Execute(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
	return a.execute(ctx, request)
}

type searchTranscriptToolArguments struct {
	Question string `json:"question"`
	TopK     int    `json:"top_k"`
}

type transcriptWindowToolArguments struct {
	ChunkIndex int `json:"chunk_index"`
	Radius     int `json:"radius"`
}

type summarizeSegmentsToolArguments struct {
	Question string              `json:"question"`
	Segments []TranscriptSegment `json:"segments"`
}

type compareSegmentsToolArguments struct {
	Question string                   `json:"question"`
	Groups   []TranscriptSegmentGroup `json:"groups"`
}

type buildCitedAnswerToolArguments struct {
	Question     string           `json:"question"`
	Intermediate string           `json:"intermediate"`
	Citations    []RetrievedChunk `json:"citations"`
}

func newVideoAgentToolRegistry(tools *VideoAgentTools) *VideoAgentToolRegistry {
	registry := NewVideoAgentToolRegistry()
	for _, tool := range defaultVideoAgentToolAdapters(tools) {
		if err := registry.Register(tool); err != nil {
			panic(fmt.Sprintf("register default video agent tool: %v", err))
		}
	}
	return registry
}

func defaultVideoAgentToolAdapters(tools *VideoAgentTools) []VideoAgentTool {
	return []VideoAgentTool{
		&videoAgentToolAdapter{
			definition: VideoAgentToolDefinition{
				Name:        VideoAgentToolSearchTranscript,
				Description: "在当前视频中检索与问题相关的转写证据。",
			},
			execute: func(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
				var args searchTranscriptToolArguments
				if err := decodeVideoAgentToolArguments(request, &args); err != nil {
					return failedVideoAgentToolResult(VideoAgentToolSearchTranscript, "search topic", err)
				}
				topK := args.TopK
				if topK <= 0 {
					topK = request.Runtime.TopK
				}
				result, step, err := tools.SearchTranscript(ctx, SearchTranscriptInput{
					UserID:         request.Runtime.UserID,
					TaskID:         request.Runtime.TaskID,
					Question:       args.Question,
					Recent:         request.Runtime.Recent,
					TopK:           topK,
					EmbeddingModel: request.Runtime.EmbeddingModel,
					Embedding:      request.Runtime.Embedding,
				})
				return marshalVideoAgentToolResult(result, step, err)
			},
		},
		&videoAgentToolAdapter{
			definition: VideoAgentToolDefinition{
				Name:        VideoAgentToolGetTranscriptWindow,
				Description: "加载命中转写片段附近的上下文窗口。",
			},
			execute: func(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
				var args transcriptWindowToolArguments
				if err := decodeVideoAgentToolArguments(request, &args); err != nil {
					return failedVideoAgentToolResult(VideoAgentToolGetTranscriptWindow, "load transcript window", err)
				}
				result, step, err := tools.GetTranscriptWindow(ctx, TranscriptWindowInput{
					UserID:         request.Runtime.UserID,
					TaskID:         request.Runtime.TaskID,
					EmbeddingModel: request.Runtime.EmbeddingModel,
					ChunkIndex:     args.ChunkIndex,
					Radius:         args.Radius,
				})
				return marshalVideoAgentToolResult(result, step, err)
			},
		},
		&videoAgentToolAdapter{
			definition: VideoAgentToolDefinition{
				Name:        VideoAgentToolSummarizeSegments,
				Description: "只基于给定转写片段提取与问题相关的要点。",
			},
			execute: func(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
				var args summarizeSegmentsToolArguments
				if err := decodeVideoAgentToolArguments(request, &args); err != nil {
					return failedVideoAgentToolResult(VideoAgentToolSummarizeSegments, "summarize segments", err)
				}
				result, step, err := tools.SummarizeSegments(ctx, SummarizeSegmentsInput{
					Question: args.Question,
					Segments: args.Segments,
				})
				return marshalVideoAgentToolResult(result, step, err)
			},
		},
		&videoAgentToolAdapter{
			definition: VideoAgentToolDefinition{
				Name:        VideoAgentToolCompareSegments,
				Description: "比较多个转写片段组中的共同点、差异和变化。",
			},
			execute: func(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
				var args compareSegmentsToolArguments
				if err := decodeVideoAgentToolArguments(request, &args); err != nil {
					return failedVideoAgentToolResult(VideoAgentToolCompareSegments, "compare segments", err)
				}
				result, step, err := tools.CompareSegments(ctx, CompareSegmentsInput{
					Question: args.Question,
					Groups:   args.Groups,
				})
				return marshalVideoAgentToolResult(result, step, err)
			},
		},
		&videoAgentToolAdapter{
			definition: VideoAgentToolDefinition{
				Name:        VideoAgentToolBuildCitedAnswer,
				Description: "基于中间结论和允许的引用片段生成最终回答。",
			},
			execute: func(ctx context.Context, request VideoAgentToolRequest) (VideoAgentToolResult, error) {
				var args buildCitedAnswerToolArguments
				if err := decodeVideoAgentToolArguments(request, &args); err != nil {
					return failedVideoAgentToolResult(VideoAgentToolBuildCitedAnswer, "build cited answer", err)
				}
				result, step, err := tools.BuildCitedAnswer(ctx, BuildCitedAnswerInput{
					Question:     args.Question,
					Intermediate: args.Intermediate,
					Citations:    args.Citations,
				})
				return marshalVideoAgentToolResult(result, step, err)
			},
		},
	}
}

func decodeVideoAgentToolArguments(request VideoAgentToolRequest, target any) error {
	if len(request.Arguments) == 0 {
		return errors.New("tool arguments 不能为空")
	}
	if err := json.Unmarshal(request.Arguments, target); err != nil {
		return fmt.Errorf("tool arguments 无效: %w", err)
	}
	return nil
}

func marshalVideoAgentToolResult(value any, step VideoAgentStep, executionErr error) (VideoAgentToolResult, error) {
	if executionErr != nil {
		return VideoAgentToolResult{Step: step}, executionErr
	}
	output, err := json.Marshal(value)
	if err != nil {
		step, stepErr := failVideoAgentStep(step, fmt.Sprintf("tool output 序列化失败: %v", err))
		return VideoAgentToolResult{Step: step}, stepErr
	}
	return VideoAgentToolResult{Output: output, Step: step}, nil
}

func failedVideoAgentToolResult(tool, name string, err error) (VideoAgentToolResult, error) {
	step, stepErr := failVideoAgentStep(newVideoAgentStep(name, tool, nil), err.Error())
	return VideoAgentToolResult{Step: step}, stepErr
}
