package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/usertags"
)

type normalizedTagCandidate struct {
	TagCandidate
	Display, Key string
}

func (r *UserTagRepository) normalizeCandidates(tx *gorm.DB, req PublishTagCandidatesRequest) ([]normalizedTagCandidate, error) {
	if len(req.Candidates) > 1000 {
		return nil, artifact.Err("tag_candidate_limit", 400)
	}
	out := make([]normalizedTagCandidate, 0, len(req.Candidates))
	seen := map[string]bool{}
	for _, candidate := range req.Candidates {
		if !utf8.ValidString(candidate.Reason) || utf8.RuneCountInString(candidate.Reason) > 2000 {
			return nil, artifact.Err("invalid_request", 400)
		}
		name := candidate.Name
		if candidate.TagID != "" {
			tag, err := resolveTag(tx, req.UserID, candidate.TagID)
			if err != nil {
				return nil, err
			}
			candidate.TagID = tag.ID
			if strings.TrimSpace(name) == "" {
				name = tag.DisplayName
			}
		}
		display, key, err := usertags.Normalize(name, r.limits.NameRunes)
		if err != nil {
			return nil, artifact.Err("invalid_request", 400)
		}
		var entry model.UserTagName
		err = tx.Where("user_id = ? AND normalized_key = ?", req.UserID, key).First(&entry).Error
		if err == nil {
			tag, err := resolveTag(tx, req.UserID, entry.TagID)
			if err != nil {
				return nil, err
			}
			if candidate.TagID != "" && candidate.TagID != tag.ID {
				return nil, artifact.Err("tag_name_conflict", 409)
			}
			candidate.TagID = tag.ID
			display = tag.DisplayName
			_, key, _ = usertags.Normalize(display, r.limits.NameRunes)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		} else if candidate.TagID != "" {
			return nil, artifact.Err("tag_name_conflict", 409)
		}
		// Domain inference does not establish a user's reading/learning intentions.
		switch key {
		case "待读", "已学会", "面试重点", "待学习", "已学习", "未读", "已读", "to read", "learned", "interview focus":
			candidate.Uncertain = true
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, normalizedTagCandidate{candidate, display, key})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if len(out) > r.limits.Candidates {
		return nil, artifact.Err("tag_candidate_limit", 400)
	}
	return out, nil
}

func currentTagGeneration(tx *gorm.DB, owner, taskID int64, sourceDigest, generation string, version int64) (*model.AISummary, error) {
	task, err := summaryTask(tx, owner, taskID, true)
	if err != nil {
		return nil, err
	}
	var summary model.AISummary
	if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", taskID).First(&summary).Error; err != nil {
		return nil, hideMissing(err)
	}
	if summary.GenerationID != generation || summary.GeneratedVersion != version || summary.SourceDigest != sourceDigest || summary.SourceID == "" || task.ActiveTextSourceID != summary.SourceID {
		return nil, artifact.Err("tag_generation_stale", 409)
	}
	var source model.VideoTextSource
	if err = tx.Where("id = ? AND user_id = ? AND task_id = ? AND source_digest = ?", summary.SourceID, owner, taskID, sourceDigest).First(&source).Error; err != nil {
		return nil, artifact.Err("source_changed", 409)
	}
	return &summary, nil
}

// PublishCandidates consumes frozen recipe output. It performs no model call,
// cannot rename or merge vocabulary, and replaces only its auto assignments.
// Call independently after durable summary + pending-tag-intent publication.
func (r *UserTagRepository) PublishCandidates(ctx context.Context, req PublishTagCandidatesRequest) (TaskTagState, error) {
	if !req.Enabled {
		return r.TaskState(ctx, req.UserID, req.TaskID)
	}
	var out TaskTagState
	if req.SourceDigest == "" || req.GenerationID == "" || req.GeneratedVersion <= 0 || req.ExpectedTagVersion < 0 {
		return out, artifact.Err("invalid_request", 400)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, req.UserID); err != nil {
			return err
		}
		if _, err := currentTagGeneration(tx, req.UserID, req.TaskID, req.SourceDigest, req.GenerationID, req.GeneratedVersion); err != nil {
			return err
		}
		if req.LeaseToken != "" {
			if err := tagWorkerFence(tx, req); err != nil {
				return err
			}
		}
		candidates, err := r.normalizeCandidates(tx, req)
		if err != nil {
			return err
		}
		requestHash := artifact.Hash(artifact.JSON(req.Candidates))
		var batch model.VideoTagClassification
		err = tx.Where("user_id = ? AND task_id = ? AND generation_id = ? AND generated_version = ?", req.UserID, req.TaskID, req.GenerationID, req.GeneratedVersion).First(&batch).Error
		if err == nil {
			if batch.CandidateDigest != requestHash {
				return artifact.Err("idempotency_conflict", 409)
			}
			out, err = r.taskState(tx, req.UserID, req.TaskID)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		head, err := lockTagSet(tx, req.UserID, req.TaskID, req.ExpectedTagVersion)
		if err != nil {
			return err
		}
		if err = tx.Where("user_id = ? AND task_id = ? AND origin = ?", req.UserID, req.TaskID, "auto").Delete(&model.VideoTagAssignment{}).Error; err != nil {
			return err
		}
		if err = tx.Model(&model.VideoTagSuggestion{}).Where("user_id = ? AND task_id = ? AND status = ? AND (generation_id <> ? OR generated_version <> ? OR source_digest <> ?)", req.UserID, req.TaskID, "pending", req.GenerationID, req.GeneratedVersion, req.SourceDigest).Update("status", "stale").Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			rejected, err := tagRejected(tx, req.UserID, req.TaskID, candidate.TagID, candidate.Key)
			if err != nil {
				return err
			}
			status := "pending"
			tagID := candidate.TagID
			if rejected {
				status = "rejected"
			} else if !candidate.Uncertain {
				var tag model.UserTag
				if tagID != "" {
					tag, err = resolveTag(tx, req.UserID, tagID)
				} else {
					tag, err = r.createOrGet(tx, req.UserID, candidate.Display, "agent")
				}
				if err != nil {
					return err
				}
				tagID = tag.ID
				assignment := model.VideoTagAssignment{UserID: req.UserID, TaskID: req.TaskID, TagID: tagID, Origin: "auto", GenerationID: req.GenerationID, GeneratedVersion: req.GeneratedVersion, SourceDigest: req.SourceDigest, CreatedAt: time.Now().UTC()}
				if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&assignment).Error; err != nil {
					return err
				}
				status = "accepted"
			}
			suggestion := model.VideoTagSuggestion{ID: uuid.NewString(), UserID: req.UserID, TaskID: req.TaskID, GenerationID: req.GenerationID, GeneratedVersion: req.GeneratedVersion, NormalizedKey: candidate.Key, DisplayName: candidate.Display, TagID: tagID, SourceDigest: req.SourceDigest, Reason: candidate.Reason, Status: status, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
			if err = tx.Create(&suggestion).Error; err != nil {
				return err
			}
		}
		var count int64
		if err = tx.Model(&model.VideoTagAssignment{}).Where("user_id = ? AND task_id = ?", req.UserID, req.TaskID).Count(&count).Error; err != nil {
			return err
		}
		if count > int64(r.limits.Assignments) {
			return artifact.Err("tag_assignment_limit", 400)
		}
		if err = advanceTagSet(tx, head); err != nil {
			return err
		}
		batch = model.VideoTagClassification{UserID: req.UserID, TaskID: req.TaskID, GenerationID: req.GenerationID, GeneratedVersion: req.GeneratedVersion, SourceDigest: req.SourceDigest, CandidateDigest: requestHash, AppliedTagVersion: head.Version + 1, CreatedAt: time.Now().UTC()}
		if err = tx.Create(&batch).Error; err != nil {
			return err
		}
		out, err = r.taskState(tx, req.UserID, req.TaskID)
		return err
	})
	return out, err
}

type TagSuggestionDecisionInput struct {
	Decision        string `json:"decision"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (r *UserTagRepository) DecideSuggestion(ctx context.Context, owner, taskID int64, sid string, input TagSuggestionDecisionInput) (TaskTagState, error) {
	var out TaskTagState
	if input.Decision != "accept" && input.Decision != "reject" && input.Decision != "restore" {
		return out, artifact.Err("invalid_request", 400)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, owner); err != nil {
			return err
		}
		if _, err := summaryTask(tx, owner, taskID, true); err != nil {
			return err
		}
		var suggestion model.VideoTagSuggestion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND task_id = ?", sid, owner, taskID).First(&suggestion).Error; err != nil {
			return hideMissing(err)
		}
		if input.Decision == "accept" {
			if _, err := currentTagGeneration(tx, owner, taskID, suggestion.SourceDigest, suggestion.GenerationID, suggestion.GeneratedVersion); err != nil {
				return err
			}
		}
		// Exact repeats are harmless; a rejected item may still be restored or
		// explicitly accepted after a later manual decision.
		want := map[string]string{"accept": "accepted", "reject": "rejected", "restore": "pending"}[input.Decision]
		if suggestion.UserDecision == input.Decision && suggestion.DecisionBaseVersion == input.ExpectedVersion {
			var err error
			out, err = r.taskState(tx, owner, taskID)
			return err
		}
		head, err := lockTagSet(tx, owner, taskID, input.ExpectedVersion)
		if err != nil {
			return err
		}
		tagID := suggestion.TagID
		var tag model.UserTag
		if tagID != "" {
			tag, err = resolveTag(tx, owner, tagID)
			if err != nil {
				return err
			}
			tagID = tag.ID
		}
		switch input.Decision {
		case "accept":
			if tagID == "" {
				tag, err = r.createOrGet(tx, owner, suggestion.DisplayName, "manual")
				if err != nil {
					return err
				}
				tagID = tag.ID
			}
			if err = protectTag(tx, &tag); err != nil {
				return err
			}
			if err = clearTagRejections(tx, owner, taskID, tag); err != nil {
				return err
			}
			if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "task_id"}, {Name: "tag_id"}}, DoUpdates: clause.Assignments(map[string]any{"origin": "manual"})}).Create(&model.VideoTagAssignment{UserID: owner, TaskID: taskID, TagID: tagID, Origin: "manual", CreatedAt: time.Now().UTC()}).Error; err != nil {
				return err
			}
			if err = setTagDecision(tx, owner, taskID, tagID, suggestion.NormalizedKey, "accepted", head.Version+1); err != nil {
				return err
			}
		case "reject":
			if tagID != "" {
				if err = tx.Where("user_id = ? AND task_id = ? AND tag_id = ?", owner, taskID, tagID).Delete(&model.VideoTagAssignment{}).Error; err != nil {
					return err
				}
			}
			if err = setTagDecision(tx, owner, taskID, tagID, suggestion.NormalizedKey, "rejected", head.Version+1); err != nil {
				return err
			}
		case "restore":
			if tagID != "" {
				if err = clearTagRejections(tx, owner, taskID, tag); err != nil {
					return err
				}
			} else {
				if err = tx.Where("user_id = ? AND task_id = ? AND normalized_key = ?", owner, taskID, suggestion.NormalizedKey).Delete(&model.VideoTagDecision{}).Error; err != nil {
					return err
				}
			}
		}
		var count int64
		if err = tx.Model(&model.VideoTagAssignment{}).Where("user_id = ? AND task_id = ?", owner, taskID).Count(&count).Error; err != nil {
			return err
		}
		if count > int64(r.limits.Assignments) {
			return artifact.Err("tag_assignment_limit", 400)
		}
		suggestion.TagID = tagID
		suggestion.Status = want
		suggestion.UserDecision = input.Decision
		suggestion.DecisionBaseVersion = input.ExpectedVersion
		suggestion.UpdatedAt = time.Now().UTC()
		if err = tx.Save(&suggestion).Error; err != nil {
			return err
		}
		if err = advanceTagSet(tx, head); err != nil {
			return err
		}
		out, err = r.taskState(tx, owner, taskID)
		return err
	})
	return out, err
}
