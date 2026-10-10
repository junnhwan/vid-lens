package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// Merge is exclusively an explicit user mutation. Recipe candidates cannot
// invoke it. Deterministically equivalent canonical names/confirmed aliases
// are reused by createOrGet before a second entity exists.
func (r *UserTagRepository) Merge(ctx context.Context, owner int64, sourceID, key string, input TagMergeInput) (UserTagView, error) {
	var out UserTagView
	if err := artifact.ValidateKey(key); err != nil {
		return out, err
	}
	if sourceID == "" || sourceID == input.TargetID || input.SourceVersion < 1 || input.TargetVersion < 1 {
		return out, artifact.Err("invalid_request", 400)
	}
	hash := artifact.Hash(artifact.JSON(struct {
		SourceID string
		Input    TagMergeInput
	}{sourceID, input}))
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, owner); err != nil {
			return err
		}
		var receipt model.UserTagMergeRecord
		err := tx.Where("user_id = ? AND key = ?", owner, key).First(&receipt).Error
		if err == nil {
			if receipt.RequestHash != hash {
				return artifact.Err("idempotency_conflict", 409)
			}
			return json.Unmarshal([]byte(receipt.ResultJSON), &out)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		ids := []string{sourceID, input.TargetID}
		sort.Strings(ids)
		var rows []model.UserTag
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND id IN ?", owner, ids).Order("id ASC").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 2 {
			return artifact.Err("not_found", 404)
		}
		byID := map[string]model.UserTag{}
		for _, tag := range rows {
			byID[tag.ID] = tag
		}
		source, target := byID[sourceID], byID[input.TargetID]
		if source.Status != "active" || target.Status != "active" || source.Version != input.SourceVersion || target.Version != input.TargetVersion {
			return artifact.Err("version_conflict", 409)
		}
		var names []model.UserTagName
		if err = tx.Where("user_id = ? AND tag_id IN ?", owner, ids).Find(&names).Error; err != nil {
			return err
		}
		if len(names)-1 > r.limits.Aliases {
			return artifact.Err("tag_alias_limit", 400)
		}
		// Every name is checked before moving any relationship. A third owner of
		// a key can never be silently included in this merge.
		for _, name := range names {
			var actual model.UserTagName
			if err = tx.Where("user_id = ? AND normalized_key = ?", owner, name.NormalizedKey).First(&actual).Error; err != nil {
				return err
			}
			if actual.TagID != source.ID && actual.TagID != target.ID {
				return artifact.Err("tag_name_conflict", 409)
			}
		}
		var assignments []model.VideoTagAssignment
		if err = tx.Where("user_id = ? AND tag_id = ?", owner, source.ID).Find(&assignments).Error; err != nil {
			return err
		}
		tasks := map[int64]bool{}
		for _, assignment := range assignments {
			tasks[assignment.TaskID] = true
			var existing model.VideoTagAssignment
			find := tx.Where("user_id = ? AND task_id = ? AND tag_id = ?", owner, assignment.TaskID, target.ID).First(&existing).Error
			if find != nil && !errors.Is(find, gorm.ErrRecordNotFound) {
				return find
			}
			if find == nil && existing.Origin == "manual" {
				continue
			}
			if find == nil && assignment.Origin != "manual" {
				continue
			}
			assignment.TagID = target.ID
			if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "task_id"}, {Name: "tag_id"}}, DoUpdates: clause.AssignmentColumns([]string{"origin", "generation_id", "generated_version", "source_digest"})}).Create(&assignment).Error; err != nil {
				return err
			}
		}
		if err = tx.Where("user_id = ? AND tag_id = ?", owner, source.ID).Delete(&model.VideoTagAssignment{}).Error; err != nil {
			return err
		}
		var decisions []model.VideoTagDecision
		if err = tx.Where("user_id = ? AND tag_id = ?", owner, source.ID).Find(&decisions).Error; err != nil {
			return err
		}
		for _, decision := range decisions {
			tasks[decision.TaskID] = true
			var current model.VideoTagDecision
			find := tx.Where("user_id = ? AND task_id = ? AND decision_key = ?", owner, decision.TaskID, "tag:"+target.ID).First(&current).Error
			if find != nil && !errors.Is(find, gorm.ErrRecordNotFound) {
				return find
			}
			if find == nil && current.Decision == "rejected" {
				decision.Decision = "rejected"
			}
			if err = setTagDecision(tx, owner, decision.TaskID, target.ID, decision.NormalizedKey, decision.Decision, decision.Version); err != nil {
				return err
			}
		}
		if err = tx.Where("user_id = ? AND tag_id = ?", owner, source.ID).Delete(&model.VideoTagDecision{}).Error; err != nil {
			return err
		}
		var suggestions []model.VideoTagSuggestion
		if err = tx.Where("user_id = ? AND tag_id = ?", owner, source.ID).Find(&suggestions).Error; err != nil {
			return err
		}
		for _, suggestion := range suggestions {
			tasks[suggestion.TaskID] = true
		}
		if err = tx.Model(&model.VideoTagSuggestion{}).Where("user_id = ? AND tag_id = ?", owner, source.ID).Update("tag_id", target.ID).Error; err != nil {
			return err
		}
		if err = tx.Model(&model.UserTagName{}).Where("user_id = ? AND tag_id = ?", owner, source.ID).Updates(map[string]any{"tag_id": target.ID, "kind": "alias"}).Error; err != nil {
			return err
		}
		var automatic []model.VideoTagAssignment
		if err = tx.Where("user_id = ? AND tag_id = ? AND origin = ?", owner, target.ID, "auto").Find(&automatic).Error; err != nil {
			return err
		}
		for _, assignment := range automatic {
			rejected, err := tagRejected(tx, owner, assignment.TaskID, target.ID, "")
			if err != nil {
				return err
			}
			if rejected {
				tasks[assignment.TaskID] = true
				if err = tx.Where("user_id = ? AND task_id = ? AND tag_id = ? AND origin = ?", owner, assignment.TaskID, target.ID, "auto").Delete(&model.VideoTagAssignment{}).Error; err != nil {
					return err
				}
			}
		}
		source.Status = "merged"
		source.MergedIntoID = &target.ID
		source.Version++
		source.ProtectedByUser = true
		source.UpdatedAt = time.Now().UTC()
		target.Version++
		target.ProtectedByUser = true
		target.UpdatedAt = source.UpdatedAt
		if err = tx.Save(&source).Error; err != nil {
			return err
		}
		if err = tx.Save(&target).Error; err != nil {
			return err
		}
		// Merge changes task-level decisions/ranges. Increment every affected set
		// so a classification frozen before this vocabulary mutation must recompute.
		taskIDs := make([]int64, 0, len(tasks))
		for id := range tasks {
			taskIDs = append(taskIDs, id)
		}
		sort.Slice(taskIDs, func(i, j int) bool { return taskIDs[i] < taskIDs[j] })
		for _, id := range taskIDs {
			head := model.VideoTagSetHead{UserID: owner, TaskID: id}
			if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&head).Error; err != nil {
				return err
			}
			if err = tx.Model(&model.VideoTagSetHead{}).Where("user_id = ? AND task_id = ?", owner, id).Update("version", gorm.Expr("version + 1")).Error; err != nil {
				return err
			}
		}
		if err = bumpVocabulary(tx, owner); err != nil {
			return err
		}
		out, err = tagView(tx, target)
		if err != nil {
			return err
		}
		return tx.Create(&model.UserTagMergeRecord{UserID: owner, Key: key, RequestHash: hash, ResultJSON: artifact.JSON(out), CreatedAt: time.Now().UTC()}).Error
	})
	return out, err
}
