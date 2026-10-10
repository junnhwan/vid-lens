package summaryselection

import (
	"encoding/json"
	"fmt"
	"strings"
	"vid-lens/internal/summarydoc"
)

func ValidateScope(base summarydoc.Document, ids []string, patch summarydoc.Patch) error {
	if len(ids) == 0 {
		return nil
	}
	allowed := map[string]bool{}
	figures := map[string]string{}
	for _, id := range ids {
		allowed[id] = true
	}
	for _, b := range base.Blocks {
		for _, f := range b.Figures {
			figures[f.ID] = b.ID
		}
	}
	for _, op := range patch.Operations {
		id := op.BlockID
		switch op.Op {
		case summarydoc.OpUpdateDocumentTitle:
			id = "summary-title"
		case summarydoc.OpUpdateOverview:
			id = "summary-overview"
		case summarydoc.OpUpdateBlock:
		case summarydoc.OpUpdateFigureCaption:
			id = figures[op.FigureID]
		default:
			return fmt.Errorf("selected block editing requires existing text or caption updates")
		}
		if !allowed[id] {
			return fmt.Errorf("patch escapes selected blocks")
		}
	}
	return nil
}

type ByteRange struct{ Start, End int }

func LegacyRanges(base string, ids []string) ([]ByteRange, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var ranges []ByteRange
	parts := strings.Split(base, "\n\n")
	for _, id := range ids {
		found := false
		offset := 0
		if id == "summary-overview" {
			ranges = append(ranges, ByteRange{0, len(base)})
			continue
		}
		for i, part := range parts {
			if id == fmt.Sprintf("legacy-%d", i) || (id == "summary-title" && i == 0) {
				ranges = append(ranges, ByteRange{offset, offset + len(part)})
				found = true
				break
			}
			offset += len(part) + 2
		}
		if !found {
			return nil, fmt.Errorf("unknown legacy block")
		}
	}
	return ranges, nil
}
func ValidateLegacyScope(base string, ids []string, raw string) error {
	if len(ids) == 0 {
		return nil
	}
	ranges, err := LegacyRanges(base, ids)
	if err != nil {
		return err
	}
	var patch struct {
		Edits []struct {
			OldText string `json:"old_text"`
			Start   *int   `json:"start,omitempty"`
		} `json:"edits"`
	}
	if err = json.Unmarshal([]byte(raw), &patch); err != nil {
		return err
	}
	for _, edit := range patch.Edits {
		start := strings.Index(base, edit.OldText)
		if edit.Start != nil {
			start = *edit.Start
		} else if strings.Count(base, edit.OldText) != 1 {
			return fmt.Errorf("ambiguous selected edit")
		}
		if start < 0 || edit.OldText == "" || start+len(edit.OldText) > len(base) || base[start:start+len(edit.OldText)] != edit.OldText {
			return fmt.Errorf("invalid selected edit")
		}
		found := false
		for _, region := range ranges {
			if start >= region.Start && start+len(edit.OldText) <= region.End {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("patch escapes selected blocks")
		}
	}
	return nil
}
