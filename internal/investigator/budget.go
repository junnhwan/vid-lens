package investigator

import (
	"context"
	"math"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type callBudget struct {
	Revision               string `json:"revision"`
	Phase                  string `json:"phase"`
	InputChars             int    `json:"input_chars"`
	InputCharsLimit        int    `json:"input_chars_limit"`
	EstimatedTextTokens    int    `json:"estimated_text_tokens"`
	EstimatedImageTokens   int    `json:"estimated_image_tokens"`
	ImageCount             int    `json:"image_count"`
	OutputTokens           int    `json:"output_tokens"`
	SafetyTokens           int    `json:"safety_tokens"`
	ContextTokens          int    `json:"context_tokens,omitempty"`
	ContextCapacityStatus  string `json:"context_capacity_status"`
	Estimator              string `json:"estimator"`
	ConfiguredOutputTokens int    `json:"configured_output_tokens,omitempty"`
	AvailableOutputTokens  int    `json:"available_output_tokens,omitempty"`
	AllocationReason       string `json:"allocation_reason,omitempty"`
	ReviewRequestID        string `json:"review_request_id,omitempty"`
	ReviewUnits            int    `json:"review_units,omitempty"`
}

func separateInputLimit(p config.Profile) int {
	if p.InputCharsLimit > 0 {
		return p.InputCharsLimit
	}
	// Imported profiles used a combined character envelope. Preserve their
	// original effective input allowance once, independently of per-call output.
	return max(0, p.MaxInputChars-4*p.MaxOutputTokens)
}
func estimateTextTokens(raw []byte) int {
	ascii, other := 0, 0
	for _, r := range string(raw) {
		if r < 128 {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+2)/3 + other*2
}
func (s *session) budget(ctx context.Context, in []*schema.Message, toolsChars int) (callBudget, error) {
	raw := domain.JSON(stripPixels(in))
	b := callBudget{Revision: "call-budget-v1", Phase: "investigate", InputChars: utf8.RuneCount(raw) + toolsChars, InputCharsLimit: separateInputLimit(s.profile), EstimatedTextTokens: estimateTextTokens(raw) + (toolsChars+1)/2, OutputTokens: s.profile.MaxOutputTokens, SafetyTokens: 2048, ContextTokens: s.profile.ContextTokens, ContextCapacityStatus: "unknown", Estimator: "conservative heuristic; not a provider tokenizer; image cost model-dependent; billing only from reported usage"}
	if b.ContextTokens > 0 {
		b.ContextCapacityStatus = "configured"
	}
	if p := s.spec.DetailPolicy; p != nil {
		b.OutputTokens = min(s.profile.MaxOutputTokens, p.InvestigationOutputTokens)
		if s.cp.Composing || s.compositionReady {
			b.Phase = "compose"
			b.OutputTokens = min(s.profile.MaxOutputTokens, p.CompositionOutputTokens)
		}
		if s.policy().Paced && !s.policy().ConceptMaps && s.turn == nil && !s.cp.Composing {
			b.OutputTokens = min(b.OutputTokens, 8192)
		}
		if s.policy().ConceptMaps && s.turn == nil && s.compositionReady && !s.cp.Composing {
			b.Phase = "outline"
		}
		if s.turn != nil {
			b.Phase = "answer"
			b.OutputTokens = min(s.profile.MaxOutputTokens, p.CompositionOutputTokens)
		}
		if s.cp.DraftCorrection || s.cp.DraftIssue != nil || s.cp.Repairs > 0 {
			b.Phase = "repair"
			b.OutputTokens = min(s.profile.MaxOutputTokens, p.RepairOutputTokens)
		}
		if s.forceDraftSubmit {
			b.Phase = "final_submit"
			b.OutputTokens = min(s.profile.MaxOutputTokens, p.FinalOutputTokens)
		}
	}
	seen := map[string]bool{}
	for _, msg := range in {
		for _, part := range msg.UserInputMultiContent {
			if part.Type != schema.ChatMessagePartTypeImageURL {
				continue
			}
			id, _ := part.Extra["frame_id"].(string)
			if id == "" {
				return b, budgetProblem("image_identity_missing", "图片缺少可追溯身份，当前请求无法继续。")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			a, err := s.engine.Store.Asset(ctx, id)
			if err != nil {
				return b, err
			}
			pixels := float64(1920 * 1080)
			if a.Width > 0 && a.Height > 0 {
				pixels = float64(a.Width) * float64(a.Height)
			}
			b.EstimatedImageTokens += 512 + int(math.Ceil(pixels/1000000))*4096
			b.ImageCount++
		}
	}
	if s.authoring() && b.Phase == "compose" && !s.boundedSourceContext() {
		// A dense/long source may need more than the normal 32K writer budget.
		// Estimate from original text, never from accumulated repair history.
		factor := 2
		if s.spec.DetailLevel == "detailed" {
			factor = 3
		}
		if s.spec.DetailLevel == "concise" {
			factor = 1
		}
		target := estimateTextTokens(domain.JSON(s.transcript))*factor + 4000
		capacity := min(s.profile.MaxOutputTokens, 65536)
		if b.ContextTokens > 0 {
			capacity = min(capacity, b.ContextTokens-b.EstimatedTextTokens-b.EstimatedImageTokens-b.SafetyTokens)
		}
		if target > b.OutputTokens && capacity > b.OutputTokens {
			b.OutputTokens = min(target, capacity)
		}
	}
	if s.reviewing {
		b.Phase = "evidence_review"
		if s.reviewStep != nil {
			b = s.reviewStep.allocate(b, s.profile.MaxOutputTokens)
		} else if s.cp.HarnessRevision >= 16 {
			// A short verdict can still require substantial provider reasoning.
			// Do not inherit the writer's final-submit/repair reservation.
			b.OutputTokens = min(s.profile.MaxOutputTokens, 32768)
		} else {
			b.OutputTokens = min(b.OutputTokens, 4096)
		}
	}
	if !s.reviewing && s.sourceSurveyPending() {
		b.Phase = "source_survey"
		b.OutputTokens = min(s.profile.MaxOutputTokens, 8192)
	}
	return b, b.check(s.profile)
}
func (b callBudget) check(p config.Profile) error {
	if b.OutputTokens < 1 {
		return budgetProblem("output_reservation_invalid", "当前模型修订没有可用的输出预算，请新建解读以使用当前模型配置。")
	}
	if b.InputChars > separateInputLimit(p) {
		return budgetProblem("input_chars_exceeded", "完整来源超过当前模型修订的输入额度；请新建解读以使用当前模型配置。不会截断视频来源继续生成。")
	}
	if p.ContextTokens > 0 && b.EstimatedTextTokens+b.EstimatedImageTokens+b.OutputTokens+b.SafetyTokens > p.ContextTokens {
		return budgetProblem("context_tokens_exceeded", "完整来源、图片与本阶段输出预留超过当前模型修订的上下文额度；请新建解读以使用当前模型配置。")
	}
	return nil
}

func budgetProblem(reason, message string) error {
	return &domain.Problem{Status: 422, Code: "RESOURCE_LIMIT_EXCEEDED", Detail: message,
		Meta: map[string]any{"budget_reason": reason, "can_retry": false, "retry_action": "new_request"}}
}
