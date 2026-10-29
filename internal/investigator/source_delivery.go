package investigator

import (
	"context"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

// Undelivered overview frames remain durable work, not observations. Never
// evict an unobserved frame merely to make the next provider request fit.
func (s *session) deliverOverviewBatch(ctx context.Context, messages *[]*schema.Message) error {
	if !s.boundedSourceContext() || s.sourceSurveyPending() || len(s.cp.PendingOverview) == 0 || len(s.unobserved(ctx)) > 0 {
		return nil
	}
	room, err := s.inspectionHeadroom(ctx)
	if err != nil {
		return err
	}
	count := min(3, room, len(s.cp.PendingOverview))
	if count == 0 {
		return nil
	}
	key := fmt.Sprint("overview-batch:", len(s.cp.OverviewIDs))
	parts, err := s.imageInput(ctx, s.cp.PendingOverview[:count], key)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, p := range parts {
		if id, ok := p.Extra["frame_id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) != count {
		return domain.Err(422, "INTERNAL_FAILURE", "概览批次缺少实际交付的图片身份。")
	}
	s.cp.Inspections[key] = ids
	s.cp.PendingOverview = s.cp.PendingOverview[count:]
	for _, id := range ids {
		if !slices.Contains(s.cp.OverviewIDs, id) {
			s.cp.OverviewIDs = append(s.cp.OverviewIDs, id)
		}
		if !slices.Contains(s.cp.CitationFrames, id) {
			s.cp.CitationFrames = append(s.cp.CitationFrames, id)
		}
	}
	*messages = append(*messages, &schema.Message{Role: schema.User, Content: "本批概览 inspection_id=" + key, UserInputMultiContent: parts})
	return nil
}

// A review receives every cited original segment and chronological neighbors,
// plus the complete chronological navigation ledger. It may not silently
// discard a required passage when even one atomic claim exceeds capacity.
func (s *session) indexedReviewSource(task reviewTask, catalog map[string]domain.Citation) (map[string]any, error) {
	var out map[string]any
	if s.scopedQuestionReview() {
		out = map[string]any{"snapshot_id": s.snapshot.ID, "source": s.snapshot.Source, "duration_ms": s.snapshot.DurationMS, "question": s.turn.Question, "source_index": s.sourceChunks(), "source_index_protocol": sourceIndexProtocol}
	} else {
		out = s.sourceContext()
	}
	delete(out, "current_chunk")
	selected := map[string]bool{}
	aliases := sourceAliases(task.Units)
	if s.scopedQuestionReview() {
		// Summary units inherit their body's citations even when output-budget
		// splitting puts the summary into a separate review task.
		for _, unit := range task.Units {
			aliases = append(aliases, unit.CitationAliases...)
		}
	}
	if len(aliases) > 0 {
		for _, alias := range aliases {
			if c, ok := catalog[alias]; ok {
				for _, id := range c.SegmentIDs {
					selected[id] = true
				}
			}
		}
	}
	if s.usesJointReview() {
		// A frame caption can contradict nearby speech even without an explicit
		// text alias. Deliver that original speech, not just global note samples.
		frames := reviewTaskFrames(task)
		for _, citation := range catalog {
			if citation.Kind != "video_frame" || citation.TimeMS == nil || !slices.Contains(frames, citation.FrameID) {
				continue
			}
			for _, segment := range s.transcript {
				if segment.StartMS <= *citation.TimeMS && (*citation.TimeMS < segment.EndMS || *citation.TimeMS == s.snapshot.DurationMS && segment.EndMS == s.snapshot.DurationMS) {
					selected[segment.ID] = true
				}
			}
		}
	}
	// Global headings and uncited scope claims still receive representative
	// originals from every completed card, not only the author's article prose.
	if len(selected) == 0 {
		for _, note := range s.cp.SourceNotes {
			for _, alias := range note.EvidenceIDs {
				if c, ok := catalog[alias]; ok {
					for _, id := range c.SegmentIDs {
						selected[id] = true
					}
				}
			}
		}
		if s.turn != nil {
			for _, hit := range sourceSearch(s.transcript, s.turn.Question, nil, nil) {
				selected[hit.ID] = true
			}
		}
	}
	neighbors := map[string]bool{}
	for i, seg := range s.transcript {
		if selected[seg.ID] {
			for j := max(0, i-1); j <= min(len(s.transcript)-1, i+1); j++ {
				neighbors[s.transcript[j].ID] = true
			}
		}
	}
	originals := []sourcePassage{}
	for i, seg := range s.transcript {
		if neighbors[seg.ID] {
			originals = append(originals, sourcePassage{Segment: seg, Alias: fmt.Sprint("t", i+1), TextEnd: utf8.RuneCountInString(seg.Text)})
		}
	}
	if utf8.RuneCount(domain.JSON(originals)) > max(18000, min(80000, separateInputLimit(s.profile)/2)) {
		return nil, domain.Err(422, "REVIEW_INPUT_TOO_LARGE", "本审核单元所需原文超过单次预算；已保留草稿和完整证据，未删除必要材料后判为通过。")
	}
	out["transcript"] = originals
	out["text_access"] = "All cited original segments and their chronological neighbors; the complete source index is navigation, not proof. not_assessed is required if the delivered originals cannot establish a material assertion."
	return out, nil
}
