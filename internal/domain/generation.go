package domain

import "strings"

const PurposeCharsMax = 500

type GenerationModel struct {
	ID              string `json:"id"`
	RevisionID      string `json:"revision_id"`
	DisplayName     string `json:"display_name"`
	BrandID         string `json:"brand_id"`
	Description     string `json:"description,omitempty"`
	SelectionLocked bool   `json:"selection_locked"`
	Legacy          bool   `json:"legacy,omitempty"`
}
type GenerationSpec struct {
	Version                int             `json:"version"`
	Purpose                string          `json:"purpose"`
	Scope                  string          `json:"scope"`
	ConfigID               string          `json:"config_id"`
	Model                  GenerationModel `json:"model"`
	DetailLevel            string          `json:"detail_level,omitempty"`
	DetailPolicyRevision   string          `json:"detail_policy_revision,omitempty"`
	DetailPolicy           *DetailPolicy   `json:"detail_policy,omitempty"`
	SourcePipelineRevision string          `json:"source_pipeline_revision,omitempty"`
}
type RequestInput struct {
	VideoURL        string  `json:"video_url"`
	Purpose         string  `json:"purpose"`
	ModelID         string  `json:"model_id"`
	ModelRevisionID string  `json:"model_revision_id"`
	DetailLevel     *string `json:"detail_level,omitempty"`
}

// DetailPolicy is part of the immutable specification stored in PostgreSQL.
// A retry never resolves this policy against a newer deployment's defaults.
type DetailPolicy struct {
	ContentPlan               bool   `json:"content_plan,omitempty"`
	Revision                  string `json:"revision"`
	Instruction               string `json:"instruction"`
	OptionalInspections       int    `json:"optional_inspections"`
	NoProgressLimit           int    `json:"no_progress_limit"`
	RepairLimit               int    `json:"repair_limit"`
	FinalCallsReserved        int    `json:"final_calls_reserved"`
	InvestigationOutputTokens int    `json:"investigation_output_tokens"`
	CompositionOutputTokens   int    `json:"composition_output_tokens"`
	RepairOutputTokens        int    `json:"repair_output_tokens"`
	FinalOutputTokens         int    `json:"final_output_tokens"`
}

func InterpretationPolicy(level string) (DetailPolicy, bool) {
	p := DetailPolicy{Revision: "interpretation-2026-10-17-v7", ContentPlan: true, NoProgressLimit: 6, RepairLimit: 3, FinalCallsReserved: 4, InvestigationOutputTokens: 8192, CompositionOutputTokens: 32768, RepairOutputTokens: 16384, FinalOutputTokens: 2048}
	switch level {
	case "concise":
		p.OptionalInspections = 2
		p.Instruction = "精简：覆盖全片所有主要主题，每个主题只保留核心结论、必要前提、不可省略的步骤与关键数字，通常用一个短段或一组紧凑清单表达。次要参数、补充例子和延展解释可合并或省略，不能省略教程的必要步骤与风险条件。正文不要重复图注、重复列举同一流程或添加百科背景。只选解释主线所需的关键真实图，不要求每张概览都发表。目标是让读者快速掌握主线，不是完整复述所有字幕。"
	case "standard":
		p.OptionalInspections = 4
		p.Instruction = "标准：覆盖全片主要内容，每个主题先给结论，再解释关键概念、原因、主要步骤或代表性例子。保留重要数字、条件和限制，配图用于解释对应内容。避免逐句复述字幕、重复图注、同义段落或空泛背景。"
	case "detailed":
		p.OptionalInspections = 8
		p.Instruction = "详细：面向系统学习。覆盖全片主要内容，并保留片中有实质信息的次要参数、条件、不同例子、反例、边界与排障线索。展开说明步骤间关系、参数含义、适用条件、为什么这样做以及常见误区，不能只是把标准文章改成参数清单。来源简略时可加少量有助理解的解释，必须独立标为background_explanation，不能冒充原视频结论、实测或未展示的代码。只为明确视觉缺口追加调查；不重复段落或图注，不用无关背景与配图凑量。信息稀疏时保持克制，不凭空增加事实。"
	default:
		return DetailPolicy{}, false
	}
	return p, true
}

func DetailLabel(level string) string {
	switch level {
	case "concise":
		return "精简"
	case "standard":
		return "标准"
	case "detailed":
		return "详细"
	default:
		return "原版解读"
	}
}

func NormalizePurpose(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
}
