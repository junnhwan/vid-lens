package investigator

import (
	"context"
	"encoding/json"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// Project the new contract without changing schemas frozen in existing runs.
func (t *argumentCheckedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.EnhancedInvokableTool.Info(ctx)
	if err != nil || t.session == nil || t.session.cp.HarnessRevision < 7 || info.ParamsOneOf == nil {
		return info, err
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err = json.Unmarshal(mustSchemaJSON(sc), &root); err != nil {
		return nil, err
	}
	projectBodySchema(root)
	if t.session.authoring() {
		authoringBodySchema(root)
		if t.name == "inspect_video_window" {
			props := root["properties"].(map[string]any)
			window := props["window"].(map[string]any)
			window["description"] = "New pixels: 0 < end_ms-start_ms <= 60000, within source_duration_ms. count 1-6. Use one concrete question. For longer gaps inspect a smaller relevant window, then record observations before another inspection."
			fields := window["properties"].(map[string]any)
			fields["count"].(map[string]any)["minimum"] = 1
			fields["count"].(map[string]any)["maximum"] = 6
			fields["mode"].(map[string]any)["enum"] = []string{"overview", "detail", "sequence"}
		}
		if t.name == "begin_article" {
			if p, ok := root["properties"].(map[string]any); ok {
				sections := p["sections"].(map[string]any)["items"].(map[string]any)
				fields := sections["properties"].(map[string]any)
				delete(fields, "start_ms")
				delete(fields, "end_ms")
				sections["required"] = []string{"id", "title", "key_points", "evidence_ids"}
			}
		}
		if t.name == "save_article_sections" {
			root["properties"].(map[string]any)["sections"].(map[string]any)["items"].(map[string]any)["required"] = []string{"id", "blocks"}
		}
	}
	if props, ok := root["properties"].(map[string]any); ok {
		props["activity_title"] = map[string]any{"type": "string", "maxLength": 60, "description": "Optional short public action title. Never include private reasoning, credentials or unverified conclusions."}
		if t.name == "repair_article_plan" {
			delete(props, "replacement_json")
		}
		if t.name == "save_article_sections" {
			props["sections"].(map[string]any)["maxItems"] = 12
			props["sections"].(map[string]any)["minItems"] = 1
			props["sections"].(map[string]any)["description"] = "Write 1-12 pending chapters in ONE call, sized to the output budget; at most 128KiB. Valid chapters are saved even when another chapter fails. Follow accepted/rejected IDs and latest version."
			if t.session.authoring() {
				props["sections"].(map[string]any)["maxItems"] = maxPlanSections
				props["sections"].(map[string]any)["description"] = "Write ALL pending chapters in ONE complete call when they fit output_tokens and 128KiB. Up to 40 chapters; split only for actual output size. Preserve required facts and explanation. Accepted chapters remain durable."
			}
		}
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	var next jsonschema.Schema
	if err = json.Unmarshal(raw, &next); err != nil {
		return nil, err
	}
	out := *info
	out.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(&next)
	if t.name == "repair_article_plan" && t.session.cp.HarnessRevision >= 9 {
		out.Desc += " Concept-tree repairs may also replace /mind_map or its existing root_id/nodes fields. Use one root with parent_id=null; group section_id=null, leaf section_id must be a real planned chapter ID. Read only fields not already present in context, then submit patches."
	}
	if t.session.turn != nil && t.session.cp.HarnessRevision >= 11 && t.name == "inspect_video_window" {
		out.Desc += " For this Q&A turn, inspect only a concrete pixel-dependent gap needed to answer the user's actual question. Use the supplied full transcript/article directly for conceptual explanations; do not fetch decorative figures or resolve unrelated article limitations."
	}
	if t.name == "save_article_sections" {
		out.Desc = "Save 1-12 planned chapters in one bounded call (128KiB maximum). Choose a batch that fits the output budget. Valid chapters are retained independently. When all chapters are saved the server validates and publishes automatically; no model-only final submit is needed."
		if t.session.authoring() {
			out.Desc = "Write all pending chapters in one complete call when the output budget allows (1-40 chapters, 128KiB). Preserve necessary detail and figures. Valid chapters are saved independently. Server validates and publishes the complete article automatically."
		}
	}
	return &out, nil
}

func mustSchemaJSON(sc *jsonschema.Schema) []byte { raw, _ := json.Marshal(sc); return raw }

func projectBodySchema(node map[string]any) {
	if props, ok := node["properties"].(map[string]any); ok {
		if typ, ok := props["type"].(map[string]any); ok {
			if _, block := props["origin"]; block {
				typ["enum"] = []string{"paragraph", "quote", "list", "code", "table", "figure"}
			} else if _, inline := props["citation_id"]; inline {
				typ["enum"] = []string{"text", "citation", "link"}
			}
		}
	}
	for _, v := range node {
		switch child := v.(type) {
		case map[string]any:
			projectBodySchema(child)
		case []any:
			for _, item := range child {
				if m, ok := item.(map[string]any); ok {
					projectBodySchema(m)
				}
			}
		}
	}
}

const efficientDraftInstructions = `
章节应对应独立主题；同一概念的定义、原因、步骤与例子优先放在同一章，不按字幕片段、每个小问题或每分钟各分一章。章内用短段和清单分层，避免产生大量很短的章节。
本次采用弹性批次协议：save_article_sections每次可保存1–12章、总JSON最多128KiB。根据剩余章节、正文信息量和当前output_tokens选择能完整返回的一批，通常优先一次写完剩余章节或按主题合并成少量批次；不要为了凑批次删必要内容。正文块type只允许paragraph、quote、list、code、table、figure，不能使用note或heading。章节内部的小标题可用paragraph的text inline加bold标记。计划和正文工具可带可选activity_title短标题。
每章独立校验；accepted_sections已可靠保存，rejected_sections列出需要纠正的章节。只重写被拒章节及其他pending_sections，复制最新version，禁止重复已接受正文。每个草稿版本的问题有独立的有界纠错额度，计划错误不会耗尽后续章节纠错。模型调用数、时限与无进展限制仍然全程累计。
全部计划章节保存后，服务器自动执行完整内容、引用和配图校验并发布，不需要额外调用模型只返回version。若完整校验反馈问题，只修正相关字段。调查预算用尽时明确列出未证实细节并及时收尾。`

const structuredAnswerInstructions = `
问答排版：先用一个短段直接回答，再按问题复杂度展开。多项理由、步骤或建议优先用list；确有对比时用table；关键词可在text inline的marks使用bold。每个paragraph只表达一个主要意思，避免把多个理由挤成长段，不要全篇加粗。简单问题不必机械列点。summary只写一句简短结论，正文不重复summary；不要把summary当作执行过程标题。返回结构化blocks，不能把Markdown列表、标题或字面量转义符塞进普通text。引用靠近所支持的结论。`
