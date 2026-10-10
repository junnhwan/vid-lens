package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/textsource"
)

type TextSourceRepository struct{ db *gorm.DB }

func NewTextSourceRepository(db *gorm.DB) *TextSourceRepository { return &TextSourceRepository{db: db} }

// Read checks the task as well as the immutable source scope. Historical
// snapshots remain readable only while their owning task remains accessible.
func (r *TextSourceRepository) Read(ctx context.Context, owner, taskID int64, sourceID string) (*textsource.Snapshot, error) {
	if _, err := summaryTask(r.db.WithContext(ctx), owner, taskID, false); err != nil {
		return nil, err
	}
	var source model.VideoTextSource
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ? AND task_id = ?", sourceID, owner, taskID).First(&source).Error; err != nil {
		return nil, hideMissing(err)
	}
	var cues []model.VideoTextCue
	if err := r.db.WithContext(ctx).Where("source_id = ?", source.ID).Order("\"order\" ASC, id ASC").Find(&cues).Error; err != nil {
		return nil, err
	}
	return textSourceSnapshot(source, cues)
}

func (r *TextSourceRepository) Active(ctx context.Context, owner, taskID int64) (*textsource.Snapshot, error) {
	task, err := summaryTask(r.db.WithContext(ctx), owner, taskID, false)
	if err != nil {
		return nil, err
	}
	if task.ActiveTextSourceID == "" {
		return nil, nil
	}
	return r.Read(ctx, owner, taskID, task.ActiveTextSourceID)
}

type PublishTextSourceRequest struct {
	UserID                 int64
	TaskID                 int64
	ExpectedActiveSourceID string
	Snapshot               textsource.Snapshot
	// Workers provide their current parent job lease. Explicit service refreshes
	// may leave this empty, but still use source/media CAS and task ownership.
	Lease *TaskProcessingLeaseRequest
}

// PublishTextSource changes source, compatibility text, and retrieval freshness
// in one transaction. advance adds the next durable job to this SAME transaction;
// callers publish MQ messages only after it commits. No remote work is allowed
// in advance. If anything fails, neither the source nor its pointer is published.
func (r *Repositories) PublishTextSource(ctx context.Context, req PublishTextSourceRequest, advance func(*Repositories, *model.VideoTextSource) error) (*model.VideoTextSource, error) {
	if req.Snapshot.Quality != textsource.QualityUsable {
		return nil, artifact.Err("subtitle_unusable", 422)
	}
	snapshot, err := textsource.Canonicalize(req.Snapshot, textsource.DefaultLimits())
	if err != nil {
		return nil, artifact.Err("subtitle_unusable", 422)
	}
	if snapshot.Quality != textsource.QualityUsable {
		return nil, artifact.Err("subtitle_unusable", 422)
	}
	if snapshot.Identity.MediaFingerprint == "" {
		return nil, artifact.Err("source_identity_mismatch", 409)
	}
	if snapshot.Identity.Platform == "bilibili" && (snapshot.Identity.BVID == "" || snapshot.Identity.CID <= 0 || snapshot.Identity.PartIndex <= 0) {
		return nil, artifact.Err("source_identity_mismatch", 409)
	}
	var result *model.VideoTextSource
	err = r.TransactionContext(ctx, func(tx *Repositories) error {
		task, err := summaryTask(tx.db, req.UserID, req.TaskID, true)
		if err != nil {
			return err
		}
		if task.FileMD5 != snapshot.Identity.MediaFingerprint || task.FileURL == "" || task.Stage == model.TaskStageDownloading {
			return artifact.Err("source_identity_mismatch", 409)
		}
		if snapshot.Identity.Platform == "bilibili" {
			var identity textsource.Identity
			if json.Unmarshal([]byte(task.MediaIdentityJSON), &identity) != nil || identity != snapshot.Identity {
				return artifact.Err("source_identity_mismatch", 409)
			}
		}
		if req.Lease != nil {
			lease := *req.Lease
			if lease.TaskID != task.ID {
				return artifact.Err("version_conflict", 409)
			}
			owned, err := tx.OwnsTaskProcessing(lease)
			if err != nil {
				return err
			}
			if !owned {
				return artifact.Err("version_conflict", 409)
			}
		}
		if task.ActiveTextSourceID != req.ExpectedActiveSourceID {
			var active model.VideoTextSource
			if err := tx.db.Where("id = ? AND user_id = ? AND task_id = ? AND source_digest = ?", task.ActiveTextSourceID, task.UserID, task.ID, snapshot.SourceDigest).First(&active).Error; err != nil {
				return artifact.Err("source_changed", 409)
			}
		}
		source, cues, err := textSourceModels(task, snapshot)
		if err != nil {
			return err
		}
		var existing model.VideoTextSource
		err = tx.db.Where("user_id = ? AND task_id = ? AND source_digest = ?", task.UserID, task.ID, source.SourceDigest).First(&existing).Error
		if err == nil {
			source = existing
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if err = tx.db.Create(&source).Error; err != nil {
				return err
			}
			if err = tx.db.CreateInBatches(cues, 500).Error; err != nil {
				return err
			}
		} else {
			return err
		}
		if task.ActiveTextSourceID != source.ID {
			transcription := &model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: source.CanonicalText, Words: utf8.RuneCountInString(source.CanonicalText), SourceID: source.ID, SourceKind: source.Kind, SourceDigest: source.SourceDigest}
			if err = tx.SaveTranscriptionAndInvalidateIndex(transcription); err != nil {
				return err
			}
			// Existing legacy Upsert only updates its legacy fields. The projection
			// metadata is written atomically here so it cannot enter MD5 caches.
			if err = tx.db.Model(&model.VideoTranscription{}).Where("task_id = ?", task.ID).Updates(map[string]any{"source_id": source.ID, "source_kind": source.Kind, "source_digest": source.SourceDigest}).Error; err != nil {
				return err
			}
			if err = tx.db.Model(task).Update("active_text_source_id", source.ID).Error; err != nil {
				return err
			}
			if err = tx.cancelReplacedSourceGenerations(ctx, task.UserID, task.ID, source.ID); err != nil {
				return err
			}
		}
		if advance != nil {
			if err = advance(tx, &source); err != nil {
				return err
			}
		}
		result = &source
		return nil
	})
	return result, err
}

// Called only inside source publication, after the task/source lock. Moving
// the active pointer also closes old generation activity in that transaction.
func (r *Repositories) cancelReplacedSourceGenerations(ctx context.Context, owner, taskID int64, newSourceID string) error {
	var runs []model.AgentRun
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND task_id=? AND subject_kind=? AND status IN ?", owner, taskID, model.AgentRunSubjectSummaryGeneration, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).Find(&runs).Error; err != nil {
		return err
	}
	for _, run := range runs {
		var policy struct {
			SourceID string `json:"source_id"`
		}
		_ = json.Unmarshal([]byte(run.PolicySnapshot), &policy)
		if policy.SourceID == newSourceID {
			continue
		}
		now := time.Now().UTC()
		if err := r.CloseSummaryGenerationActivities(&run, "cancelled", "source_changed", now); err != nil {
			return err
		}
		if err := r.db.Model(&run).Updates(map[string]any{"status": model.AgentRunStatusCancelled, "stop_reason": "source_changed", "finished_at": now, "updated_at": now, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		if err := appendEvent(r.db, &run, "run.cancelled", map[string]any{"status": "cancelled", "stop_reason": "source_changed", "finished_at": now}); err != nil {
			return err
		}
	}
	return nil
}

func textSourceModels(task *model.VideoTask, snapshot textsource.Snapshot) (model.VideoTextSource, []model.VideoTextCue, error) {
	identity, err := json.Marshal(snapshot.Identity)
	if err != nil {
		return model.VideoTextSource{}, nil, err
	}
	warnings, err := json.Marshal(snapshot.Warnings)
	if err != nil {
		return model.VideoTextSource{}, nil, err
	}
	if snapshot.Warnings == nil {
		warnings = []byte("[]")
	}
	id := uuid.NewString()
	source := model.VideoTextSource{ID: id, UserID: task.UserID, TaskID: task.ID, Kind: snapshot.Kind, IdentityJSON: string(identity), MediaFingerprint: snapshot.Identity.MediaFingerprint, Language: snapshot.Language, TrackKey: snapshot.TrackKey, SubtitleKind: snapshot.SubtitleKind, KindBasis: snapshot.KindBasis, RawObjectKey: snapshot.RawObjectKey, RawHash: snapshot.RawHash, CanonicalText: snapshot.CanonicalText, CanonicalHash: snapshot.CanonicalHash, SourceDigest: snapshot.SourceDigest, ParserVersion: snapshot.ParserVersion, Quality: snapshot.Quality, WarningsJSON: string(warnings), CreatedAt: time.Now().UTC()}
	cues := make([]model.VideoTextCue, 0, len(snapshot.Cues))
	for _, cue := range snapshot.Cues {
		raw, err := json.Marshal(cue.RawRefs)
		if err != nil {
			return source, nil, err
		}
		cues = append(cues, model.VideoTextCue{SourceID: id, CueID: cue.ID, Order: cue.Order, RawText: cue.RawText, Text: cue.Text, JoinBefore: cue.JoinBefore, StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod, RawRefsJSON: string(raw)})
	}
	return source, cues, nil
}

func textSourceSnapshot(source model.VideoTextSource, cues []model.VideoTextCue) (*textsource.Snapshot, error) {
	snapshot := textsource.Snapshot{ID: source.ID, RawObjectKey: source.RawObjectKey, Kind: source.Kind, TrackKey: source.TrackKey, Language: source.Language, SubtitleKind: source.SubtitleKind, KindBasis: source.KindBasis, ParserVersion: source.ParserVersion, RawHash: source.RawHash, CanonicalText: source.CanonicalText, CanonicalHash: source.CanonicalHash, SourceDigest: source.SourceDigest, Quality: source.Quality}
	if err := json.Unmarshal([]byte(source.IdentityJSON), &snapshot.Identity); err != nil {
		return nil, fmt.Errorf("stored source identity: %w", err)
	}
	if err := json.Unmarshal([]byte(source.WarningsJSON), &snapshot.Warnings); err != nil {
		return nil, fmt.Errorf("stored source warnings: %w", err)
	}
	for _, cue := range cues {
		row := textsource.Cue{ID: cue.CueID, Order: cue.Order, RawText: cue.RawText, Text: cue.Text, JoinBefore: cue.JoinBefore, StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod}
		if err := json.Unmarshal([]byte(cue.RawRefsJSON), &row.RawRefs); err != nil {
			return nil, fmt.Errorf("stored cue mapping: %w", err)
		}
		snapshot.Cues = append(snapshot.Cues, row)
	}
	if err := textsource.Validate(snapshot, textsource.DefaultLimits()); err != nil {
		return nil, fmt.Errorf("stored source invalid: %w", err)
	}
	if source.Quality != textsource.QualityUsable || snapshot.Identity.MediaFingerprint != source.MediaFingerprint || artifact.Hash(snapshot.CanonicalText) != source.CanonicalHash {
		return nil, fmt.Errorf("stored source integrity mismatch")
	}
	digest, err := textsource.Digest(snapshot)
	if err != nil || digest != source.SourceDigest {
		return nil, fmt.Errorf("stored source digest mismatch")
	}
	return &snapshot, nil
}

// DeleteTaskSources is called inside task cleanup, after raw object keys have
// been captured by the object cleanup plan. Never delete shared media here.
func (r *TextSourceRepository) DeleteTaskSources(taskID int64) error {
	if err := r.db.Where("task_id = ?", taskID).Delete(&model.SummaryScreenshotRef{}).Error; err != nil {
		return err
	}
	sources := r.db.Model(&model.VideoTextSource{}).Select("id").Where("task_id = ?", taskID)
	if err := r.db.Where("source_id IN (?)", sources).Delete(&model.VideoTextCue{}).Error; err != nil {
		return err
	}
	return r.db.Where("task_id = ?", taskID).Delete(&model.VideoTextSource{}).Error
}

func (r *TextSourceRepository) RawObjects(taskID int64) ([]string, error) {
	var rows []model.VideoTextSource
	if err := r.db.Select("raw_object_key").Where("task_id = ?", taskID).Find(&rows).Error; err != nil {
		return nil, err
	}
	var keys []string
	seen := map[string]bool{}
	for _, row := range rows {
		key := strings.TrimSpace(row.RawObjectKey)
		if key != "" && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// LockSource binds generated publications to both the owner and current source.
// Call it on transaction repositories so its row locks fence the publication.
func (r *TextSourceRepository) LockSource(ctx context.Context, owner, taskID int64, sourceID, digest string) (*model.VideoTextSource, error) {
	task, err := summaryTask(r.db.WithContext(ctx), owner, taskID, true)
	if err != nil {
		return nil, err
	}
	if task.ActiveTextSourceID != sourceID {
		return nil, artifact.Err("source_changed", 409)
	}
	var source model.VideoTextSource
	if err = r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_id = ? AND user_id = ? AND source_digest = ? AND media_fingerprint = ?", sourceID, taskID, owner, digest, task.FileMD5).First(&source).Error; err != nil {
		return nil, hideMissing(err)
	}
	return &source, nil
}
