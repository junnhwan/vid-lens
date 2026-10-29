package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

// Resolve navigation through the immutable article's source identities, then
// deliver originals. A Chinese chapter title need not match English subtitles.
// Each section/selection has its own window; never bridge distant selections.
func (s *session) questionOriginals() ([]sourcePassage, int) {
	selected := map[string]bool{}
	if s.article != nil {
		for _, section := range s.article.Sections {
			whole := section.ID == s.turn.Context.SectionID || s.turn.Context.SectionID == "" && len([]rune(section.Title)) >= 4 && strings.Contains(s.turn.Question, section.Title)
			ids := map[string]bool{}
			for _, block := range section.Blocks {
				chosen := whole
				for _, passage := range s.turn.Context.SelectedPassages() {
					chosen = chosen || passage.SectionID == section.ID && passage.BlockID == block.ID
				}
				if !chosen {
					continue
				}
				for _, alias := range blockCitationIDs(block) {
					c := s.article.Citations[alias]
					if c.Kind == "video_text" && c.SnapshotID == s.snapshot.ID {
						for _, id := range c.SegmentIDs {
							ids[id] = true
						}
					}
				}
			}
			first, last := len(s.transcript), -1
			for i, segment := range s.transcript {
				if ids[segment.ID] {
					first, last = min(first, i), max(last, i)
				}
			}
			if last >= 0 {
				for i := max(0, first-1); i <= min(len(s.transcript)-1, last+1); i++ {
					selected[s.transcript[i].ID] = true
				}
			}
		}
	}
	if len(selected) == 0 {
		for _, hit := range sourceSearch(s.transcript, s.turn.Question, nil, nil) {
			for i, segment := range s.transcript {
				if segment.ID == hit.ID {
					for j := max(0, i-2); j <= min(len(s.transcript)-1, i+2); j++ {
						selected[s.transcript[j].ID] = true
					}
				}
			}
		}
	}
	// A short source can be read completely without a tool round trip.
	if len(selected) == 0 && !s.chunkedSource() {
		for _, segment := range s.transcript {
			selected[segment.ID] = true
		}
	}
	limit := max(1500, min(18000, separateInputLimit(s.profile)/5))
	out, size := []sourcePassage{}, 0
	for i, segment := range s.transcript {
		if !selected[segment.ID] {
			continue
		}
		p := sourcePassage{Segment: segment, Alias: fmt.Sprint("t", i+1), TextEnd: utf8.RuneCountInString(segment.Text)}
		cost := utf8.RuneCount(domain.JSON([5]any{p.Alias, p.StartMS, p.EndMS, p.Text, p.Origin}))
		if size+cost > limit {
			break
		}
		out, size = append(out, p), size+cost
	}
	return out, len(selected)
}

func (s *session) focusedQuestionSource() map[string]any {
	s.cp.SourceIndexProtocol = sourceIndexProtocol
	passages, total := s.questionOriginals()
	rows := make([][5]any, 0, len(passages))
	for _, p := range passages {
		rows = append(rows, [5]any{p.Alias, p.StartMS, p.EndMS, p.Text, p.Origin})
	}
	return map[string]any{
		"snapshot_id": s.snapshot.ID, "source": s.snapshot.Source, "duration_ms": s.snapshot.DurationMS,
		"transcript": map[string]any{"columns": []string{"alias", "start_ms", "end_ms", "text", "origin"}, "rows": rows}, "source_index": s.sourceChunks(), "source_index_protocol": sourceIndexProtocol,
		"source_coverage": map[string]any{"delivered_segments": len(passages), "selected_segments": total, "total_segments": len(s.transcript), "has_more": len(passages) < total},
		"text_access":     "transcript contains actual original text, timestamps and citation_alias. It is already read; do not read it again. source_index is navigation. If necessary evidence is outside delivered passages, read_source can retrieve it. An unread gap does not mean the video omits the fact.",
	}
}

// Only remove a known, semantically empty legacy decoration. Non-null quote,
// unknown fields and ambiguous bodies still fail the declared strict schema.
func normalizeQuestionJSON(raw string) string {
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &body) != nil {
		return raw
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(body["blocks"], &blocks) != nil {
		return raw
	}
	changed := false
	for _, block := range blocks {
		if string(block["type"]) == `"quote"` && strings.TrimSpace(string(block["quote"])) == "null" {
			delete(block, "quote")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	body["blocks"] = domain.JSON(blocks)
	return string(domain.JSON(body))
}

// Pixel metadata survives in the frozen model input only after real pixels have
// passed the transport budget. Count successful author sends, never captions,
// planned inspections or reviewer-only images.
func questionViewedFrames(input frozenModelInput) []string {
	ids := []string{}
	if input.Budget.ImageCount == 0 || input.Budget.Phase == "evidence_review" {
		return ids
	}
	for _, message := range input.Messages {
		for _, part := range message.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeImageURL && part.Image != nil {
				if id, ok := part.Extra["frame_id"].(string); ok && id != "" && !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func (s *session) answerEvidence(ctx context.Context, a *domain.Answer) error {
	evidence := &domain.AnswerEvidence{TextSources: []domain.AnswerTextSource{}, ViewedFrameIDs: []string{}, Review: "passed"}
	byID := map[string]domain.Segment{}
	for _, segment := range s.transcript {
		byID[segment.ID] = segment
	}
	aliases := []string{}
	for alias := range a.Citations {
		aliases = append(aliases, alias)
	}
	slices.SortFunc(aliases, func(aID, bID string) int {
		a, b := a.Citations[aID], a.Citations[bID]
		if (a.Range != nil) != (b.Range != nil) {
			if a.Range != nil {
				return -1
			}
			return 1
		}
		if a.Range != nil && b.Range != nil && a.Range.StartMS != b.Range.StartMS {
			if a.Range.StartMS < b.Range.StartMS {
				return -1
			}
			return 1
		}
		return strings.Compare(aID, bID)
	})
	for _, alias := range aliases {
		citation := a.Citations[alias]
		if citation.Kind != "video_text" {
			continue
		}
		parts := []string{}
		for _, id := range citation.SegmentIDs {
			parts = append(parts, byID[id].Text)
		}
		evidence.TextSources = append(evidence.TextSources, domain.AnswerTextSource{CitationID: alias, Text: strings.Join(parts, "\n")})
	}
	for i := 0; i < s.cp.Calls; i++ {
		key := fmt.Sprint("model:", i)
		op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
		if err != nil {
			return err
		}
		if op.Status != "completed" || op.Submissions == 0 {
			continue
		}
		op, err = s.engine.Store.Operation(ctx, s.claim.Run.ID, "input:"+key)
		if err != nil {
			return err
		}
		var input frozenModelInput
		if err = json.Unmarshal(op.Result, &input); err != nil {
			return err
		}
		for _, id := range questionViewedFrames(input) {
			if !slices.Contains(evidence.ViewedFrameIDs, id) {
				evidence.ViewedFrameIDs = append(evidence.ViewedFrameIDs, id)
			}
		}
	}
	frames := storeFrameIDs(a.Blocks, a.Citations)
	for _, id := range evidence.ViewedFrameIDs {
		if !slices.Contains(frames, id) {
			frames = append(frames, id)
		}
	}
	var err error
	a.Frames, err = s.engine.Store.PublicFrames(ctx, s.turn.RequestID, frames)
	if err != nil {
		return err
	}
	a.Evidence = evidence
	return nil
}
