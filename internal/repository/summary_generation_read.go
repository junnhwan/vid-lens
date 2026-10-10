package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

// A short shared-lock transaction gives the reader one run/event high-watermark
// while deletion and publication follow the same task -> run lock order.
type SummaryGenerationRead struct {
	Task         model.VideoTask
	Job          *model.TaskJob
	Intent       *processing.Intent
	Run          *model.AgentRun
	Steps        []model.AgentStep
	Events       []model.RunEvent
	Source       *model.VideoTextSource
	Effective    *EffectiveSummary
	GenerationID string
}

func (r *Repositories) ReadSummaryGeneration(ctx context.Context, owner, taskID int64, generation string) (*SummaryGenerationRead, error) {
	if owner <= 0 || taskID <= 0 || len(generation) > 36 {
		return nil, artifact.Err("invalid_request", 400)
	}
	var out SummaryGenerationRead
	err := r.TransactionContext(ctx, func(tx *Repositories) error {
		task, err := summaryTask(tx.db.Clauses(clause.Locking{Strength: "SHARE"}), owner, taskID, false)
		if err != nil {
			return err
		}
		out.Task = *task
		job, err := tx.TaskJob.FindByTaskAndType(taskID, model.TaskJobTypeSummary)
		if err != nil {
			return err
		}
		out.Job = job
		if task.ProcessingIntentJSON != "" {
			intent, decodeErr := processing.Decode(task.ProcessingIntentJSON)
			if decodeErr != nil {
				return artifact.Err("invalid_generation_snapshot", 409)
			}
			out.Intent = &intent
		}
		if out.Intent != nil && (job == nil || job.GenerationID != out.Intent.GenerationID) && (task.LastJobType == model.TaskJobTypeTextSource || task.LastJobType == model.TaskJobTypeTranscribe) {
			sourceJob, readErr := tx.TaskJob.FindByTaskAndType(taskID, task.LastJobType)
			if readErr != nil {
				return readErr
			}
			if sourceJob != nil && sourceJob.GenerationID == out.Intent.GenerationID {
				var frozen processing.SourceRefreshSnapshot
				if json.Unmarshal([]byte(sourceJob.InputSnapshotJSON), &frozen) == nil && frozen.Operation == processing.OperationSourceRefresh {
					job = sourceJob
					out.Job = sourceJob
				}
			}
		}
		selected := ""
		if job != nil && job.GenerationID != "" {
			selected = job.GenerationID
		} else if out.Intent != nil {
			selected = out.Intent.GenerationID
		}
		if generation != "" {
			selected = generation
		}
		out.GenerationID = selected
		var run model.AgentRun
		if selected != "" {
			err = tx.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=? AND subject_id=?", selected, owner, taskID, model.AgentRunSubjectSummaryGeneration, selected).First(&run).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				out.Run = &run
				if err = tx.db.Where("run_id=?", selected).Order("sequence ASC, attempt ASC").Find(&out.Steps).Error; err != nil {
					return err
				}
				if err = tx.db.Where("run_id=? AND seq<=?", selected, run.EventSeq).Order("seq ASC").Find(&out.Events).Error; err != nil {
					return err
				}
			} else if generation != "" && selected != currentGenerationID(job, out.Intent) {
				return artifact.Err("not_found", 404)
			}
		}
		sourceID := task.ActiveTextSourceID
		if out.Run != nil {
			var policy struct {
				SourceID string `json:"source_id"`
			}
			if json.Unmarshal([]byte(run.PolicySnapshot), &policy) != nil {
				return artifact.Err("invalid_generation_checkpoint", 409)
			}
			sourceID = policy.SourceID
		} else if job != nil && job.GenerationID == selected && job.InputSourceID != "" && !(job.JobType != model.TaskJobTypeSummary && job.Status == model.TaskStatusCompleted) {
			sourceID = job.InputSourceID
		}
		if sourceID != "" {
			var source model.VideoTextSource
			if err = tx.db.Where("id=? AND user_id=? AND task_id=?", sourceID, owner, taskID).First(&source).Error; err != nil {
				return hideMissing(err)
			}
			out.Source = &source
		}
		out.Effective, err = tx.SummaryRevision.Effective(ctx, owner, taskID)
		return err
	})
	return &out, err
}
func currentGenerationID(job *model.TaskJob, intent *processing.Intent) string {
	if job != nil && job.GenerationID != "" {
		return job.GenerationID
	}
	if intent != nil {
		return intent.GenerationID
	}
	return ""
}

func (r *Repositories) ReadSummaryGenerationEvents(ctx context.Context, owner, taskID int64, generation string, after int64, limit int) (*SummaryGenerationRead, []model.RunEvent, bool, error) {
	if strings.TrimSpace(generation) == "" || after < 0 || limit < 1 || limit > 100 {
		return nil, nil, false, artifact.Err("invalid_request", 400)
	}
	read, err := r.ReadSummaryGeneration(ctx, owner, taskID, generation)
	if err != nil {
		return nil, nil, false, err
	}
	high := int64(0)
	if read.Run != nil {
		high = read.Run.EventSeq
	}
	if after > high {
		return nil, nil, false, artifact.Err("event_cursor_ahead", 409)
	}
	events := make([]model.RunEvent, 0, limit)
	expected := after + 1
	gap := false
	for _, event := range read.Events {
		if event.Seq <= after {
			continue
		}
		if event.Seq != expected {
			gap = true
		}
		expected = event.Seq + 1
		if len(events) < limit {
			events = append(events, event)
		} else {
			break
		}
	}
	if len(events) == 0 && after < high {
		gap = true
	}
	if len(events) > 0 && events[len(events)-1].Seq < high && len(events) < limit {
		gap = true
	}
	return read, events, gap, nil
}
