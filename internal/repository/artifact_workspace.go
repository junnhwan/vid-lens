package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type ArtifactRepository struct{ db *gorm.DB }

func NewArtifactRepository(db *gorm.DB) *ArtifactRepository { return &ArtifactRepository{db} }

type SourceReader func(context.Context, *Repositories, int64, int64) (string, []model.SourceSnapshotItem, error)

func sourceLock(tx *gorm.DB, owner, id int64) (*model.VideoTask, error) {
	var t model.VideoTask
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, owner).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, artifact.Err("source_deleted", 410)
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
func (r *ArtifactRepository) Freeze(ctx context.Context, owner, id int64, read SourceReader) (*model.SourceManifest, []model.SourceSnapshotItem, error) {
	var manifest model.SourceManifest
	var items []model.SourceSnapshotItem
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := sourceLock(tx, owner, id)
		if err != nil {
			if e, ok := err.(*artifact.Error); ok && e.Status == 410 {
				return artifact.Err("not_found", 404)
			}
			return err
		}
		if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
			return artifact.Err("source_not_ready", 422)
		}
		hash, rows, err := read(ctx, NewRepositories(tx), owner, id)
		if err != nil {
			return err
		}
		err = tx.Where("user_id=? AND source_id=? AND content_hash=?", owner, id, hash).First(&manifest).Error
		if err == nil {
			return tx.Where("manifest_id=?", manifest.ID).Order("position").Find(&items).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		title := task.Title
		if title == "" {
			title = task.Filename
		}
		manifest = model.SourceManifest{ID: uuid.NewString(), UserID: owner, SourceID: id, ContentHash: hash, Title: title}
		if err = tx.Create(&manifest).Error; err != nil {
			return err
		}
		for i := range rows {
			rows[i].ID = uuid.NewString()
			rows[i].ManifestID = manifest.ID
			rows[i].SourceID = id
			rows[i].SourceTitle = title
			rows[i].Position = i
		}
		items = rows
		return tx.Create(&items).Error
	})
	return &manifest, items, err
}
func readableManifest(tx *gorm.DB, owner int64, id string) (*model.SourceManifest, error) {
	var m model.SourceManifest
	if err := tx.Where("id=? AND user_id=?", id, owner).First(&m).Error; err != nil {
		return nil, hideMissing(err)
	}
	if m.RevokedAt != nil {
		return nil, artifact.Err("source_deleted", 410)
	}
	var count int64
	if err := tx.Model(&model.VideoTask{}).Where("id=? AND user_id=?", m.SourceID, owner).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, artifact.Err("source_deleted", 410)
	}
	return &m, nil
}
func hideMissing(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return artifact.Err("not_found", 404)
	}
	return err
}
func (r *ArtifactRepository) Evidence(ctx context.Context, owner int64, manifest, id string) (*model.SourceSnapshotItem, error) {
	var item model.SourceSnapshotItem
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := readableManifest(tx, owner, manifest); err != nil {
			return err
		}
		return hideMissing(tx.Where("manifest_id=? AND id=?", manifest, id).First(&item).Error)
	})
	return &item, err
}
func (r *ArtifactRepository) Snapshot(ctx context.Context, owner int64, id string) (*model.SourceManifest, []model.SourceSnapshotItem, error) {
	var m *model.SourceManifest
	rows := []model.SourceSnapshotItem{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		m, err = readableManifest(tx, owner, id)
		if err != nil {
			return err
		}
		return tx.Where("manifest_id=?", id).Order("position").Find(&rows).Error
	})
	return m, rows, err
}
func allowedEvidence(tx *gorm.DB, manifest string) (map[string]bool, error) {
	var ids []string
	err := tx.Model(&model.SourceSnapshotItem{}).Where("manifest_id=?", manifest).Pluck("id", &ids).Error
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}
func (r *ArtifactRepository) List(ctx context.Context, owner, source int64, page, size int) ([]model.Artifact, int64, error) {
	rows := []model.Artifact{}
	var total int64
	q := r.db.WithContext(ctx).Model(&model.Artifact{}).Where("user_id=?", owner)
	if source > 0 {
		manifests := r.db.Model(&model.SourceManifest{}).Select("id").Where("source_id=? AND user_id=?", source, owner)
		q = q.Where("(id IN (?) OR id IN (?))", r.db.Model(&model.ArtifactVersion{}).Select("artifact_id").Where("manifest_id IN (?)", manifests), r.db.Model(&model.GenerationRequest{}).Select("artifact_id").Where("user_id=? AND manifest_id IN (?)", owner, manifests))
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("updated_at DESC, id").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// LatestRunIDs resolves each artifact's newest submitted attempt from durable
// generation requests. Task pagination and run update time do not affect order.
func (r *ArtifactRepository) LatestRunIDs(ctx context.Context, owner int64, artifactIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(artifactIDs))
	if len(artifactIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ArtifactID string
		RunID      string
	}
	err := r.db.WithContext(ctx).Table("generation_requests AS gr").
		Select("gr.artifact_id, gr.run_id").
		Joins("JOIN agent_runs AS ar ON ar.id = gr.run_id AND ar.user_id = gr.user_id AND ar.subject_kind = 'generation_request'").
		Joins("JOIN artifacts AS a ON a.id = gr.artifact_id AND a.user_id = gr.user_id").
		Where("gr.user_id = ? AND gr.artifact_id IN ?", owner, artifactIDs).
		Order("ar.created_at DESC, ar.id DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, exists := out[row.ArtifactID]; !exists {
			out[row.ArtifactID] = row.RunID
		}
	}
	return out, nil
}
func ownedArtifact(tx *gorm.DB, owner int64, id string, lock bool) (*model.Artifact, error) {
	var a model.Artifact
	if lock {
		tx = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := tx.Where("id=? AND user_id=?", id, owner).First(&a).Error
	return &a, hideMissing(err)
}
func (r *ArtifactRepository) Get(ctx context.Context, owner int64, id string) (*model.Artifact, *model.ArtifactVersion, error) {
	var a *model.Artifact
	var v *model.ArtifactVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		a, err = ownedArtifact(tx, owner, id, false)
		if err != nil {
			return err
		}
		if a.CurrentVersionID == nil {
			return nil
		}
		v, err = versionRead(tx, owner, id, *a.CurrentVersionID)
		return err
	})
	return a, v, err
}
func versionRead(tx *gorm.DB, owner int64, id, vid string) (*model.ArtifactVersion, error) {
	var v model.ArtifactVersion
	if err := tx.Where("id=? AND artifact_id=?", vid, id).First(&v).Error; err != nil {
		return nil, hideMissing(err)
	}
	if _, err := readableManifest(tx, owner, v.ManifestID); err != nil {
		return nil, err
	}
	return &v, nil
}
func (r *ArtifactRepository) Version(ctx context.Context, owner int64, id, vid string) (*model.ArtifactVersion, error) {
	if _, err := ownedArtifact(r.db.WithContext(ctx), owner, id, false); err != nil {
		return nil, err
	}
	return versionRead(r.db.WithContext(ctx), owner, id, vid)
}
func (r *ArtifactRepository) Versions(ctx context.Context, owner int64, id string) ([]model.ArtifactVersion, error) {
	if _, err := ownedArtifact(r.db.WithContext(ctx), owner, id, false); err != nil {
		return nil, err
	}
	rows := []model.ArtifactVersion{}
	err := r.db.WithContext(ctx).Omit("body_json").Where("artifact_id=?", id).Order("version DESC").Find(&rows).Error
	return rows, err
}
func insertRevision(tx *gorm.DB, a *model.Artifact, body artifact.Body, manifest, origin string, base int64, run *string, adopt bool, adoptedFrom ...*string) (*model.ArtifactVersion, error) {
	var n int64
	if err := tx.Model(&model.ArtifactVersion{}).Where("artifact_id=?", a.ID).Select("COALESCE(MAX(version),0)").Scan(&n).Error; err != nil {
		return nil, err
	}
	v := &model.ArtifactVersion{ID: uuid.NewString(), ArtifactID: a.ID, Version: n + 1, BaseVersion: base, Origin: origin, RunID: run, OutputRole: "study", ManifestID: manifest, BodyJSON: artifact.JSON(body), Quality: "needs_review"}
	v.WasCandidate = origin == "generated" && !adopt
	if len(adoptedFrom) > 0 {
		v.AdoptedFromVersionID = adoptedFrom[0]
	}
	if err := tx.Create(v).Error; err != nil {
		return nil, err
	}
	refs := []model.ArtifactEvidenceRef{}
	for _, b := range body.Blocks {
		for _, ref := range b.EvidenceRefs {
			refs = append(refs, model.ArtifactEvidenceRef{VersionID: v.ID, BlockID: b.BlockID, EvidenceID: ref.EvidenceID, Relation: ref.Relation})
		}
	}
	if len(refs) > 0 {
		if err := tx.Create(&refs).Error; err != nil {
			return nil, err
		}
	}
	if adopt {
		a.HeadVersion = v.Version
		a.CurrentVersionID = &v.ID
		a.Title = body.Title
		if err := tx.Model(a).Updates(map[string]any{"head_version": v.Version, "current_version_id": v.ID, "title": body.Title, "updated_at": time.Now().UTC()}).Error; err != nil {
			return nil, err
		}
	}
	return v, nil
}
func (r *ArtifactRepository) Create(ctx context.Context, owner int64, manifest string, body artifact.Body) (*model.Artifact, error) {
	a := &model.Artifact{ID: uuid.NewString(), UserID: owner, Kind: "study", Title: body.Title}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := readableManifest(tx, owner, manifest)
		if err != nil {
			return err
		}
		if _, err = sourceLock(tx, owner, m.SourceID); err != nil {
			return err
		}
		allowed, err := allowedEvidence(tx, manifest)
		if err != nil {
			return err
		}
		if err = body.Validate(allowed); err != nil {
			return err
		}
		artifact.StampEdits(&body, nil)
		if err = tx.Create(a).Error; err != nil {
			return err
		}
		_, err = insertRevision(tx, a, body, manifest, "user", 0, nil, true)
		return err
	})
	return a, err
}
func (r *ArtifactRepository) Save(ctx context.Context, owner int64, id string, expected int64, body *artifact.Body, adoptID string) error {
	// Source lock always precedes artifact and run locks, matching source deletion.
	current, _, err := r.Get(ctx, owner, id)
	if err != nil {
		return err
	}
	if current.CurrentVersionID == nil {
		return artifact.Err("version_conflict", 409)
	}
	vid := *current.CurrentVersionID
	if adoptID != "" {
		vid = adoptID
	}
	sourceVersion, err := r.Version(ctx, owner, id, vid)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := readableManifest(tx, owner, sourceVersion.ManifestID)
		if err != nil {
			return err
		}
		if _, err = sourceLock(tx, owner, m.SourceID); err != nil {
			return err
		}
		a, err := ownedArtifact(tx, owner, id, true)
		if err != nil {
			return err
		}
		if a.HeadVersion != expected {
			return artifact.Err("version_conflict", 409)
		}
		if body == nil {
			body = &artifact.Body{}
			if err = json.Unmarshal([]byte(sourceVersion.BodyJSON), body); err != nil {
				return err
			}
		}
		allowed, err := allowedEvidence(tx, m.ID)
		if err != nil {
			return err
		}
		if err = body.Validate(allowed); err != nil {
			return err
		}
		var previous artifact.Body
		if err = json.Unmarshal([]byte(sourceVersion.BodyJSON), &previous); err != nil {
			return err
		}
		artifact.StampEdits(body, &previous)
		var adoptedFrom *string
		if adoptID != "" {
			adoptedFrom = &adoptID
		}
		_, err = insertRevision(tx, a, *body, m.ID, "user", expected, nil, true, adoptedFrom)
		return err
	})
}

func (r *ArtifactRepository) ResultCandidate(ctx context.Context, versionID string) (bool, error) {
	var v model.ArtifactVersion
	if err := r.db.WithContext(ctx).Select("id,was_candidate").Where("id=?", versionID).First(&v).Error; err != nil {
		return false, err
	}
	if !v.WasCandidate {
		return false, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&model.ArtifactVersion{}).Where("adopted_from_version_id=?", versionID).Count(&n).Error
	return n == 0, err
}

// Called inside the existing source deletion transaction after locking the video row.
func (r *ArtifactRepository) RevokeSource(taskID int64) error {
	if !r.db.Migrator().HasTable(&model.SourceManifest{}) {
		return nil
	} // partial legacy test schemas
	now := time.Now().UTC()
	if err := r.db.Model(&model.SourceManifest{}).Where("source_id=?", taskID).Update("revoked_at", now).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.SourceSnapshotItem{}).Where("source_id=?", taskID).Update("content", "").Error; err != nil {
		return err
	}
	var runs []model.AgentRun
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subject_kind='generation_request' AND task_id=?", taskID).Find(&runs).Error; err != nil {
		return err
	}
	for i := range runs {
		run := &runs[i]
		if active(run.Status) {
			if err := r.db.Model(run).Update("cancel_requested_at", now).Error; err != nil {
				return err
			}
			if err := appendEvent(r.db, run, "run.cancel_requested", map[string]any{"reason": "source_deleted"}); err != nil {
				return err
			}
		}
		if err := r.db.Model(&model.AgentStep{}).Where("run_id=?", run.ID).Update("result_checkpoint", "").Error; err != nil {
			return err
		}
		if err := r.db.Model(&model.AgentToolCall{}).Where("run_id=?", run.ID).Update("result_checkpoint", "").Error; err != nil {
			return err
		}
	}
	return nil
}
