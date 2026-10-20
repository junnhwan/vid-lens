package investigator

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

const sectionReadPageSize = 3

type articleSectionReadInput struct {
	SectionIDs []string `json:"section_ids" jsonschema:"description=1-40 actual saved section IDs. Copy affected_section_ids or valid_section_ids, never derive IDs from array offsets. Repeated IDs are read once. Keep this same list when following next_offset."`
	Offset     int      `json:"offset,omitempty" jsonschema:"description=Zero for the first page. Counts distinct requested IDs. Copy next_offset to read the next page of the same section_ids list."`
}

func (s *session) articleSectionReadTool(add func(tool.EnhancedInvokableTool, error) error) error {
	if s.usesBoundedReview() {
		return add(utils.InferEnhancedTool("read_article_sections", "Read saved chapters for a local correction. Accept 1-40 actual section IDs; the server returns at most 3 chapters per page. Follow next_offset with the same section_ids, or read remaining_section_ids. Missing IDs return valid saved IDs and never change the pending content repair.", func(ctx context.Context, in articleSectionReadInput) (*schema.ToolResult, error) {
			version, draft, err := s.loadDraft(ctx)
			if err != nil {
				return nil, err
			}
			return textResult(articleSectionPage(version, draft, in)), nil
		}))
	}
	// Frozen v1 runs retain both the old schema and tool behavior.
	return add(utils.InferEnhancedTool("read_article_sections", "Read 1-3 saved chapters for a local correction, without regenerating other chapters.", func(ctx context.Context, in struct {
		SectionIDs []string `json:"section_ids"`
	}) (*schema.ToolResult, error) {
		version, draft, err := s.loadDraft(ctx)
		if err != nil {
			return nil, err
		}
		if len(in.SectionIDs) == 0 || len(in.SectionIDs) > 3 {
			return s.draftFeedback(ctx, "一次读取1–3个已保存章节。")
		}
		sections := []domain.Section{}
		for _, id := range in.SectionIDs {
			if sec, ok := draft.Sections[id]; ok {
				sections = append(sections, sec)
			}
		}
		return textResult(map[string]any{"version": version, "sections": sections}), nil
	}))
}

// Checked-tool decoding uses the same corrective response as a bad page,
// including real IDs. Decode failures must not become content-repair failures.
func (s *session) articleSectionReadFeedback(ctx context.Context, message string) (*schema.ToolResult, error) {
	version, draft, err := s.loadDraft(ctx)
	if err != nil {
		return nil, err
	}
	body := articleSectionPage(version, draft, articleSectionReadInput{})
	body["error"].(map[string]any)["detail"] = "read_article_sections参数无效：" + message
	return textResult(body), nil
}

// Read argument errors belong to this tool invocation, not to the retained
// candidate. They must not consume author correction budget or erase the issue
// that the author was reading these chapters to resolve.
func articleSectionPage(version int, draft articleDraft, in articleSectionReadInput) map[string]any {
	valid, pending := []string{}, []string{}
	for _, p := range draft.Plan {
		if _, ok := draft.Sections[p.ID]; ok {
			valid = append(valid, p.ID)
		} else {
			pending = append(pending, p.ID)
		}
	}
	out := map[string]any{"version": version, "sections": []domain.Section{}, "valid_section_ids": valid, "pending_section_ids": pending}
	reject := func(code, detail string) map[string]any {
		out["error"] = map[string]any{"category": "tool_read", "code": code, "detail": detail, "repairable": true}
		out["correct_arguments_and_try_again"] = true
		out["next_action"] = "Correct section_ids using valid_section_ids and retry read_article_sections with offset=0. Copy actual IDs, never array offsets or invented IDs. pending_section_ids have no saved body to read; continue writing them with the latest version. The pending content review remains unchanged."
		return out
	}
	if len(in.SectionIDs) == 0 || len(in.SectionIDs) > maxPlanSections {
		return reject("invalid_section_read", "section_ids必须包含1–40个实际已保存章节ID；服务器每页最多返回3章。")
	}
	ids, missing := []string{}, []string{}
	seen := map[string]bool{}
	for _, id := range in.SectionIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if _, ok := draft.Sections[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		out["missing_section_ids"] = missing
		return reject("section_not_saved", "请求包含不存在或尚未保存正文的章节；本次未读取部分结果。")
	}
	if in.Offset < 0 || in.Offset >= len(ids) {
		return reject("invalid_section_read", fmt.Sprintf("offset必须位于0..%d；跟随上一页的next_offset。", len(ids)-1))
	}
	end := min(in.Offset+sectionReadPageSize, len(ids))
	sections := make([]domain.Section, 0, end-in.Offset)
	for _, id := range ids[in.Offset:end] {
		sections = append(sections, draft.Sections[id])
	}
	out["sections"], out["offset"], out["total_sections"] = sections, in.Offset, len(ids)
	out["remaining_section_ids"] = ids[end:]
	out["next_action"] = "Use the returned original chapter bodies to correct only the listed content issues, then save_article_sections with this latest version. Preserve other chapters."
	if end < len(ids) {
		out["next_offset"] = end
		out["next_action"] = "Continue read_article_sections with the same section_ids and next_offset as offset, or read remaining_section_ids with offset=0. Then correct the listed content issues using the original chapter bodies and latest version; preserve other chapters."
	}
	return out
}
