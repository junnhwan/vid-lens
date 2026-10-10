package processing

const OperationVisualRetry = "visual_retry"

// A user-authorized new attempt grants a new bounded visual budget. The prior
// run remains immutable; automatic deliveries reuse this exact snapshot.
type VisualRetrySnapshot struct {
	ParentGenerationID     string `json:"parent_generation_id"`
	CheckpointGenerationID string `json:"checkpoint_generation_id,omitempty"`
	BaseDocumentJSON       string `json:"base_document_json"`
	BaseModelName          string `json:"base_model_name"`
	PreviousSpendJSON      string `json:"previous_spend_json"`
	NewBudgetAuthorized    bool   `json:"new_budget_authorized"`
}
