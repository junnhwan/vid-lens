package eval

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFrozenTranscriptBoundarySuite(t *testing.T) {
	b, err := os.ReadFile("../../docs/eval/transcript-cases.dev.json")
	if err != nil {
		t.Fatal(err)
	}
	var dataset TranscriptDataset
	if err := json.Unmarshal(b, &dataset); err != nil {
		t.Fatal(err)
	}
	r, err := EvaluateTranscript(dataset)
	if err != nil || !r.StructurePassed {
		t.Fatalf("err=%v report=%+v", err, r)
	}
	if r.HumanQualityStatus != "not_audited" || r.CER != nil || r.StartError.P95 != nil || r.PlaybackSuccessRate != nil {
		t.Fatal("synthetic structure promoted to manual precision")
	}
	if len(r.Cases) < 10 {
		t.Fatal("boundary suite incomplete")
	}
}
func TestTranscriptStatisticsIncludeUnknownUnmatchedAndPlaybackFailures(t *testing.T) {
	start, end := int64(800), int64(2100)
	yes, no := true, false
	annotation := func(text string, replay *bool) *TranscriptAnnotation {
		return &TranscriptAnnotation{Text: text, StartAllowedMS: []int64{1000, 1100}, EndAllowedMS: []int64{1900, 2000}, ReviewedBy: "fixture-human", ReviewedDate: "2026-10-06", SourceChecked: true, CompletePlayback: replay}
	}
	c := TranscriptCase{ID: "metric-fixture", Kind: "media", MediaSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", DurationMS: 5000, Sentences: []TranscriptSentence{
		{ID: "matched", Text: "Hello!", SourceIDs: []string{"s1"}, StartMS: &start, EndMS: &end, TimeStatus: "exact", Annotation: annotation("hello", &yes)},
		{ID: "unmatched", Text: "help", SourceIDs: []string{"s2"}, StartMS: &start, EndMS: &end, TimeStatus: "exact", Annotation: annotation("hello", &no)},
		{ID: "unknown", Text: "unknown", SourceIDs: []string{"s3"}},
	}}
	r, err := EvaluateTranscript(TranscriptDataset{SchemaVersion: 1, Split: "dev", Cases: []TranscriptCase{c}})
	if err != nil {
		t.Fatal(err)
	}
	if r.StartError.Count != 1 || *r.StartError.P95 != 200 || *r.EndError.P95 != 100 || r.Unmatched != 1 || r.Unknown != 1 || r.PlaybackReviewed != 2 || r.PlaybackUnknown != 1 || *r.PlaybackSuccessRate != .5 {
		t.Fatalf("biased metrics: %+v", r)
	}
	c.Sentences[0].Annotation.SourceChecked = false
	if _, err := EvaluateTranscript(TranscriptDataset{SchemaVersion: 1, Split: "dev", Cases: []TranscriptCase{c}}); err == nil {
		t.Fatal("unreviewed source admitted")
	}
}
