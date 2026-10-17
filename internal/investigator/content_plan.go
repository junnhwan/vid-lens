package investigator

import (
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
	return validateContentPlan(plan, catalog)
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
begin_article.sections每个主题必须有key_points（1–12条要保留的事实/条件/步骤）与evidence_ids（1–24个当前tN/fN）。evidence_ids只选最能支持要点的代表性来源，不要罗列该章节时间范围内的全部编号；若校验反馈列出章节和实际数量，逐章按反馈精简。先登记全片主要信息，再按详细程度分配解释；不同主题不要重复同一要点。章节来源范围可重叠，但联合覆盖全片且不能有缺口。草稿状态提供已保存正文的canonical_text_excerpt，写后续章节前对照避免重讲同一概念；摘录明确标记截短，必要时read_article_sections回读完整章节。精简只用关键要点组织短段，标准补原因与代表例子，详细补来源支持的非重复条件/参数/例子与有帮助的解释。不要把重复正文当详细。`
