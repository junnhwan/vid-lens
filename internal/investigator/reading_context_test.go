package investigator

import (
	"reflect"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestQuestionReviewReadingPreservesEveryOriginalAndCitationBinding(t *testing.T) {
	s := &session{turn: &domain.Turn{}, cp: checkpoint{ExecutionContract: &domain.ExecutionContract{InputRevision: domain.QuestionReadingV1}}, transcript: []domain.Segment{
		{ID: "owned-original-A", StartMS: 0, EndMS: 100, Text: "First full original.", Origin: "asr"},
		{ID: "owned-original-B", StartMS: 100, EndMS: 200, Text: "Final full original.", Origin: "asr"},
	}}
	catalog := map[string]domain.Citation{"t2": {ID: "t2", Kind: "video_text", SegmentIDs: []string{"owned-original-B"}, Range: &domain.Range{StartMS: 110, EndMS: 190}}}
	source, payload := map[string]any{"transcript": s.transcript}, map[string]any{}
	s.questionReviewReading(source, payload, catalog)
	view := source["transcript"].([]originalReading)
	if len(view) != len(s.transcript) {
		t.Fatal("question review dropped original source coverage")
	}
	for i, original := range s.transcript {
		if view[i].Original.Text != original.Text || view[i].Original.StartMS != original.StartMS || view[i].Original.EndMS != original.EndMS || view[i].Original.Origin != original.Origin {
			t.Fatal("question review changed source content, windows or provenance")
		}
	}
	binding := payload["citation_catalog"].(map[string]any)["t2"].(map[string]any)
	if !reflect.DeepEqual(binding["originals"], []string{"t2"}) || binding["range"] != catalog["t2"].Range || strings.Contains(string(domain.JSON(source)), "owned-original") {
		t.Fatal("compact review lost an exact original/range binding")
	}
	if len(readingOriginals(s.transcript, []string{})) != 0 || len(readingOriginals(s.transcript, []string{"t2"})) != 1 {
		t.Fatal("empty or partial article review scope gained unrelated source material")
	}
	s.cp.ExecutionContract.InputRevision = ""
	source = map[string]any{"transcript": s.transcript}
	s.questionReviewReading(source, payload, catalog)
	if !reflect.DeepEqual(source["transcript"], s.transcript) {
		t.Fatal("historical frozen QA review input changed")
	}
}
