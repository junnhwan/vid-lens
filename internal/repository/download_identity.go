package repository

import (
	"context"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// FreezeDownloadIdentity is called after resolving metadata and before the
// first media transfer. Recovery reuses this snapshot, never resolves a new P.
func (r *Repositories) FreezeDownloadIdentity(ctx context.Context, taskID int64, token, snapshot string) error {
	owned, err := r.RunWithTaskProcessingLease(TaskProcessingLeaseRequest{TaskID: taskID, JobType: model.TaskJobTypeDownload, Token: token}, func(tx *Repositories) error {
		job, err := tx.TaskJob.FindByTaskAndType(taskID, model.TaskJobTypeDownload)
		if err != nil {
			return err
		}
		if job.InputSnapshotJSON != "" && job.InputSnapshotJSON != snapshot {
			return artifact.Err("source_identity_mismatch", 409)
		}
		return tx.db.WithContext(ctx).Model(job).Update("input_snapshot_json", snapshot).Error
	})
	if err != nil {
		return err
	}
	if !owned {
		return artifact.Err("version_conflict", 409)
	}
	return nil
}
