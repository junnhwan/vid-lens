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
	Revision              string `json:"revision"`
	Phase                 string `json:"phase"`
	InputChars            int    `json:"input_chars"`
	InputCharsLimit       int    `json:"input_chars_limit"`
	EstimatedTextTokens   int    `json:"estimated_text_tokens"`
	EstimatedImageTokens  int    `json:"estimated_image_tokens"`
	ImageCount            int    `json:"image_count"`
	OutputTokens          int    `json:"output_tokens"`
	SafetyTokens          int    `json:"safety_tokens"`
	ContextTokens         int    `json:"context_tokens,omitempty"`
	ContextCapacityStatus string `json:"context_capacity_status"`
	Estimator             string `json:"estimator"`
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
		if s.turn != nil {
			b.Phase = "answer"
			b.OutputTokens = min(s.profile.MaxOutputTokens, p.CompositionOutputTokens)
		}
		if s.cp.DraftCorrection || s.cp.Repairs > 0 {
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
				return b, domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "图片缺少可追溯身份。")
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
	return b, b.check(s.profile)
}
func (b callBudget) check(p config.Profile) error {
	if b.OutputTokens < 1 || b.InputChars > separateInputLimit(p) || p.ContextTokens > 0 && b.EstimatedTextTokens+b.EstimatedImageTokens+b.OutputTokens+b.SafetyTokens > p.ContextTokens {
		return domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "完整来源、工具、图片估算与本阶段输出预留超过模型预算；已保留来源与章节，不会截断后半片。")
	}
	return nil
}
