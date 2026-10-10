package summarydoc

import (
	"regexp"
	"strings"
)

// GenerationContentError describes a repairable generation-only failure.
// Existing documents and human edits continue to use Validate alone.
type GenerationContentError struct{ Code string }

func (e *GenerationContentError) Error() string { return e.Code }

var internalCueMarker = regexp.MustCompile(`\[(?:asr-window-|cue-|subtitle-cue-)[^\]\s]*\]`)

// ValidateGeneratedContent adds publication requirements for new machine
// summaries. Empty grouping headings are allowed; substantive block bodies
// must carry authorized source refs. IDs belong in the structured refs, never
// as pseudo-citations in the text visible to readers.
func ValidateGeneratedContent(doc Document, ctx ValidationContext) error {
	if err := Validate(doc, ctx); err != nil {
		return err
	}
	checkMarkers := func(text string) error {
		if internalCueMarker.MatchString(text) {
			return &GenerationContentError{Code: "internal_cue_marker"}
		}
		for id := range ctx.Cues {
			if strings.Contains(text, "["+id+"]") {
				return &GenerationContentError{Code: "internal_cue_marker"}
			}
		}
		return nil
	}
	for _, text := range []string{doc.Title, doc.Overview} {
		if err := checkMarkers(text); err != nil {
			return err
		}
	}
	hasBody := false
	for _, block := range doc.Blocks {
		if strings.TrimSpace(block.BodyMarkdown) != "" {
			hasBody = true
		}
		if strings.TrimSpace(block.BodyMarkdown) != "" && len(block.SourceRefs) == 0 {
			return &GenerationContentError{Code: "body_source_refs_missing"}
		}
		for _, text := range []string{block.Title, block.BodyMarkdown} {
			if err := checkMarkers(text); err != nil {
				return err
			}
		}
		for _, figure := range block.Figures {
			for _, text := range []string{figure.Caption, figure.Alt, figure.Supports} {
				if err := checkMarkers(text); err != nil {
					return err
				}
			}
		}
	}
	if !hasBody {
		return &GenerationContentError{Code: "body_content_missing"}
	}
	return nil
}
