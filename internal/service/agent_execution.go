package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

const agentStepLeaseDuration = 2 * time.Minute

type frozenAgentProfile struct {
	LLMProvider             string `json:"llm_provider"`
	LLMModel                string `json:"llm_model"`
	LLMEndpointDigest       string `json:"llm_endpoint_digest"`
	EmbeddingProvider       string `json:"embedding_provider"`
	EmbeddingModel          string `json:"embedding_model"`
	EmbeddingDim            int    `json:"embedding_dim"`
	EmbeddingEndpointDigest string `json:"embedding_endpoint_digest"`
	VisionProvider          string `json:"vision_provider,omitempty"`
	VisionModel             string `json:"vision_model,omitempty"`
	VisionEndpointDigest    string `json:"vision_endpoint_digest,omitempty"`
}

type frozenAgentPolicy struct {
	MemberTaskIDs         []int64  `json:"member_task_ids,omitempty"`
	EngineVersion         int      `json:"engine_version,omitempty"`
	TopK                  int      `json:"top_k"`
	MaxSteps              int      `json:"max_steps"`
	MaxReplans            int      `json:"max_replans"`
	AllowedTools          []string `json:"allowed_tools"`
	MaxWindowSelections   int      `json:"max_window_selections,omitempty"`
	WindowRadius          int      `json:"window_radius,omitempty"`
	MaxVisualCandidates   int      `json:"max_visual_candidates,omitempty"`
	MaxVisualSelections   int      `json:"max_visual_selections,omitempty"`
	MaxFinalEvidenceItems int      `json:"max_final_evidence_items,omitempty"`
}

type frozenAgentBudget struct {
	SchemaVersion       int                        `json:"schema_version,omitempty"`
	ProfileID           int64                      `json:"profile_id,omitempty"`
	Source              string                     `json:"source,omitempty"`
	Requested           *model.AgentBudgetOverride `json:"requested,omitempty"`
	ReserveInputTokens  int64                      `json:"reserve_input_tokens,omitempty"`
	ReserveOutputTokens int64                      `json:"reserve_output_tokens,omitempty"`
	ReserveDurationMs   int64                      `json:"reserve_duration_ms,omitempty"`
	MaxSteps            int                        `json:"max_steps"`
	MaxToolCalls        int                        `json:"max_tool_calls"`
	MaxLLMCalls         int                        `json:"max_llm_calls"`
	MaxVisionCalls      int                        `json:"max_vision_calls"`
	MaxAttemptsPerStep  int                        `json:"max_attempts_per_step"`
	MaxRetrievalCalls   int                        `json:"max_retrieval_calls"`
	MaxVisualCalls      int                        `json:"max_visual_calls"`
	MaxFrames           int                        `json:"max_frames"`
	MaxPromptTokens     int64                      `json:"max_prompt_tokens"`
	MaxCompletionTokens int64                      `json:"max_completion_tokens"`
	MaxCostMicros       int64                      `json:"max_cost_micros"`
	MaxDurationMs       int64                      `json:"max_duration_ms"`
	MaxContextChars     int64                      `json:"max_context_chars"`
}

func (s *VideoAgentService) ensureAgentRun(ctx context.Context, runID string, userID int64, session *model.ChatSession, goal, mode, agentProfile string, profile ai.Profile, policy frozenAgentPolicy, budget frozenAgentBudget) (*model.AgentRun, error) {
	if s == nil || s.executionJournal == nil {
		return nil, errors.New("agent execution repository unavailable")
	}
	return s.executionJournal.EnsureRun(ctx, AgentJournalRunRequest{
		RunID: runID, UserID: userID, Session: session, Goal: goal, Mode: mode, AgentProfile: agentProfile,
		Profile: profile, Policy: policy, Budget: budget,
	})
}

func safeAgentProfile(profile ai.Profile) frozenAgentProfile {
	return frozenAgentProfile{
		LLMProvider: profile.LLMProvider, LLMModel: profile.LLMModel, LLMEndpointDigest: digestAgentValue(strings.TrimSpace(profile.LLMBaseURL)),
		EmbeddingProvider: profile.EmbeddingProvider, EmbeddingModel: profile.EmbeddingModel, EmbeddingDim: profile.EmbeddingDim,
		EmbeddingEndpointDigest: digestAgentValue(strings.TrimSpace(profile.EmbeddingEndpoint)),
		VisionProvider:          profile.VisionProvider, VisionModel: profile.VisionModel, VisionEndpointDigest: digestAgentValue(strings.TrimSpace(profile.VisionBaseURL)),
	}
}

func loopAgentPolicy(topK int, policy VideoAgentLoopPolicy) (frozenAgentPolicy, frozenAgentBudget) {
	return loopAgentPolicyWithVisual(topK, policy, false)
}

func loopAgentPolicyWithVisual(topK int, policy VideoAgentLoopPolicy, visualEnabled bool) (frozenAgentPolicy, frozenAgentBudget) {
	allowed := defaultAgentToolNames()
	maxVisionCalls, maxVisualCalls, maxFrames := 0, 0, 0
	if visualEnabled {
		allowed = append(allowed, VideoAgentToolInvestigateVisual)
		sort.Strings(allowed)
		// One research step may invoke the investigator once. The investigator
		// owns its smaller per-call VLM/frame budget and reports its usage.
		maxVisionCalls, maxVisualCalls, maxFrames = 1, 1, 8
	}
	return frozenAgentPolicy{EngineVersion: 2, TopK: topK, MaxSteps: policy.MaxSteps, MaxReplans: policy.MaxReplans, AllowedTools: allowed}, frozenAgentBudget{
		// Each research iteration has one planner checkpoint plus one tool step.
		MaxSteps: policy.MaxSteps*2 + 1, MaxToolCalls: policy.MaxSteps,
		MaxLLMCalls: policy.MaxSteps*2 + 1, MaxVisionCalls: maxVisionCalls, MaxAttemptsPerStep: 1,
		MaxRetrievalCalls: policy.MaxSteps, MaxVisualCalls: maxVisualCalls, MaxFrames: maxFrames, MaxPromptTokens: 32000,
		MaxCompletionTokens: 8000, MaxCostMicros: 1000000, MaxDurationMs: 300000, MaxContextChars: 100000,
	}
}

func defaultAgentToolNames() []string {
	names := []string{VideoAgentToolSearchTranscript, VideoAgentToolGetTranscriptWindow, VideoAgentToolSearchVisualEvidence, VideoAgentToolInspectVisualWindow, VideoAgentToolBuildCitedAnswer}
	sort.Strings(names)
	return names
}

func (s *VideoAgentService) markAgentRunTerminal(ctx context.Context, userID int64, runID, status, reason string, err error) {
	if s == nil || s.executionJournal == nil {
		return
	}
	s.executionJournal.MarkTerminal(ctx, userID, runID, status, reason, err)
}

func safeAgentError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	runes := []rune(message)
	if len(runes) > 1000 {
		message = string(runes[:1000])
	}
	return message
}

func safeToolReason(tool string) string {
	switch tool {
	case VideoAgentToolSearchTranscript:
		return "retrieve evidence from the current video"
	case VideoAgentToolGetTranscriptWindow:
		return "load context around an observed transcript chunk"
	case VideoAgentToolSummarizeSegments:
		return "summarize selected transcript segments"
	case VideoAgentToolCompareSegments:
		return "compare selected transcript segment groups"
	case VideoAgentToolBuildCitedAnswer:
		return "build the cited answer from observed evidence"
	case VideoAgentToolInvestigateVisual:
		return "inspect bounded raw frames from the current video"
	default:
		return "execute an allow-listed Agent tool"
	}
}

func replaySafeAgentAction(tool string) bool {
	return tool == VideoAgentToolSearchTranscript || tool == VideoAgentToolGetTranscriptWindow || tool == VideoAgentToolSearchVisualEvidence || tool == VideoAgentToolInspectVisualWindow
}

func retrievalAgentAction(tool string) bool {
	return tool == VideoAgentToolSearchTranscript || tool == VideoAgentToolGetTranscriptWindow || tool == VideoAgentToolSearchVisualEvidence || tool == VideoAgentToolInspectVisualWindow
}

func llmAgentAction(tool string) bool {
	return tool == VideoAgentToolBuildCitedAnswer
}

func visionAgentAction(tool string) bool { return tool == VideoAgentToolInvestigateVisual }

func visualAgentAction(tool string) bool { return tool == VideoAgentToolInvestigateVisual }

func visualFrameBudget(tool string, arguments json.RawMessage) int {
	if tool != VideoAgentToolInvestigateVisual {
		return 0
	}
	var input investigateVisualToolArguments
	if err := json.Unmarshal(arguments, &input); err != nil || input.Budget.MaxFrames <= 0 {
		return 8
	}
	if input.Budget.MaxFrames > 8 {
		return 8
	}
	return input.Budget.MaxFrames
}

func digestAgentValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
