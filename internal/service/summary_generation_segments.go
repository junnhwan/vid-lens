package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

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
	return e.contextFits(e.messages(input), e.plannedOutput(input))
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
	reviewRequired := 0
	for _, cue := range cues {
		reviewRequired += utf8.RuneCountInString(cue.Text)
	}
	needsReview := reviewRequired >= 1500
	sourceFits := func(input string) bool {
		if !e.fits(input) {
			return false
		}
		if !needsReview {
			return true
		}
		// Leave room for the semantic draft as well as the second response. The
		// actual review input is checked again before any provider call.
		return studyPromptTokens(e.messages(summaryGroundingReviewPrefix+input))+2*e.plannedOutput(input)+64+256 <= e.window
	}
	cueInput := func(rows []summaryGenerationCue) string {
		return summaryGenerationSemanticCueInput(rows)
	}
	if sourceFits(cueInput(cues)) {
		doc, _, err := e.call(ctx, "summary-complete", "整理视频的要点与结构", cueInput(cues))
		if err != nil || !needsReview {
			return doc, err
		}
		return e.reviewDocument(ctx, "summary-grounding-review", doc, cues)
	}
	// A single large cue is split without dropping any rune or inventing time.
	// Every span retains the same source cue identity.
	var bounded []summaryGenerationCue
	for _, cue := range cues {
		if sourceFits(cueInput([]summaryGenerationCue{cue})) {
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
				if sourceFits(cueInput([]summaryGenerationCue{part})) {
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
		if sourceFits(cueInput(candidate)) {
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
		if needsReview {
			doc, err = e.reviewDocument(ctx, fmt.Sprintf("summary-grounding-review-leaf-%d", index+1), doc, group)
			if err != nil {
				return summarydoc.Document{}, err
			}
		}
		docs = append(docs, doc)
		publicTitle = firstNonEmpty(title, publicTitle)
	}
	// Reduce every completed leaf. No top-k truncation or head/tail omission is
	// allowed; if the remaining budget cannot cover the tree, nothing publishes.
	reduceInput := summaryGenerationVerifiedPartsInput
	reduceFits := func(input string) bool {
		if !needsReview {
			return e.fits(input)
		}
		return studyPromptTokens(e.messages(summaryReductionReviewPrefix+input))+2*e.plannedOutput(input)+64+256 <= e.window
	}
	for level := 1; len(docs) > 1; level++ {
		groups := [][]summarydoc.Document{}
		for _, doc := range docs {
			if !reduceFits(reduceInput([]summarydoc.Document{doc})) {
				return summarydoc.Document{}, artifact.Err("context_budget_exhausted", 422)
			}
			if len(groups) == 0 {
				groups = append(groups, []summarydoc.Document{doc})
				continue
			}
			last := len(groups) - 1
			candidate := append(append([]summarydoc.Document(nil), groups[last]...), doc)
			if reduceFits(reduceInput(candidate)) {
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
			if needsReview {
				doc, err = e.reviewVerifiedMerge(ctx, fmt.Sprintf("summary-reduce-review-%d-%d", level, index+1), doc, group)
				if err != nil {
					return summarydoc.Document{}, err
				}
			}
			next = append(next, doc)
		}
		docs = next
	}
	return docs[0], nil
}
