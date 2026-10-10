package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/usertags"
)

type UserTagRepository struct {
	db     *gorm.DB
	limits usertags.Limits
}

func NewUserTagRepository(db *gorm.DB, limits ...usertags.Limits) *UserTagRepository {
	l := usertags.DefaultLimits()
	if len(limits) > 0 {
		l = limits[0].WithDefaults()
	}
	return &UserTagRepository{db, l}
}

type UserTagView struct {
	model.UserTag
	Aliases    []string `json:"aliases"`
	VideoCount int64    `json:"video_count"`
}
type AssignedTag struct {
	model.VideoTagAssignment
	Tag UserTagView `json:"tag"`
}
type TagClassificationState struct {
	Status           string `json:"status"`
	Enabled          bool   `json:"enabled"`
	ErrorCode        string `json:"error_code,omitempty"`
	GeneratedVersion int64  `json:"generated_version"`
}

type TaskTagState struct {
	Classification *TagClassificationState    `json:"classification,omitempty"`
	TaskID         int64                      `json:"task_id"`
	Version        int64                      `json:"version"`
	Assignments    []AssignedTag              `json:"assignments"`
	Suggestions    []model.VideoTagSuggestion `json:"suggestions"`
	Decisions      []model.VideoTagDecision   `json:"decisions"`
}
type TagPatch struct {
	ExpectedVersion int64    `json:"expected_version"`
	AddIDs          []string `json:"add_ids"`
	RemoveIDs       []string `json:"remove_ids"`
	KeepAutoIDs     []string `json:"keep_auto_ids"`
}
type TagFilter struct {
	IDs   []string
	Match string
}
type TagCandidate struct {
	TagID     string `json:"tag_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Reason    string `json:"reason"`
	Uncertain bool   `json:"uncertain"`
}
type PublishTagCandidatesRequest struct {
	UserID, TaskID                       int64
	SourceDigest, GenerationID           string
	LeaseToken                           string
	GeneratedVersion, ExpectedTagVersion int64
	Enabled                              bool
	Candidates                           []TagCandidate
}
type TagMergeInput struct {
	TargetID      string `json:"target_id"`
	SourceVersion int64  `json:"source_version"`
	TargetVersion int64  `json:"target_version"`
}

func (r *UserTagRepository) lockVocabulary(tx *gorm.DB, owner int64) error {
	if owner <= 0 {
		return artifact.Err("invalid_request", 400)
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.UserTagVocabularyHead{UserID: owner}).Error; err != nil {
		return err
	}
	var head model.UserTagVocabularyHead
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", owner).First(&head).Error
}
func bumpVocabulary(tx *gorm.DB, owner int64) error {
	return tx.Model(&model.UserTagVocabularyHead{}).Where("user_id = ?", owner).Update("version", gorm.Expr("version + 1")).Error
}

func resolveTag(tx *gorm.DB, owner int64, id string) (model.UserTag, error) {
	seen := map[string]bool{}
	for hops := 0; hops < 64; hops++ {
		if id == "" || len(id) > 128 || seen[id] {
			return model.UserTag{}, artifact.Err("not_found", 404)
		}
		seen[id] = true
		var tag model.UserTag
		if err := tx.Where("id = ? AND user_id = ?", id, owner).First(&tag).Error; err != nil {
			return tag, hideMissing(err)
		}
		if tag.Status == "active" {
			return tag, nil
		}
		if tag.Status != "merged" || tag.MergedIntoID == nil {
			return tag, artifact.Err("not_found", 404)
		}
		id = *tag.MergedIntoID
	}
	return model.UserTag{}, artifact.Err("invalid_tag_chain", 409)
}

func tagView(tx *gorm.DB, tag model.UserTag) (UserTagView, error) {
	view := UserTagView{UserTag: tag, Aliases: []string{}}
	var names []model.UserTagName
	if err := tx.Where("user_id = ? AND tag_id = ? AND kind = ?", tag.UserID, tag.ID, "alias").Order("normalized_key").Find(&names).Error; err != nil {
		return view, err
	}
	for _, name := range names {
		view.Aliases = append(view.Aliases, name.DisplayName)
	}
	err := tx.Model(&model.VideoTagAssignment{}).Joins("JOIN video_tasks AS t ON t.id = video_tag_assignments.task_id AND t.user_id = video_tag_assignments.user_id AND t.deleted_at IS NULL").Where("video_tag_assignments.user_id = ? AND video_tag_assignments.tag_id = ?", tag.UserID, tag.ID).Count(&view.VideoCount).Error
	return view, err
}

func (r *UserTagRepository) createOrGet(tx *gorm.DB, owner int64, name, origin string) (model.UserTag, error) {
	display, key, err := usertags.Normalize(name, r.limits.NameRunes)
	if err != nil {
		return model.UserTag{}, artifact.Err("invalid_request", 400)
	}
	var entry model.UserTagName
	err = tx.Where("user_id = ? AND normalized_key = ?", owner, key).First(&entry).Error
	if err == nil {
		tag, err := resolveTag(tx, owner, entry.TagID)
		if err != nil {
			return tag, err
		}
		if origin == "manual" && !tag.ProtectedByUser {
			tag.ProtectedByUser = true
			tag.Version++
			tag.UpdatedAt = time.Now().UTC()
			if err = tx.Save(&tag).Error; err != nil {
				return tag, err
			}
			err = bumpVocabulary(tx, owner)
		}
		return tag, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UserTag{}, err
	}
	now := time.Now().UTC()
	tag := model.UserTag{ID: uuid.NewString(), UserID: owner, DisplayName: display, CreationOrigin: origin, ProtectedByUser: origin == "manual", Version: 1, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err = tx.Create(&tag).Error; err != nil {
		return tag, err
	}
	if err = tx.Create(&model.UserTagName{UserID: owner, NormalizedKey: key, TagID: tag.ID, DisplayName: display, Kind: "canonical"}).Error; err != nil {
		return tag, err
	}
	return tag, bumpVocabulary(tx, owner)
}

func (r *UserTagRepository) Create(ctx context.Context, owner int64, name string) (UserTagView, error) {
	var out UserTagView
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, owner); err != nil {
			return err
		}
		tag, err := r.createOrGet(tx, owner, name, "manual")
		if err != nil {
			return err
		}
		out, err = tagView(tx, tag)
		return err
	})
	return out, err
}

func (r *UserTagRepository) Get(ctx context.Context, owner int64, id string) (UserTagView, error) {
	tag, err := resolveTag(r.db.WithContext(ctx), owner, id)
	if err != nil {
		return UserTagView{}, err
	}
	return tagView(r.db.WithContext(ctx), tag)
}

func (r *UserTagRepository) List(ctx context.Context, owner int64, page, size int, search string, sortBy ...string) ([]UserTagView, int64, error) {
	if owner <= 0 || page < 1 || size < 1 || size > r.limits.ListPage {
		return nil, 0, artifact.Err("invalid_request", 400)
	}
	sortMode := "name"
	if len(sortBy) > 0 && sortBy[0] != "" {
		sortMode = sortBy[0]
	}
	if sortMode != "name" && sortMode != "usage" {
		return nil, 0, artifact.Err("invalid_request", 400)
	}
	query := r.db.WithContext(ctx).Model(&model.UserTag{}).Where("user_id = ? AND status = ?", owner, "active")
	if strings.TrimSpace(search) != "" {
		_, key, err := usertags.Normalize(search, r.limits.NameRunes)
		if err != nil {
			return nil, 0, artifact.Err("invalid_request", 400)
		}
		query = query.Where("EXISTS (SELECT 1 FROM user_tag_names AS n WHERE n.user_id = user_tags.user_id AND n.tag_id = user_tags.id AND n.normalized_key LIKE ? ESCAPE '\\')", "%"+escapeTagLike(key)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var tags []model.UserTag
	if sortMode == "usage" {
		query = query.Select("user_tags.*, (SELECT COUNT(*) FROM video_tag_assignments a JOIN video_tasks t ON t.id = a.task_id AND t.user_id = a.user_id AND t.deleted_at IS NULL WHERE a.user_id = user_tags.user_id AND a.tag_id = user_tags.id) AS usage_count").Order("usage_count DESC")
	}
	if err := query.Order("display_name ASC, id ASC").Offset((page - 1) * size).Limit(size).Find(&tags).Error; err != nil {
		return nil, 0, err
	}
	out := make([]UserTagView, 0, len(tags))
	for _, tag := range tags {
		view, err := tagView(r.db.WithContext(ctx), tag)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, view)
	}
	return out, total, nil
}
func escapeTagLike(s string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(s)
}

func (r *UserTagRepository) Rename(ctx context.Context, owner int64, id, name string, expected int64) (UserTagView, error) {
	var out UserTagView
	display, key, err := usertags.Normalize(name, r.limits.NameRunes)
	if err != nil {
		return out, artifact.Err("invalid_request", 400)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, owner); err != nil {
			return err
		}
		tag, err := resolveTag(tx, owner, id)
		if err != nil {
			return err
		}
		if tag.ID != id || tag.Version != expected {
			return artifact.Err("version_conflict", 409)
		}
		var collision model.UserTagName
		err = tx.Where("user_id = ? AND normalized_key = ?", owner, key).First(&collision).Error
		if err == nil && collision.TagID != tag.ID {
			return artifact.Err("tag_name_conflict", 409)
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err = tx.Model(&model.UserTagName{}).Where("user_id = ? AND tag_id = ? AND kind = ?", owner, id, "canonical").Update("kind", "alias").Error; err != nil {
			return err
		}
		entry := model.UserTagName{UserID: owner, NormalizedKey: key, TagID: id, DisplayName: display, Kind: "canonical"}
		if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "normalized_key"}}, DoUpdates: clause.AssignmentColumns([]string{"display_name", "kind"})}).Create(&entry).Error; err != nil {
			return err
		}
		var aliasCount int64
		if err = tx.Model(&model.UserTagName{}).Where("user_id = ? AND tag_id = ? AND kind = ?", owner, id, "alias").Count(&aliasCount).Error; err != nil {
			return err
		}
		if aliasCount > int64(r.limits.Aliases) {
			return artifact.Err("tag_alias_limit", 400)
		}
		tag.DisplayName = display
		tag.Version++
		tag.ProtectedByUser = true
		tag.UpdatedAt = time.Now().UTC()
		if err = tx.Save(&tag).Error; err != nil {
			return err
		}
		if err = bumpVocabulary(tx, owner); err != nil {
			return err
		}
		out, err = tagView(tx, tag)
		return err
	})
	return out, err
}

func (r *UserTagRepository) SetAliases(ctx context.Context, owner int64, id string, aliases []string, expected int64) (UserTagView, error) {
	var out UserTagView
	entries := map[string]string{}
	for _, name := range aliases {
		display, key, err := usertags.Normalize(name, r.limits.NameRunes)
		if err != nil {
			return out, artifact.Err("invalid_request", 400)
		}
		entries[key] = display
	}
	if len(entries) > r.limits.Aliases {
		return out, artifact.Err("invalid_request", 400)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, owner); err != nil {
			return err
		}
		tag, err := resolveTag(tx, owner, id)
		if err != nil {
			return err
		}
		if tag.ID != id || tag.Version != expected {
			return artifact.Err("version_conflict", 409)
		}
		_, canonical, _ := usertags.Normalize(tag.DisplayName, r.limits.NameRunes)
		delete(entries, canonical)
		for key := range entries {
			var collision model.UserTagName
			err = tx.Where("user_id = ? AND normalized_key = ?", owner, key).First(&collision).Error
			if err == nil && collision.TagID != id {
				return artifact.Err("tag_name_conflict", 409)
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if err = tx.Where("user_id = ? AND tag_id = ? AND kind = ?", owner, id, "alias").Delete(&model.UserTagName{}).Error; err != nil {
			return err
		}
		for key, display := range entries {
			if err = tx.Create(&model.UserTagName{UserID: owner, NormalizedKey: key, TagID: id, DisplayName: display, Kind: "alias"}).Error; err != nil {
				return err
			}
		}
		tag.ProtectedByUser = true
		tag.Version++
		tag.UpdatedAt = time.Now().UTC()
		if err = tx.Save(&tag).Error; err != nil {
			return err
		}
		if err = bumpVocabulary(tx, owner); err != nil {
			return err
		}
		out, err = tagView(tx, tag)
		return err
	})
	return out, err
}

func (r *UserTagRepository) ResolveIDs(ctx context.Context, owner int64, ids []string) ([]string, error) {
	unique := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 128 {
			return nil, artifact.Err("invalid_request", 400)
		}
		unique[id] = true
	}
	if len(unique) > r.limits.FilterIDs {
		return nil, artifact.Err("invalid_request", 400)
	}
	resolved := map[string]bool{}
	for id := range unique {
		tag, err := resolveTag(r.db.WithContext(ctx), owner, id)
		if err != nil {
			return nil, err
		}
		resolved[tag.ID] = true
	}
	out := make([]string, 0, len(resolved))
	for id := range resolved {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (r *UserTagRepository) taskState(tx *gorm.DB, owner, taskID int64) (TaskTagState, error) {
	out := TaskTagState{TaskID: taskID, Assignments: []AssignedTag{}, Suggestions: []model.VideoTagSuggestion{}, Decisions: []model.VideoTagDecision{}}
	task, err := summaryTask(tx, owner, taskID, false)
	if err != nil {
		return out, err
	}
	var head model.VideoTagSetHead
	err = tx.Where("user_id = ? AND task_id = ?", owner, taskID).First(&head).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return out, err
	}
	out.Version = head.Version
	var assigned []model.VideoTagAssignment
	if err = tx.Where("user_id = ? AND task_id = ?", owner, taskID).Order("created_at, tag_id").Find(&assigned).Error; err != nil {
		return out, err
	}
	for _, assignment := range assigned {
		tag, err := resolveTag(tx, owner, assignment.TagID)
		if err != nil {
			return out, err
		}
		view, err := tagView(tx, tag)
		if err != nil {
			return out, err
		}
		out.Assignments = append(out.Assignments, AssignedTag{assignment, view})
	}
	if err = tx.Where("user_id = ? AND task_id = ?", owner, taskID).Order("created_at, id").Find(&out.Suggestions).Error; err != nil {
		return out, err
	}
	var generated model.AISummary
	genErr := tx.Where("task_id = ?", taskID).First(&generated).Error
	if genErr != nil && !errors.Is(genErr, gorm.ErrRecordNotFound) {
		return out, genErr
	}
	if genErr == nil && generated.GenerationID != "" {
		var intent model.SummaryTagIntent
		err := tx.Where("user_id=? AND task_id=? AND generation_id=? AND generated_version=? AND source_digest=?", owner, taskID, generated.GenerationID, generated.GeneratedVersion, generated.SourceDigest).First(&intent).Error
		if err == nil {
			out.Classification = &TagClassificationState{Status: intent.Status, Enabled: intent.Enabled, ErrorCode: intent.ErrorCode, GeneratedVersion: intent.GeneratedVersion}
			if task.ActiveTextSourceID != generated.SourceID && out.Classification.Status == "pending" {
				out.Classification.Status = "cancelled"
				out.Classification.ErrorCode = "tag_generation_stale"
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return out, err
		}
	}
	for i := range out.Suggestions {
		suggestion := &out.Suggestions[i]
		if suggestion.Status == "pending" && (genErr != nil || generated.GenerationID != suggestion.GenerationID || generated.GeneratedVersion != suggestion.GeneratedVersion || generated.SourceDigest != suggestion.SourceDigest || task.ActiveTextSourceID != generated.SourceID) {
			suggestion.Status = "stale"
		}
	}
	if err = tx.Where("user_id = ? AND task_id = ?", owner, taskID).Order("decision_key").Find(&out.Decisions).Error; err != nil {
		return out, err
	}
	rejectedIDs, rejectedKeys := map[string]bool{}, map[string]bool{}
	acceptedIDs, acceptedKeys := map[string]bool{}, map[string]bool{}
	for _, decision := range out.Decisions {
		if decision.Decision == "rejected" {
			if decision.TagID != "" {
				rejectedIDs[decision.TagID] = true
			}
			if decision.NormalizedKey != "" {
				rejectedKeys[decision.NormalizedKey] = true
			}
		}
		if decision.Decision == "accepted" {
			if decision.TagID != "" {
				acceptedIDs[decision.TagID] = true
			}
			if decision.NormalizedKey != "" {
				acceptedKeys[decision.NormalizedKey] = true
			}
		}
	}
	for i := range out.Suggestions {
		suggestion := &out.Suggestions[i]
		suggestion.EffectiveStatus = suggestion.Status
		tagID := suggestion.TagID
		if tagID != "" {
			resolved, resolveErr := resolveTag(tx, owner, tagID)
			if resolveErr != nil {
				return out, resolveErr
			}
			tagID = resolved.ID
		}
		if rejectedIDs[tagID] || rejectedKeys[suggestion.NormalizedKey] {
			suggestion.EffectiveStatus = "rejected"
		} else if acceptedIDs[tagID] || acceptedKeys[suggestion.NormalizedKey] {
			suggestion.EffectiveStatus = "accepted"
		}
	}
	return out, nil
}
func (r *UserTagRepository) TaskState(ctx context.Context, owner, taskID int64) (TaskTagState, error) {
	return r.taskState(r.db.WithContext(ctx), owner, taskID)
}

func lockTagSet(tx *gorm.DB, owner, taskID, expected int64) (model.VideoTagSetHead, error) {
	head := model.VideoTagSetHead{UserID: owner, TaskID: taskID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&head).Error; err != nil {
		return head, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND task_id = ?", owner, taskID).First(&head).Error; err != nil {
		return head, err
	}
	if head.Version != expected {
		return head, artifact.Err("version_conflict", 409)
	}
	return head, nil
}
func advanceTagSet(tx *gorm.DB, head model.VideoTagSetHead) error {
	return tx.Model(&model.VideoTagSetHead{}).Where("user_id = ? AND task_id = ? AND version = ?", head.UserID, head.TaskID, head.Version).Update("version", head.Version+1).Error
}

func protectTag(tx *gorm.DB, tag *model.UserTag) error {
	if tag.ProtectedByUser {
		return nil
	}
	tag.ProtectedByUser = true
	tag.Version++
	tag.UpdatedAt = time.Now().UTC()
	return tx.Save(tag).Error
}
func clearTagRejections(tx *gorm.DB, owner, taskID int64, tag model.UserTag) error {
	var keys []string
	if err := tx.Model(&model.UserTagName{}).Where("user_id = ? AND tag_id = ?", owner, tag.ID).Pluck("normalized_key", &keys).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ? AND task_id = ? AND (tag_id = ? OR normalized_key IN ?)", owner, taskID, tag.ID, keys).Delete(&model.VideoTagDecision{}).Error
}
func setTagDecision(tx *gorm.DB, owner, taskID int64, tagID, key, decision string, version int64) error {
	identity := "name:" + key
	if tagID != "" {
		identity = "tag:" + tagID
	}
	row := model.VideoTagDecision{UserID: owner, TaskID: taskID, DecisionKey: identity, TagID: tagID, NormalizedKey: key, Decision: decision, Version: version, UpdatedAt: time.Now().UTC()}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "task_id"}, {Name: "decision_key"}}, DoUpdates: clause.AssignmentColumns([]string{"tag_id", "normalized_key", "decision", "version", "updated_at"})}).Create(&row).Error
}
func tagRejected(tx *gorm.DB, owner, taskID int64, tagID, key string) (bool, error) {
	keys := []string{key}
	if tagID != "" {
		if err := tx.Model(&model.UserTagName{}).Where("user_id = ? AND tag_id = ?", owner, tagID).Pluck("normalized_key", &keys).Error; err != nil {
			return false, err
		}
		keys = append(keys, key)
	}
	var count int64
	err := tx.Model(&model.VideoTagDecision{}).Where("user_id = ? AND task_id = ? AND decision = ? AND ((tag_id <> '' AND tag_id = ?) OR normalized_key IN ?)", owner, taskID, "rejected", tagID, keys).Count(&count).Error
	return count > 0, err
}

func (r *UserTagRepository) PatchTask(ctx context.Context, owner, taskID int64, input TagPatch) (TaskTagState, error) {
	var out TaskTagState
	if input.ExpectedVersion < 0 {
		return out, artifact.Err("invalid_request", 400)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, owner); err != nil {
			return err
		}
		if _, err := summaryTask(tx, owner, taskID, true); err != nil {
			return err
		}
		head, err := lockTagSet(tx, owner, taskID, input.ExpectedVersion)
		if err != nil {
			return err
		}
		repo := NewUserTagRepository(tx, r.limits)
		add, err := repo.ResolveIDs(ctx, owner, input.AddIDs)
		if err != nil {
			return err
		}
		remove, err := repo.ResolveIDs(ctx, owner, input.RemoveIDs)
		if err != nil {
			return err
		}
		keep, err := repo.ResolveIDs(ctx, owner, input.KeepAutoIDs)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, group := range [][]string{add, remove, keep} {
			for _, id := range group {
				if seen[id] {
					return artifact.Err("contradictory_tag_patch", 400)
				}
				seen[id] = true
			}
		}
		if len(seen) == 0 {
			return artifact.Err("invalid_request", 400)
		}
		for _, id := range remove {
			tag, err := resolveTag(tx, owner, id)
			if err != nil {
				return err
			}
			if err = tx.Where("user_id = ? AND task_id = ? AND tag_id = ?", owner, taskID, id).Delete(&model.VideoTagAssignment{}).Error; err != nil {
				return err
			}
			_, key, _ := usertags.Normalize(tag.DisplayName, r.limits.NameRunes)
			if err = setTagDecision(tx, owner, taskID, id, key, "rejected", head.Version+1); err != nil {
				return err
			}
		}
		for _, id := range keep {
			var assignment model.VideoTagAssignment
			if err := tx.Where("user_id = ? AND task_id = ? AND tag_id = ? AND origin = ?", owner, taskID, id, "auto").First(&assignment).Error; err != nil {
				return artifact.Err("invalid_tag_assignment", 409)
			}
		}
		for _, id := range append(add, keep...) {
			tag, err := resolveTag(tx, owner, id)
			if err != nil {
				return err
			}
			if err = protectTag(tx, &tag); err != nil {
				return err
			}
			if err = clearTagRejections(tx, owner, taskID, tag); err != nil {
				return err
			}
			if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "task_id"}, {Name: "tag_id"}}, DoUpdates: clause.Assignments(map[string]any{"origin": "manual"})}).Create(&model.VideoTagAssignment{UserID: owner, TaskID: taskID, TagID: id, Origin: "manual", CreatedAt: time.Now().UTC()}).Error; err != nil {
				return err
			}
			_, key, _ := usertags.Normalize(tag.DisplayName, r.limits.NameRunes)
			if err = setTagDecision(tx, owner, taskID, id, key, "accepted", head.Version+1); err != nil {
				return err
			}
		}
		var count int64
		if err = tx.Model(&model.VideoTagAssignment{}).Where("user_id = ? AND task_id = ?", owner, taskID).Count(&count).Error; err != nil {
			return err
		}
		if count > int64(r.limits.Assignments) {
			return artifact.Err("tag_assignment_limit", 400)
		}
		if err = advanceTagSet(tx, head); err != nil {
			return err
		}
		out, err = r.taskState(tx, owner, taskID)
		return err
	})
	return out, err
}
