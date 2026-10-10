package repository

import (
	"fmt"
	"vid-lens/internal/model"
)

// FreezeSummaryInput is used inside the source handoff transaction after the
// independent summary dispatch is prepared. Retries never call this setter.
func (r *TaskJobRepository) FreezeSummaryInput(taskID int64, generationID, sourceID, snapshot string) error {
	if taskID <= 0 || generationID == "" || sourceID == "" || snapshot == "" {
		return fmt.Errorf("incomplete summary snapshot")
	}
	result := r.db.Model(&model.TaskJob{}).Where("task_id = ? AND job_type = ? AND status = ? AND lease_kind = ?", taskID, model.TaskJobTypeSummary, model.TaskStatusQueued, model.TaskLeaseKindDispatch).Updates(map[string]any{"generation_id": generationID, "input_source_id": sourceID, "input_snapshot_json": snapshot})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrInitialTaskDispatchConflict
	}
	return nil
}

// RecordTextSourceOutcome records a safe source selection reason after its
// lease has completed in the same handoff transaction.
func (r *TaskJobRepository) RecordTextSourceOutcome(taskID int64, reason, detail string) error {
	if len(reason) > 100 || len(detail) > 500 {
		return fmt.Errorf("source outcome too long")
	}
	result := r.db.Model(&model.TaskJob{}).Where("task_id = ? AND job_type = ? AND status = ? AND processing_token = ''", taskID, "text_source", model.TaskStatusCompleted).Updates(map[string]any{"last_error_code": reason, "last_error_msg": detail})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrInitialTaskDispatchConflict
	}
	return nil
}
