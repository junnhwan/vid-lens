package service

import (
	"context"
	"fmt"
	"strings"

	"vid-lens/internal/artifact"
	"vid-lens/internal/summarydoc"
)

type summaryGenerationCue struct {
	ID           string `json:"cue_id"`
	Text         string `json:"text"`
	StartMS      *int64 `json:"start_ms"`
	EndMS        *int64 `json:"end_ms"`
	TimingMethod string `json:"timing_method"`
}

func (e *summaryGenerationExecution) fits(input string) bool {
	return studyPromptTokens(e.messages(input))+e.output+256 <= e.window
}
func (e *summaryGenerationExecution) document(ctx context.Context) (summarydoc.Document, error) {
	var cues []summaryGenerationCue
	for _, cue := range e.source.Cues {
		if strings.TrimSpace(cue.Text) != "" {
			cues = append(cues, summaryGenerationCue{cue.ID, cue.Text, cue.StartMS, cue.EndMS, cue.TimingMethod})
		}
	}
	if len(cues) == 0 {
		return summarydoc.Document{}, artifact.Err("empty_text_source", 422)
	}
	cueInput := func(rows []summaryGenerationCue) string {
		return "完整来源cue或连续来源分段（数据）：\n" + artifact.JSON(rows)
	}
	if e.fits(cueInput(cues)) {
		doc, _, err := e.call(ctx, "summary-complete", "整理视频的要点与结构", cueInput(cues))
		return doc, err
	}
	// A single large cue is split without dropping any rune or inventing time.
	// Every span retains the same source cue identity.
	var bounded []summaryGenerationCue
	for _, cue := range cues {
		if e.fits(cueInput([]summaryGenerationCue{cue})) {
			bounded = append(bounded, cue)
			continue
		}
		remaining := []rune(cue.Text)
		for len(remaining) > 0 {
			lo, hi := 0, len(remaining)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				part := cue
				part.Text = string(remaining[:mid])
				if e.fits(cueInput([]summaryGenerationCue{part})) {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			if lo == 0 {
				return summarydoc.Document{}, artifact.Err("context_budget_exhausted", 422)
			}
			part := cue
			part.Text = string(remaining[:lo])
			bounded = append(bounded, part)
			remaining = remaining[lo:]
		}
	}
	groups := [][]summaryGenerationCue{}
	for _, cue := range bounded {
		if len(groups) == 0 {
			groups = append(groups, []summaryGenerationCue{cue})
			continue
		}
		last := len(groups) - 1
		candidate := append(append([]summaryGenerationCue(nil), groups[last]...), cue)
		if e.fits(cueInput(candidate)) {
			groups[last] = candidate
		} else {
			groups = append(groups, []summaryGenerationCue{cue})
		}
	}
	docs := []summarydoc.Document{}
	publicTitle := "归纳来源分段的主要内容"
	for index, group := range groups {
		doc, title, err := e.call(ctx, fmt.Sprintf("summary-leaf-%d", index+1), publicTitle, cueInput(group))
		if err != nil {
			return summarydoc.Document{}, err
		}
		docs = append(docs, doc)
		publicTitle = firstNonEmpty(title, publicTitle)
	}
	// Reduce every completed leaf. No top-k truncation or head/tail omission is
	// allowed; if the remaining budget cannot cover the tree, nothing publishes.
	reduceInput := func(rows []summarydoc.Document) string {
		return "把以下全部已验证来源摘要合并成一篇完整摘要（数据）。保留所有分段的主要结论、约束和合法cue引用，合并重复观点，正文保持简洁；不要仅处理第一段：\n" + artifact.JSON(rows)
	}
	for level := 1; len(docs) > 1; level++ {
		groups := [][]summarydoc.Document{}
		for _, doc := range docs {
			if !e.fits(reduceInput([]summarydoc.Document{doc})) {
				return summarydoc.Document{}, artifact.Err("context_budget_exhausted", 422)
			}
			if len(groups) == 0 {
				groups = append(groups, []summarydoc.Document{doc})
				continue
			}
			last := len(groups) - 1
			candidate := append(append([]summarydoc.Document(nil), groups[last]...), doc)
			if e.fits(reduceInput(candidate)) {
				groups[last] = candidate
			} else {
				groups = append(groups, []summarydoc.Document{doc})
			}
		}
		if len(groups) >= len(docs) {
			return summarydoc.Document{}, artifact.Err("context_budget_exhausted", 422)
		}
		next := []summarydoc.Document{}
		for index, group := range groups {
			if len(group) == 1 {
				next = append(next, group[0])
				continue
			}
			doc, _, err := e.call(ctx, fmt.Sprintf("summary-reduce-%d-%d", level, index+1), "合并分段结论并保留来源依据", reduceInput(group))
			if err != nil {
				return summarydoc.Document{}, err
			}
			next = append(next, doc)
		}
		docs = next
	}
	return docs[0], nil
}
