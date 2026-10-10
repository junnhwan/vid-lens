package service

import (
	"fmt"
	"regexp"
	"strings"

	"vid-lens/internal/summarydoc"
)

var summaryPercentClaim = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?\s*[%％]`)
var summaryEmbeddedSection = regexp.MustCompile(`(?m)^\s*(?:#{2,6}\s+[^\n]+|\*\*[^\n*]+(?:[：:]\*\*|\*\*\s*[：:]|\*\*\s*$))`)

// These are narrow, inspectable quality gates, not a claim that registration
// proves semantic entailment. Numerical claims require the chosen words;
// multiple named sections belong in the actual shared content hierarchy.
func (e *summaryGenerationExecution) validateContentQuality(doc summarydoc.Document) summaryGenerationCheckpoint {
	sourceText := map[string]string{}
	all := strings.Builder{}
	for _, cue := range e.source.Cues {
		sourceText[cue.ID] = cue.Text
		all.WriteString(cue.Text)
	}
	checkPercent := func(text, evidence, path string) summaryGenerationCheckpoint {
		evidence = strings.ReplaceAll(strings.ReplaceAll(evidence, "％", "%"), " ", "")
		known := map[string]bool{}
		for _, value := range summaryPercentClaim.FindAllString(evidence, -1) {
			known[value] = true
		}
		for _, claim := range summaryPercentClaim.FindAllString(text, -1) {
			claim = strings.ReplaceAll(strings.ReplaceAll(claim, "％", "%"), " ", "")
			if !known[claim] {
				return summaryGenerationCheckpoint{Invalid: true, ValidationCode: "unsupported_percentage", ValidationPath: path}
			}
		}
		return summaryGenerationCheckpoint{}
	}
	for _, row := range []struct{ text, path string }{{doc.Title, "document.title"}, {doc.Overview, "document.overview"}} {
		if invalid := checkPercent(row.text, all.String(), row.path); invalid.Invalid {
			return invalid
		}
	}
	children := map[string]bool{}
	for _, block := range doc.Blocks {
		if block.ParentID != nil {
			children[*block.ParentID] = true
		}
	}
	for i, block := range doc.Blocks {
		evidence := strings.Builder{}
		for _, ref := range block.SourceRefs {
			for _, id := range ref.CueIDs {
				evidence.WriteString(sourceText[id])
			}
		}
		if invalid := checkPercent(block.Title, evidence.String(), fmt.Sprintf("document.blocks[%d].title", i)); invalid.Invalid {
			return invalid
		}
		path := fmt.Sprintf("document.blocks[%d].body_markdown", i)
		if invalid := checkPercent(block.BodyMarkdown, evidence.String(), path); invalid.Invalid {
			return invalid
		}
		if e.snapshot.Intent.Options.MindmapEnabled && !children[block.ID] && len(summaryEmbeddedSection.FindAllString(block.BodyMarkdown, -1)) >= 2 {
			return summaryGenerationCheckpoint{Invalid: true, ValidationCode: "content_hierarchy_missing", ValidationPath: path}
		}
	}
	return summaryGenerationCheckpoint{}
}
