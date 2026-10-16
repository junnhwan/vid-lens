package investigator

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
	"vidlens/internal/domain"
)

func pointKey(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
}

func validatePlanFacts(plan []sectionPlan, catalog map[string]domain.Citation) error {
	if len(domain.JSON(plan)) > 64<<10 {
		return domain.Err(422, "MODEL_RESULT_INVALID", "主题计划超过64KiB，请只保留主要信息。")
	}
	seen := map[string]bool{}
	for i, p := range plan {
		fail := func(msg string) error {
			return &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: msg, Meta: map[string]any{"path": fmt.Sprintf("/sections/%d", i), "expected": "1-12 distinct key_points and 1-24 existing evidence_ids supporting this source range"}}
		}
		if len(p.KeyPoints) < 1 || len(p.KeyPoints) > 12 || len(p.EvidenceIDs) < 1 || len(p.EvidenceIDs) > 24 {
			return fail("每个主题须登记要保留的事实/条件及来源短引用，不能只给重复标题。")
		}
		for _, point := range p.KeyPoints {
			key := pointKey(point)
			if key == "" || utf8.RuneCountInString(point) > 300 || seen[key] {
				return fail("key_points需简洁、非空，且不同章节不能重复同一要点。")
			}
			seen[key] = true
		}
		for _, id := range p.EvidenceIDs {
			c, ok := catalog[id]
			if !ok {
				return fail("计划来源必须来自当前citation_catalog。")
			}
			intersects := c.Range != nil && c.Range.StartMS < p.EndMS && c.Range.EndMS > p.StartMS || c.TimeMS != nil && *c.TimeMS >= p.StartMS && *c.TimeMS <= p.EndMS
			if !intersects {
				return fail("计划引用的实际来源范围不支持这一章节时间范围。")
			}
		}
	}
	return nil
}

// Excerpts come directly from accepted canonical blocks. They are private
// writing context, not a recursive summary or a substitute for video text.
func draftPreviews(d articleDraft) []map[string]any {
	out := []map[string]any{}
	remaining := 12000
	for _, p := range d.Plan {
		sec, ok := d.Sections[p.ID]
		if !ok {
			continue
		}
		parts := []string{}
		for _, b := range sec.Blocks {
			parts = append(parts, domain.BlockText(b))
		}
		runes := []rune(strings.Join(parts, "\n"))
		n := min(len(runes), 600, max(0, remaining))
		remaining -= n
		out = append(out, map[string]any{"section_id": p.ID, "canonical_text_excerpt": string(runes[:n]), "excerpt_truncated": n < len(runes), "note": "Use read_article_sections for complete text. Do not repeat these accepted points in later chapters."})
	}
	return out
}

func validateDistinctProse(sections []domain.Section) error {
	seen := map[string]string{}
	for _, sec := range sections {
		for _, b := range sec.Blocks {
			if b.Type != "paragraph" && b.Type != "list" {
				continue
			}
			text := pointKey(domain.BlockText(b))
			if utf8.RuneCountInString(text) < 60 {
				continue
			}
			if previous := seen[text]; previous != "" {
				return &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "长正文与已保存内容完全重复，请保留一次并仅修正相关块。", Meta: map[string]any{"block_id": b.ID, "expected": "distinct content", "actual": previous}}
			}
			seen[text] = b.ID
		}
	}
	return nil
}

const contentPlanInstructions = `
begin_article.sections每个主题必须有key_points（1–12条要保留的事实/条件/步骤）与evidence_ids（1–24个当前tN/fN）。先登记全片主要信息，再按详细程度分配解释；不同主题不要重复同一要点。章节来源范围可重叠，但联合覆盖全片且不能有缺口。草稿状态提供已保存正文的canonical_text_excerpt，写后续章节前对照避免重讲同一概念；摘录明确标记截短，必要时read_article_sections回读完整章节。精简只用关键要点组织短段，标准补原因与代表例子，详细补来源支持的非重复条件/参数/例子与有帮助的解释。不要把重复正文当详细。`
