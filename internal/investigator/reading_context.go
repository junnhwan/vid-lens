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

func (s *session) questionReviewReading(source, payload map[string]any, catalog map[string]domain.Citation) {
	if !s.questionReading() {
		return
	}
	source["transcript"] = readingOriginals(s.transcript, nil)
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
}
