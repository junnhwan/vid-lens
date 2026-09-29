package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"vid-lens/internal/artifact"
)

type summaryEditAnchor struct {
	ID    string `json:"anchor_id"`
	Text  string `json:"text"`
	Start int    `json:"-"`
}

type summaryAnchoredPatch struct {
	Edits []struct {
		AnchorID string `json:"anchor_id"`
		NewText  string `json:"new_text"`
	} `json:"edits"`
}

// Stable IDs identify exact editable source spans, including duplicate lines.
// Quotes and code are available as context but never receive editable IDs.
func summaryEditAnchors(base string) []summaryEditAnchor {
	protected := protectedSummarySpans(base)
	sort.Slice(protected, func(i, j int) bool { return protected[i].start < protected[j].start })
	var anchors []summaryEditAnchor
	appendRegion := func(start, end int) {
		offset := start
		for _, line := range strings.SplitAfter(base[start:end], "\n") {
			if strings.TrimSpace(line) != "" {
				anchors = append(anchors, summaryEditAnchor{ID: fmt.Sprintf("s%d", len(anchors)+1), Text: line, Start: offset})
			}
			offset += len(line)
		}
	}
	last := 0
	for _, span := range protected {
		if span.start > last {
			appendRegion(last, span.start)
		}
		if span.end > last {
			last = span.end
		}
	}
	if last < len(base) {
		appendRegion(last, len(base))
	}
	return anchors
}

func decodeAnchoredSummaryPatch(raw string, anchors []summaryEditAnchor, base string) (SummaryTextPatch, error) {
	var proposal summaryAnchoredPatch
	if err := decodeSummaryJSON(raw, &proposal); err != nil {
		return SummaryTextPatch{}, err
	}
	patch := SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: make([]SummaryTextEdit, 0, len(proposal.Edits))}
	if len(proposal.Edits) > 20 {
		return patch, artifact.Err("invalid_patch", 422)
	}
	byID := make(map[string]summaryEditAnchor, len(anchors))
	for _, anchor := range anchors {
		byID[anchor.ID] = anchor
	}
	seen := make(map[string]bool, len(proposal.Edits))
	for _, edit := range proposal.Edits {
		anchor, ok := byID[edit.AnchorID]
		if !ok || seen[edit.AnchorID] {
			return patch, artifact.Err("invalid_patch", 422)
		}
		seen[edit.AnchorID] = true
		start := anchor.Start
		patch.Edits = append(patch.Edits, SummaryTextEdit{OldText: anchor.Text, NewText: edit.NewText, Start: &start})
	}
	return patch, nil
}

// Recognize only a complete, single ASCII name-correction instruction. Scoped
// edits and broader prose instructions keep the general editing semantics.
var summaryLiteralTermInstruction = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9]*)\s*(?:是|应为|应是|改成|改为|更正为|纠正为)\s*([A-Za-z][A-Za-z0-9]*)\s*[。.!]?\s*$`)

func summaryLiteralTermTarget(base, instruction string) (target, from, to string, ok bool) {
	match := summaryLiteralTermInstruction.FindStringSubmatch(instruction)
	if len(match) == 0 {
		return "", "", "", false
	}
	from, to = match[1], match[2]
	term := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(from) + `\b`)
	anchors := summaryEditAnchors(base)
	var out strings.Builder
	last := 0
	for _, anchor := range anchors {
		out.WriteString(base[last:anchor.Start])
		out.WriteString(term.ReplaceAllString(anchor.Text, to))
		last = anchor.Start + len(anchor.Text)
	}
	out.WriteString(base[last:])
	return out.String(), from, to, true
}

func literalSummaryTermPatch(base, from, to string) SummaryTextPatch {
	patch := SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{}}
	term := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(from) + `\b`)
	for _, anchor := range summaryEditAnchors(base) {
		next := term.ReplaceAllString(anchor.Text, to)
		if next != anchor.Text {
			start := anchor.Start
			patch.Edits = append(patch.Edits, SummaryTextEdit{OldText: anchor.Text, NewText: next, Start: &start})
		}
	}
	return patch
}
