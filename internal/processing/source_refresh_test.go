package processing

import "testing"

func TestSourceRefreshBodyFingerprintExcludesClassificationAndReadingPreference(t *testing.T) {
	old := Intent{Options: Options{AutoSummary: true, TextSourcePolicy: "prefer_platform", SummaryInstruction: "explain", MindmapEnabled: true}, ProfileID: 3, ProfileFingerprint: "profile", SummaryPreference: "preference", RecipeVersion: Recipe, BudgetJSON: "budget"}
	next := old
	next.Options.AutoTagsEnabled = true
	next.Options.MindmapEnabled = false
	next.TagVocabulary = &TagVocabularySnapshot{Version: 42, Candidates: []TagVocabularyEntry{{TagID: "tag-one", Name: "coding"}}}
	version := int64(8)
	next.ExpectedTagVersion = &version
	if SourceSummaryInputFingerprint(old) != SourceSummaryInputFingerprint(next) {
		t.Fatal("classification/read-only preferences invalidate prose")
	}
	if SourceClassificationFingerprint(old) == SourceClassificationFingerprint(next) {
		t.Fatal("classification change hidden")
	}
	next.Options.SummaryInstruction = "changed instruction"
	if SourceSummaryInputFingerprint(old) == SourceSummaryInputFingerprint(next) {
		t.Fatal("content change reused old prose")
	}
}
