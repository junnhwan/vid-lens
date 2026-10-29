package investigator

import (
	"fmt"
	"slices"

	"vidlens/internal/domain"
)

type originalReading struct {
	Alias    string `json:"alias"`
	Original struct {
		StartMS int64  `json:"start_ms"`
		EndMS   int64  `json:"end_ms"`
		Text    string `json:"text"`
		Origin  string `json:"origin"`
	} `json:"original"`
}

// Keep every selected original's text, window and provenance. Source identities
// are represented once by stable aliases; their authoritative mapping stays in PG.
func readingOriginals(transcript []domain.Segment, selected []string) []originalReading {
	out := []originalReading{}
	for i, segment := range transcript {
		alias := fmt.Sprint("t", i+1)
		if selected != nil && !slices.Contains(selected, alias) {
			continue
		}
		view := originalReading{Alias: alias}
		view.Original.StartMS, view.Original.EndMS = segment.StartMS, segment.EndMS
		view.Original.Text, view.Original.Origin = segment.Text, segment.Origin
		out = append(out, view)
	}
	return out
}

func (s *session) questionReading() bool {
	return s.turn != nil && s.cp.ExecutionContract != nil && domain.QuestionReadingContract(s.cp.ExecutionContract.InputRevision)
}

func (s *session) semanticQuestionReading() bool {
	return s.turn != nil && s.cp.ExecutionContract != nil && domain.QuestionSemanticReadingContract(s.cp.ExecutionContract.InputRevision)
}

func (s *session) scopedQuestionReview() bool {
	return s.questionReading() && (s.cp.ExecutionContract.InputRevision == domain.QuestionReadingV3 || s.focusedQuestion())
}

func (s *session) focusedQuestion() bool {
	return s.questionReading() && (s.cp.ExecutionContract.InputRevision == domain.QuestionReadingV4 || s.compactQuestionReview())
}

func (s *session) compactQuestionReview() bool {
	return s.questionReading() && s.cp.ExecutionContract.InputRevision == domain.QuestionReadingV5
}

func (s *session) indexedReview() bool {
	return s.scopedQuestionReview() || s.boundedSourceContext() && s.chunkedSource()
}

// A scoped review may cite only originals actually delivered to it. Preserve
// custom citations only when all their source segments are present.
func questionReviewCatalog(source map[string]any, catalog map[string]domain.Citation) (map[string]domain.Citation, error) {
	passages, ok := source["transcript"].([]sourcePassage)
	if !ok {
		return nil, domain.Err(422, "INTERNAL_FAILURE", "问答审核缺少已选定的原文范围。")
	}
	delivered := map[string]bool{}
	for _, passage := range passages {
		delivered[passage.ID] = true
	}
	out := map[string]domain.Citation{}
	for alias, citation := range catalog {
		if citation.Kind == "video_text" && (len(citation.SegmentIDs) == 0 || slices.ContainsFunc(citation.SegmentIDs, func(id string) bool { return !delivered[id] })) {
			continue
		}
		out[alias] = citation
	}
	return out, nil
}

func (s *session) questionReviewReading(source, payload map[string]any, catalog map[string]domain.Citation) error {
	if !s.questionReading() {
		return nil
	}
	if s.scopedQuestionReview() {
		var err error
		catalog, err = questionReviewCatalog(source, catalog)
		if err != nil {
			return err
		}
		// The selector has already gathered every required passage. Compact
		// those originals without replacing the selection with the full video.
		passages := source["transcript"].([]sourcePassage)
		rows := make([][5]any, 0, len(passages))
		for _, passage := range passages {
			rows = append(rows, [5]any{passage.Alias, passage.StartMS, passage.EndMS, passage.Text, passage.Origin})
		}
		source["transcript"] = map[string]any{"columns": []string{"alias", "start_ms", "end_ms", "text", "origin"}, "rows": rows}
		source["source_coverage"] = map[string]any{"delivered_segments": len(passages), "total_segments": len(s.transcript), "scope": "cited_originals_and_neighbors"}
		source["question"] = s.turn.Question
		source["text_access"] = "Only the selected original passages are delivered, each complete and in chronological order. Each row follows transcript.columns exactly; text, timestamps and provenance are unchanged. The remaining video is indexed, not read. Missing necessary evidence requires not_assessed so the author can retrieve and cite it before resubmitting."
	} else {
		source["transcript"] = readingOriginals(s.transcript, nil)
	}
	aliases := map[string]string{}
	windows := map[string]domain.Range{}
	for i, segment := range s.transcript {
		aliases[segment.ID] = fmt.Sprint("t", i+1)
		end := segment.EndMS
		if s.snapshot.DurationMS > 0 {
			end = min(end, s.snapshot.DurationMS)
		}
		windows[segment.ID] = domain.Range{StartMS: segment.StartMS, EndMS: end}
	}
	bindings := map[string]any{}
	for id, citation := range catalog {
		if citation.Kind != "video_text" {
			bindings[id] = citation
			continue
		}
		if len(citation.SegmentIDs) == 1 && aliases[citation.SegmentIDs[0]] == id && citation.Quote == nil && citation.Range != nil && *citation.Range == windows[citation.SegmentIDs[0]] {
			bindings[id] = "video_text"
			continue
		}
		originals := []string{}
		for _, original := range citation.SegmentIDs {
			originals = append(originals, aliases[original])
		}
		bindings[id] = map[string]any{"kind": citation.Kind, "originals": originals, "range": citation.Range, "verbatim_quote": citation.Quote}
	}
	payload["citation_catalog"] = bindings
	source["reading_contract"] = "Every original text/window/origin is preserved. A citation_catalog tN:video_text tag binds that exact original alias and its media-intersected window; object entries retain custom original/range/quote bindings. Canonical identities are server-owned and already mechanically validated. Judge only this question's candidate and its evidence, with the full source available for necessary context."
	if s.scopedQuestionReview() {
		source["reading_contract"] = "citation_catalog binds only delivered originals and available non-text evidence. A tN:video_text tag binds that exact original alias and its media-intersected window; object entries retain custom original/range/quote bindings. Canonical identities are server-owned and already mechanically validated. Judge only the current question's candidate against the supplied evidence; the source index is navigation, never proof of an unread claim."
	}
	return nil
}
