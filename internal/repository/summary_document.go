package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

// SummaryValidationContext resolves frozen facts under the owning task. It
// deliberately accepts retained historical sources for user revision/undo;
// generation publication separately requires the active source and lease.
func (r *Repositories) SummaryValidationContext(ctx context.Context, owner, taskID int64, sourceID, sourceDigest, generationID string) (summarydoc.ValidationContext, error) {
	var out summarydoc.ValidationContext
	source, err := r.TextSource.Read(ctx, owner, taskID, sourceID)
	if err != nil {
		return out, err
	}
	if source.SourceDigest != sourceDigest {
		return out, artifact.Err("source_changed", 409)
	}
	out = summarydoc.ValidationContext{SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: source.Identity.MediaFingerprint, GenerationID: generationID, Cues: map[string]summarydoc.Cue{}, Figures: map[string]summarydoc.RegisteredFigure{}}
	for _, cue := range source.Cues {
		out.Cues[cue.ID] = summarydoc.Cue{StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod}
	}
	var figures []model.SummaryScreenshotRef
	if err := r.db.WithContext(ctx).Where("user_id = ? AND task_id = ? AND source_id = ? AND source_digest = ? AND generation_id = ? AND media_revision = ? AND inspected = ? AND status = ?", owner, taskID, sourceID, sourceDigest, generationID, out.MediaRevision, true, "ready").Find(&figures).Error; err != nil {
		return out, err
	}
	for _, figure := range figures {
		out.Figures[figure.ID] = summarydoc.RegisteredFigure{SourceID: figure.SourceID, SourceDigest: figure.SourceDigest, MediaRevision: figure.MediaRevision, BlockID: figure.BlockID, GenerationID: figure.GenerationID, CaptureMS: figure.CaptureMS, Inspected: figure.Inspected}
	}
	return out, nil
}

type PublishSummaryDocumentRequest struct {
	UserID                    int64
	TaskID                    int64
	GenerationID              string
	SourceID                  string
	SourceDigest              string
	LeaseToken                string
	ExpectedGeneratedVersion  int64
	ExpectedGeneratedHash     string
	ExpectedGeneratedHashKind string
	Document                  summarydoc.Document
	ModelName                 string
}

// PublishSummaryDocument derives both representations and replaces only the
// generated original. User revisions remain untouched. Lease, source,
// generation and old result all participate in the same publication CAS.
func (r *Repositories) PublishSummaryDocument(ctx context.Context, req PublishSummaryDocumentRequest) (*model.AISummary, error) {
	if req.GenerationID == "" || req.SourceID == "" || req.SourceDigest == "" || req.LeaseToken == "" {
		return nil, artifact.Err("invalid_request", 400)
	}
	var result *model.AISummary
	owned, err := r.RunWithTaskProcessingLease(TaskProcessingLeaseRequest{TaskID: req.TaskID, JobType: model.TaskJobTypeSummary, Token: req.LeaseToken, Now: time.Now()}, func(tx *Repositories) error {
		if _, err := tx.TextSource.LockSource(ctx, req.UserID, req.TaskID, req.SourceID, req.SourceDigest); err != nil {
			return err
		}
		job, err := tx.TaskJob.FindByTaskAndType(req.TaskID, model.TaskJobTypeSummary)
		if err != nil {
			return err
		}
		if job == nil || job.UserID != req.UserID || job.GenerationID != req.GenerationID || job.InputSourceID != req.SourceID {
			return artifact.Err("generation_stale", 409)
		}
		validation, err := tx.SummaryValidationContext(ctx, req.UserID, req.TaskID, req.SourceID, req.SourceDigest, req.GenerationID)
		if err != nil {
			return err
		}
		if err := summarydoc.Validate(req.Document, validation); err != nil {
			return artifact.Err("invalid_summary_document", 422)
		}
		canonical, err := summarydoc.CanonicalJSON(req.Document)
		if err != nil {
			return err
		}
		content, err := summarydoc.Markdown(req.Document)
		if err != nil {
			return err
		}
		digest, err := summarydoc.Digest(req.Document)
		if err != nil {
			return err
		}
		var prior model.AISummary
		err = tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", req.TaskID).First(&prior).Error
		found := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if found && prior.GenerationID == req.GenerationID && prior.ContentDigest == digest {
			result = &prior
			return nil
		}
		priorKind := model.SummaryHashMarkdown
		priorHash := ""
		if found {
			priorHash = artifact.Hash(prior.Content)
			if prior.DocumentJSON != "" {
				priorKind = model.SummaryHashDocument
				priorHash = prior.ContentDigest
			}
		}
		expectedKind := req.ExpectedGeneratedHashKind
		if expectedKind == "" {
			expectedKind = model.SummaryHashMarkdown
		}
		if prior.GeneratedVersion != req.ExpectedGeneratedVersion || priorHash != req.ExpectedGeneratedHash || priorKind != expectedKind {
			return artifact.Err("version_conflict", 409)
		}
		now := time.Now().UTC()
		row := model.AISummary{TaskID: req.TaskID, FileMD5: validation.MediaRevision, Content: content, DocumentJSON: string(canonical), SchemaVersion: summarydoc.SchemaVersion, SourceID: req.SourceID, SourceDigest: req.SourceDigest, ContentDigest: digest, ContentHashKind: summarydoc.HashKind, GeneratedVersion: prior.GeneratedVersion + 1, GenerationID: req.GenerationID, ModelName: req.ModelName, CreatedAt: now}
		if found {
			row.ID = prior.ID
			row.CreatedAt = prior.CreatedAt
		}
		values := map[string]any{"file_md5": row.FileMD5, "content": row.Content, "document_json": row.DocumentJSON, "schema_version": row.SchemaVersion, "source_id": row.SourceID, "source_digest": row.SourceDigest, "content_digest": row.ContentDigest, "content_hash_kind": row.ContentHashKind, "generated_version": row.GeneratedVersion, "generation_id": row.GenerationID, "model_name": row.ModelName}
		if err := tx.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}}, DoUpdates: clause.Assignments(values)}).Create(&row).Error; err != nil {
			return err
		}
		result = &row
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, artifact.Err("version_conflict", 409)
	}
	return result, nil
}
