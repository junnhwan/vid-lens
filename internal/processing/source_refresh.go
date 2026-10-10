package processing

// Source refresh is a new user-accepted operation; automatic queue retries keep
// this identity. It uses the existing source/transcribe jobs and summary recipe.
const OperationSourceRefresh = "source_refresh"

type SourceRefreshSnapshot struct {
	Operation                 string `json:"operation"`
	Intent                    Intent `json:"intent"`
	ExpectedActiveSourceID    string `json:"expected_active_source_id"`
	PreviousInputFingerprint  string `json:"previous_input_fingerprint"`
	ASRCheckpointGenerationID string `json:"asr_checkpoint_generation_id,omitempty"`
	ReuseUnscopedASRWindows   bool   `json:"reuse_unscoped_asr_windows,omitempty"`
	ClassificationChanged     bool   `json:"classification_changed,omitempty"`
}

func SourceSummaryInputFingerprint(intent Intent) string {
	opts := intent.Options
	opts.ProfileID = intent.ProfileID
	// Classification and reading preferences do not invalidate generated prose.
	opts.AutoTagsEnabled = false
	opts.MindmapEnabled = false
	return Fingerprint(struct {
		Options                                                          Options
		ProfileID                                                        int64
		ProfileFingerprint, SummaryPreference, RecipeVersion, BudgetJSON string
	}{opts, intent.ProfileID, intent.ProfileFingerprint, intent.SummaryPreference, intent.RecipeVersion, intent.BudgetJSON})
}

func SourceClassificationFingerprint(intent Intent) string {
	return Fingerprint([]any{intent.Options.AutoTagsEnabled, intent.TagVocabulary, intent.ExpectedTagVersion})
}
