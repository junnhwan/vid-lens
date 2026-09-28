package service

import (
	"sort"
	"strings"
	"unicode/utf8"

	"vid-lens/internal/artifact"
)

type SummaryTextEdit struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type SummaryTextPatch struct {
	BaseHash string            `json:"base_hash"`
	Edits    []SummaryTextEdit `json:"edits"`
}

type summarySpan struct {
	start, end  int
	replacement string
}

// applySummaryTextPatch applies exact, unique anchors against one frozen
// document. It never performs a global replacement or edits transcript text.
func applySummaryTextPatch(base string, patch SummaryTextPatch) (string, error) {
	if patch.BaseHash != artifact.Hash(base) || len(patch.Edits) < 1 || len(patch.Edits) > 20 {
		return "", artifact.Err("invalid_patch", 422)
	}
	spans := make([]summarySpan, 0, len(patch.Edits))
	protected := protectedSummarySpans(base)
	for _, edit := range patch.Edits {
		if edit.OldText == "" || edit.OldText == edit.NewText || utf8.RuneCountInString(edit.OldText) > 48000 || utf8.RuneCountInString(edit.NewText) > 48000 {
			return "", artifact.Err("invalid_patch", 422)
		}
		index := strings.Index(base, edit.OldText)
		if index < 0 || strings.Count(base, edit.OldText) != 1 {
			return "", artifact.Err("anchor_ambiguous", 409)
		}
		for _, region := range protected {
			if index < region.end && index+len(edit.OldText) > region.start {
				return "", artifact.Err("protected_quote", 422)
			}
		}
		spans = append(spans, summarySpan{index, index + len(edit.OldText), edit.NewText})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var out strings.Builder
	last := 0
	for _, span := range spans {
		if span.start < last {
			return "", artifact.Err("invalid_patch", 422)
		}
		out.WriteString(base[last:span.start])
		out.WriteString(span.replacement)
		last = span.end
	}
	out.WriteString(base[last:])
	result := out.String()
	if strings.TrimSpace(result) == "" || utf8.RuneCountInString(result) > 48000 {
		return "", artifact.Err("invalid_patch", 422)
	}
	return result, nil
}

func protectedSummarySpans(base string) []summarySpan {
	var protected []summarySpan
	offset, fenced := 0, false
	for _, line := range strings.SplitAfter(base, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			protected = append(protected, summarySpan{start: offset, end: offset + len(line)})
			fenced = !fenced
		} else if fenced || strings.HasPrefix(trimmed, ">") {
			protected = append(protected, summarySpan{start: offset, end: offset + len(line)})
		}
		offset += len(line)
	}
	for _, pair := range [][2]string{{"“", "”"}, {"「", "」"}, {"`", "`"}} {
		from := 0
		for from < len(base) {
			start := strings.Index(base[from:], pair[0])
			if start < 0 {
				break
			}
			start += from
			end := strings.Index(base[start+len(pair[0]):], pair[1])
			if end < 0 {
				break
			}
			end += start + len(pair[0]) + len(pair[1])
			protected = append(protected, summarySpan{start: start, end: end})
			from = end
		}
	}
	return protected
}

func reverseSummaryTextPatch(current, result, original string, patch SummaryTextPatch) (string, error) {
	if current == result {
		return original, nil
	}
	// Later edits survive only when every replacement is still uniquely present.
	for _, edit := range patch.Edits {
		if edit.NewText == "" || strings.Count(current, edit.NewText) != 1 {
			return "", artifact.Err("undo_conflict", 409)
		}
		current = strings.Replace(current, edit.NewText, edit.OldText, 1)
	}
	return current, nil
}
