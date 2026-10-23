package investigator

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"vidlens/internal/domain"
)

// Keep complete chapters linked to this source range. Unsourced/background or
// pixel-only chapters remain in both halves, so partitioning cannot hide them.
func loopScopeChapters(scope domain.ArticleReviewScope, a domain.Article) domain.ArticleReviewScope {
	allowed := scope.SectionIDs
	scope.SectionIDs = nil
	knownFrames, selectedFrames := map[string]bool{}, map[string]bool{}
	for _, section := range a.Sections {
		if !slices.Contains(allowed, section.ID) {
			continue
		}
		originals := []string{}
		frames := []string{}
		collect := func(refs []string) {
			for _, ref := range refs {
				citation := a.Citations[ref]
				for _, id := range citation.SegmentIDs {
					originals = appendUnique(originals, id)
				}
				if citation.FrameID != "" {
					frames = appendUnique(frames, citation.FrameID)
				}
			}
		}
		for _, block := range section.Blocks {
			collect(blockCitationIDs(block))
			for _, id := range block.FrameIDs {
				frames = appendUnique(frames, id)
			}
		}
		for _, limitation := range a.Limitations {
			if slices.Contains(limitation.SectionIDs, section.ID) {
				collect(limitation.CitationIDs)
			}
		}
		for _, id := range frames {
			knownFrames[id] = true
		}
		if len(originals) == 0 || slices.ContainsFunc(originals, func(id string) bool { return slices.Contains(scope.SourceIDs, id) }) {
			scope.SectionIDs = append(scope.SectionIDs, section.ID)
			for _, id := range frames {
				selectedFrames[id] = true
			}
		}
	}
	frames := []string{}
	for _, id := range scope.FrameIDs {
		// An unassigned pixel obligation must not disappear during partitioning.
		if !knownFrames[id] || selectedFrames[id] {
			frames = append(frames, id)
		}
	}
	scope.FrameIDs = frames
	// 完整章节的跨范围引用仍须交付原文；补充证据不扩大本轮主题覆盖义务。
	scope = loopScopeEvidence(scope, a)
	return scope
}

func loopScopeEvidence(scope domain.ArticleReviewScope, a domain.Article) domain.ArticleReviewScope {
	collect := func(refs []string) {
		for _, ref := range refs {
			for _, id := range a.Citations[ref].SegmentIDs {
				if !slices.Contains(scope.SourceIDs, id) {
					scope.ContextSourceIDs = appendUnique(scope.ContextSourceIDs, id)
				}
			}
		}
	}
	for _, section := range a.Sections {
		if slices.Contains(scope.SectionIDs, section.ID) {
			for _, block := range section.Blocks {
				collect(blockCitationIDs(block))
			}
		}
	}
	for _, limitation := range a.Limitations {
		if len(limitation.SectionIDs) == 0 || slices.ContainsFunc(limitation.SectionIDs, func(id string) bool { return slices.Contains(scope.SectionIDs, id) }) {
			collect(limitation.CitationIDs)
		}
	}
	return scope
}

// A retried draft retains old fN citation keys but may copy only some frames.
// Present immutable frame IDs throughout review instead of two conflicting fN
// namespaces. This is a detached view; the frozen article/hash never changes.
func loopReviewPixelIdentities(a domain.Article) (domain.Article, error) {
	var out domain.Article
	if err := json.Unmarshal(domain.JSON(a), &out); err != nil {
		return out, err
	}
	refs := map[string]string{}
	for id, cit := range a.Citations {
		if cit.Kind == "video_frame" && cit.FrameID != "" {
			refs[id] = cit.FrameID
			delete(out.Citations, id)
			cit.ID = cit.FrameID
			out.Citations[cit.ID] = cit
		}
	}
	ids := func(values []string) {
		for i, id := range values {
			if frame, ok := refs[id]; ok {
				values[i] = frame
			}
		}
	}
	inline := func(values []domain.Inline) {
		for i, v := range values {
			if frame, ok := refs[v.CitationID]; ok {
				values[i].CitationID = frame
			}
		}
	}
	for i := range out.Sections {
		for j := range out.Sections[i].Blocks {
			b := &out.Sections[i].Blocks[j]
			ids(b.CitationIDs)
			inline(b.Content)
			for k := range b.Items {
				inline(b.Items[k].Content)
			}
			for k := range b.Rows {
				ids(b.Rows[k].CitationIDs)
			}
		}
	}
	for i := range out.Limitations {
		ids(out.Limitations[i].CitationIDs)
	}
	return out, nil
}

// Project persisted canonical content back into the author's own write language.
// The renderer AST belongs only to the program and the independent reviewer;
// exposing it as the editable draft invites incompatible type/blocks repairs.
func (s *session) loopAuthorDraft(a domain.Article, notes map[string]loopFrameNote, version int) (loopSaveInput, error) {
	out := loopSaveInput{ExpectedVersion: version, Title: &a.Title, Sections: []loopSection{}}
	textAliases, frameAliases := map[string]string{}, map[string]string{}
	for i, part := range s.transcript {
		textAliases[part.ID] = fmt.Sprint("t", i+1)
	}
	for i, frame := range s.cp.ArticleLoop.Frames {
		frameAliases[frame] = fmt.Sprint("f", i+1)
	}
	refs := func(ids []string) ([]string, error) {
		out := []string{}
		for _, id := range ids {
			cit, ok := a.Citations[id]
			if !ok {
				return nil, fmt.Errorf("draft citation %s missing", id)
			}
			if cit.Kind == "video_frame" {
				alias, ok := frameAliases[cit.FrameID]
				if !ok {
					return nil, fmt.Errorf("draft frame %s missing", cit.FrameID)
				}
				out = appendUnique(out, alias)
			} else {
				for _, segment := range cit.SegmentIDs {
					alias, ok := textAliases[segment]
					if !ok {
						return nil, fmt.Errorf("draft original %s missing", segment)
					}
					out = appendUnique(out, alias)
				}
			}
		}
		return out, nil
	}
	plain := func(in []domain.Inline) string {
		var text strings.Builder
		for _, part := range in {
			if part.Type == "text" {
				text.WriteString(part.Text)
			}
		}
		return text.String()
	}
	for _, sec := range a.Sections {
		projected := loopSection{SectionID: sec.ID, Title: sec.Title, Content: []loopContent{}}
		seenPixels := map[string]bool{}
		for _, block := range sec.Blocks {
			content := loopContent{}
			var err error
			content.Sources, err = refs(block.CitationIDs)
			if err != nil {
				return out, err
			}
			switch block.Type {
			case "paragraph", "quote":
				value := plain(block.Content)
				switch {
				case block.Origin == "background_explanation":
					content.Background, content.Sources = &value, nil
				case block.Type == "quote":
					content.Quote = &value
				default:
					content.Text = &value
				}
			case "code":
				content.Code = &loopCode{Text: block.Text}
				if block.Language != nil {
					content.Code.Language = *block.Language
				}
			case "list":
				content.List = &loopList{Items: []loopSourcedText{}}
				if block.Ordered != nil {
					content.List.Ordered = *block.Ordered
				}
				content.Sources = nil
				for _, item := range block.Items {
					ids := []string{}
					for _, part := range item.Content {
						if part.CitationID != "" {
							ids = appendUnique(ids, part.CitationID)
						}
					}
					sources, err := refs(ids)
					if err != nil {
						return out, err
					}
					content.List.Items = append(content.List.Items, loopSourcedText{Text: plain(item.Content), Sources: sources})
				}
			case "table":
				content.Table = &loopTable{Columns: block.Columns, Rows: []loopRow{}}
				content.Sources = nil
				for _, row := range block.Rows {
					sources, err := refs(row.CitationIDs)
					if err != nil {
						return out, err
					}
					content.Table.Rows = append(content.Table.Rows, loopRow{Cells: row.Cells, Sources: sources})
				}
			case "figure":
				content.Sources = nil
				content.Figure = &loopFigure{Caption: block.Caption, Frames: []string{}, VisibleFacts: []string{}, Uncertainty: []string{}}
				for _, id := range block.FrameIDs {
					alias, ok := frameAliases[id]
					if !ok {
						return out, fmt.Errorf("draft figure %s missing", id)
					}
					content.Figure.Frames = append(content.Figure.Frames, alias)
					for _, fact := range notes[id].VisibleFacts {
						content.Figure.VisibleFacts = appendUnique(content.Figure.VisibleFacts, fact)
					}
					for _, uncertainty := range notes[id].Uncertainty {
						content.Figure.Uncertainty = appendUnique(content.Figure.Uncertainty, uncertainty)
					}
				}
			default:
				return out, fmt.Errorf("unsupported persisted author block %s", block.Type)
			}
			projected.Content = append(projected.Content, content)
			for _, id := range blockCitationIDs(block) {
				cit := a.Citations[id]
				if cit.Kind == "video_frame" && !seenPixels[cit.FrameID] {
					seenPixels[cit.FrameID] = true
					note := notes[cit.FrameID]
					projected.Observations = append(projected.Observations, loopPixelObservation{Frame: frameAliases[cit.FrameID], VisibleFacts: note.VisibleFacts, Uncertainty: note.Uncertainty})
				}
			}
		}
		for _, limitation := range a.Limitations {
			if slices.Contains(limitation.SectionIDs, sec.ID) || len(limitation.SectionIDs) == 0 {
				sources, err := refs(limitation.CitationIDs)
				if err != nil {
					return out, err
				}
				projected.Limitations = append(projected.Limitations, loopSectionLimitation{Code: limitation.Code, Message: limitation.Message, Sources: sources})
			}
		}
		out.Sections = append(out.Sections, projected)
	}
	return out, nil
}
