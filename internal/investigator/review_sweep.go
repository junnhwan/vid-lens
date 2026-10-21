package investigator

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	"vidlens/internal/domain"
)

const reviewCitationBindingInstructions = `evidence_bindings区分每个单元实际绑定的引用与仅作上下文的原文。必须核对每个具体断言紧邻的引用；列表中另一项的引用、邻居或别章含有正确事实，都不自动支持当前绑定的引用。邻居可补全跨时间段的句子，不能把同主题的其他段落都当成引用范围。事实成立但引用错位时，报告可执行的引用修正并给出实际已提供的正确原文编号；无须因此删除有依据的内容。`

func (s *session) reviewOutputGrowth(ctx context.Context, slot int) (next, previous int, err error) {
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "input:model:"+strconv.Itoa(slot))
	if err != nil {
		return 0, 0, err
	}
	var frozen frozenModelInput
	if err = json.Unmarshal(op.Result, &frozen); err != nil {
		return 0, 0, err
	}
	previous = frozen.Budget.OutputTokens
	return min(frozen.Budget.AvailableOutputTokens, previous*2), previous, nil
}

// Shared original text appears once. These per-unit roles keep a surrounding
// passage from silently replacing the source actually attached to a claim.
func reviewEvidenceBindings(task reviewTask, catalog map[string]domain.Citation, source map[string]any) map[string]any {
	var originals []domain.Segment
	_ = json.Unmarshal(domain.JSON(source["transcript"]), &originals)
	delivered := map[string]bool{}
	for _, segment := range originals {
		delivered[segment.ID] = true
	}
	textAliases := []string{}
	for alias, citation := range catalog {
		if citation.Kind == "video_text" && slices.ContainsFunc(citation.SegmentIDs, func(id string) bool { return delivered[id] }) {
			textAliases = append(textAliases, alias)
		}
	}
	slices.Sort(textAliases)
	out := map[string]any{}
	for _, unit := range task.Units {
		cited, contextual := []string{}, []string{}
		for _, alias := range textAliases {
			if slices.Contains(unit.CitationAliases, alias) {
				cited = append(cited, alias)
			} else {
				contextual = append(contextual, alias)
			}
		}
		out[unit.ID] = map[string]any{"cited_citation_aliases": unit.CitationAliases, "cited_text_aliases": cited, "context_only_text_aliases": contextual, "required_frame_ids": unit.FrameIDs}
	}
	return out
}
