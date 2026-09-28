package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func canvasVersion(tx *gorm.DB, owner int64, artifactID, versionID string) (*model.ArtifactVersion, artifact.Body, error) {
	version, err := versionRead(tx, owner, artifactID, versionID)
	if err != nil {
		return nil, artifact.Body{}, err
	}
	var body artifact.Body
	if err = json.Unmarshal([]byte(version.BodyJSON), &body); err != nil {
		return nil, artifact.Body{}, err
	}
	return version, body, nil
}

func (r *ArtifactRepository) CanvasLayout(ctx context.Context, owner int64, artifactID, versionID string, revision int64) (*artifact.CanvasLayoutView, error) {
	if revision < 0 {
		return nil, artifact.Err("invalid_request", 400)
	}
	view := &artifact.CanvasLayoutView{ContentVersionID: versionID, ViewID: "knowledge", Layout: artifact.DefaultCanvasLayout()}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := ownedArtifact(tx, owner, artifactID, false); err != nil {
			return err
		}
		version, body, err := canvasVersion(tx, owner, artifactID, versionID)
		if err != nil {
			return err
		}
		query := tx.Where("user_id=? AND artifact_id=? AND content_version_id=? AND view_id=?", owner, artifactID, versionID, view.ViewID)
		if revision > 0 {
			query = query.Where("revision=?", revision)
		} else {
			query = query.Order("revision DESC")
		}
		var row model.ArtifactCanvasLayout
		if err := query.First(&row).Error; err == nil {
			view.Revision = row.Revision
			return json.Unmarshal([]byte(row.LayoutJSON), &view.Layout)
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		if revision > 0 {
			return artifact.Err("not_found", 404)
		}
		// A new content version inherits only surviving stable block positions.
		var prior model.ArtifactCanvasLayout
		if err := tx.Table("artifact_canvas_layouts AS l").Joins("JOIN artifact_versions AS v ON v.id=l.content_version_id").Where("l.user_id=? AND l.artifact_id=? AND l.view_id=? AND v.version<?", owner, artifactID, view.ViewID, version.Version).Order("v.version DESC, l.revision DESC").Select("l.*").First(&prior).Error; err == nil {
			if err := json.Unmarshal([]byte(prior.LayoutJSON), &view.Layout); err != nil {
				return err
			}
			valid := map[string]bool{}
			for _, block := range body.Blocks {
				valid[block.BlockID] = true
			}
			for id := range view.Layout.Nodes {
				if !valid[id] {
					delete(view.Layout.Nodes, id)
				}
			}
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		return nil
	})
	return view, err
}

func (r *ArtifactRepository) SaveCanvasLayout(ctx context.Context, owner int64, artifactID, versionID string, expected int64, key string, layout artifact.CanvasLayout) (*artifact.CanvasLayoutView, error) {
	if expected < 0 || artifact.ValidateKey(key) != nil {
		return nil, artifact.Err("invalid_request", 400)
	}
	const viewID = "knowledge"
	hash := artifact.Hash(artifact.JSON(struct {
		VersionID string
		Expected  int64
		Layout    artifact.CanvasLayout
	}{versionID, expected, layout}))
	var result *artifact.CanvasLayoutView
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, err := ownedArtifact(tx, owner, artifactID, true)
		if err != nil {
			return err
		}
		_, body, err := canvasVersion(tx, owner, artifactID, versionID)
		if err != nil {
			return err
		}
		if err := layout.Validate(body); err != nil {
			return err
		}
		var replay model.ArtifactCanvasLayout
		err = tx.Where("user_id=? AND artifact_id=? AND idempotency_key=?", owner, artifactID, key).First(&replay).Error
		if err == nil {
			if replay.RequestHash != hash {
				return artifact.Err("idempotency_conflict", 409)
			}
			result = &artifact.CanvasLayoutView{ContentVersionID: replay.ContentVersionID, ViewID: replay.ViewID, Revision: replay.Revision, Layout: layout}
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if a.CurrentVersionID == nil || *a.CurrentVersionID != versionID {
			return artifact.Err("version_conflict", 409)
		}
		var latest model.ArtifactCanvasLayout
		err = tx.Where("user_id=? AND artifact_id=? AND content_version_id=? AND view_id=?", owner, artifactID, versionID, viewID).Order("revision DESC").First(&latest).Error
		current := int64(0)
		if err == nil {
			current = latest.Revision
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		if current != expected {
			return artifact.Err("version_conflict", 409)
		}
		row := model.ArtifactCanvasLayout{ID: uuid.NewString(), UserID: owner, ArtifactID: artifactID, ContentVersionID: versionID, ViewID: viewID, Revision: current + 1, IdempotencyKey: key, RequestHash: hash, LayoutJSON: artifact.JSON(layout)}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		result = &artifact.CanvasLayoutView{ContentVersionID: versionID, ViewID: viewID, Revision: row.Revision, Layout: layout}
		return nil
	})
	return result, err
}
