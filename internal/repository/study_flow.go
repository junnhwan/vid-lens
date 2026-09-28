package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type LearningPositionView struct {
	model.LearningPosition
	Fallback string `json:"fallback,omitempty"`
}

func (r *ArtifactRepository) LearningPosition(ctx context.Context, owner int64) (*LearningPositionView, error) {
	var row model.LearningPosition
	err := r.db.WithContext(ctx).Where("user_id=?", owner).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	view := &LearningPositionView{LearningPosition: row}
	var task model.VideoTask
	if err := r.db.WithContext(ctx).Where("id=? AND user_id=?", row.TaskID, owner).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if row.ArtifactID == "" {
		return view, nil
	}
	a, v, err := r.Get(ctx, owner, row.ArtifactID)
	if err != nil || v == nil {
		view.ArtifactID, view.VersionID, view.BlockID = "", "", ""
		view.Fallback = "artifact_unavailable"
		return view, nil
	}
	var body artifact.Body
	if err := json.Unmarshal([]byte(v.BodyJSON), &body); err != nil {
		return nil, err
	}
	view.VersionID = v.ID
	if a.CurrentVersionID == nil || *a.CurrentVersionID != row.VersionID {
		view.Fallback = "version_changed"
	}
	for _, block := range body.Blocks {
		if block.BlockID == row.BlockID {
			return view, nil
		}
	}
	view.BlockID = ""
	if len(body.Blocks) > 0 {
		view.BlockID = body.Blocks[0].BlockID
	}
	view.Fallback = "block_removed"
	return view, nil
}

// SaveLearningPosition uses one user-wide CAS revision. The client serializes
// its own writes; stale tabs and delayed requests receive 409.
func (r *ArtifactRepository) SaveLearningPosition(ctx context.Context, owner, expected, taskID int64, artifactID, versionID, blockID string, timeMS int64) (*LearningPositionView, error) {
	if expected < 0 || taskID <= 0 || timeMS < 0 || timeMS > 24*60*60*1000 || (artifactID == "" && (versionID != "" || blockID != "")) || (artifactID != "" && (versionID == "" || blockID == "")) {
		return nil, artifact.Err("invalid_request", 400)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := sourceLock(tx, owner, taskID); err != nil {
			return artifact.Err("not_found", 404)
		}
		if artifactID != "" {
			a, err := ownedArtifact(tx, owner, artifactID, false)
			if err != nil {
				return err
			}
			if a.CurrentVersionID == nil || *a.CurrentVersionID != versionID {
				return artifact.Err("version_conflict", 409)
			}
			v, err := versionRead(tx, owner, artifactID, versionID)
			if err != nil {
				return err
			}
			var m model.SourceManifest
			if err := tx.Where("id=? AND source_id=?", v.ManifestID, taskID).First(&m).Error; err != nil {
				return artifact.Err("invalid_request", 400)
			}
			var body artifact.Body
			if err := json.Unmarshal([]byte(v.BodyJSON), &body); err != nil {
				return err
			}
			found := false
			for _, b := range body.Blocks {
				if b.BlockID == blockID {
					found = true
					break
				}
			}
			if !found {
				return artifact.Err("block_removed", 409)
			}
		}
		var row model.LearningPosition
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=?", owner).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expected != 0 {
				return artifact.Err("position_conflict", 409)
			}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.LearningPosition{UserID: owner, Revision: 1, TaskID: taskID, ArtifactID: artifactID, VersionID: versionID, BlockID: blockID, TimeMS: timeMS})
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected == 0 {
				return artifact.Err("position_conflict", 409)
			}
			return nil
		}
		if err != nil {
			return err
		}
		if row.Revision != expected {
			// A deleted or inaccessible video is intentionally hidden by
			// LearningPosition, so a fresh client sees no position and sends 0.
			// Only replace that unavailable target; keep the revision increasing
			// so a delayed write from before deletion still cannot win later.
			if expected != 0 {
				return artifact.Err("position_conflict", 409)
			}
			var available int64
			if err := tx.Model(&model.VideoTask{}).Where("id=? AND user_id=?", row.TaskID, owner).Count(&available).Error; err != nil {
				return err
			}
			if available != 0 {
				return artifact.Err("position_conflict", 409)
			}
		}
		return tx.Model(&row).Updates(map[string]any{"revision": row.Revision + 1, "task_id": taskID, "artifact_id": artifactID, "version_id": versionID, "block_id": blockID, "time_ms": timeMS, "updated_at": time.Now().UTC()}).Error
	})
	if err != nil {
		return nil, err
	}
	return r.LearningPosition(ctx, owner)
}

type StudyBlockContext struct {
	ArtifactID string         `json:"artifact_id"`
	VersionID  string         `json:"version_id"`
	TaskID     int64          `json:"task_id"`
	Block      artifact.Block `json:"block"`
}

func (r *ArtifactRepository) BlockContext(ctx context.Context, owner int64, artifactID, versionID, blockID string) (*StudyBlockContext, error) {
	v, err := r.Version(ctx, owner, artifactID, versionID)
	if err != nil {
		return nil, err
	}
	m, _, err := r.Snapshot(ctx, owner, v.ManifestID)
	if err != nil {
		return nil, err
	}
	var body artifact.Body
	if err := json.Unmarshal([]byte(v.BodyJSON), &body); err != nil {
		return nil, err
	}
	for _, b := range body.Blocks {
		if b.BlockID == blockID {
			return &StudyBlockContext{artifactID, versionID, m.SourceID, b}, nil
		}
	}
	return nil, artifact.Err("not_found", 404)
}

type answerCitation struct {
	CitationID string `json:"citation_id"`
	TaskID     int64  `json:"task_id"`
	SourceRefs []struct {
		SourceType  string `json:"source_type"`
		StableID    string `json:"stable_id"`
		ContentHash string `json:"content_hash"`
	} `json:"source_refs"`
}
type AnswerPreview struct {
	MessageID    int64          `json:"message_id"`
	Content      string         `json:"content"`
	AfterBlockID string         `json:"after_block_id"`
	Mapped       []artifact.Ref `json:"mapped"`
	Unmapped     []string       `json:"unmapped"`
	VersionID    string         `json:"version_id"`
}

func answerPreview(tx *gorm.DB, owner, messageID int64, artifactID, afterBlockID string, expected int64) (*AnswerPreview, *model.Artifact, *model.ArtifactVersion, artifact.Body, error) {
	a, err := ownedArtifact(tx, owner, artifactID, false)
	if err != nil {
		return nil, nil, nil, artifact.Body{}, err
	}
	if a.CurrentVersionID == nil || a.HeadVersion != expected {
		return nil, nil, nil, artifact.Body{}, artifact.Err("version_conflict", 409)
	}
	v, err := versionRead(tx, owner, artifactID, *a.CurrentVersionID)
	if err != nil {
		return nil, nil, nil, artifact.Body{}, err
	}
	m, err := readableManifest(tx, owner, v.ManifestID)
	if err != nil {
		return nil, nil, nil, artifact.Body{}, err
	}
	var body artifact.Body
	if err := json.Unmarshal([]byte(v.BodyJSON), &body); err != nil {
		return nil, nil, nil, body, err
	}
	found := false
	for _, block := range body.Blocks {
		if block.BlockID == afterBlockID {
			found = true
			break
		}
	}
	if !found {
		return nil, nil, nil, body, artifact.Err("block_removed", 409)
	}
	var message model.ChatMessage
	if err := tx.Where("id=? AND user_id=? AND role='assistant'", messageID, owner).First(&message).Error; err != nil {
		return nil, nil, nil, body, hideMissing(err)
	}
	if strings.TrimSpace(message.Content) == "" || message.RetrievalSnapshot == nil {
		return nil, nil, nil, body, artifact.Err("answer_incomplete", 422)
	}
	var session model.ChatSession
	if err := tx.Where("id=? AND user_id=? AND scope_type=? AND task_id=?", message.SessionID, owner, model.ChatScopeVideo, m.SourceID).First(&session).Error; err != nil {
		return nil, nil, nil, body, artifact.Err("answer_scope_mismatch", 422)
	}
	var envelope struct {
		Citations []answerCitation `json:"citations"`
		RunID     string           `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(*message.RetrievalSnapshot), &envelope); err != nil {
		return nil, nil, nil, body, artifact.Err("answer_incomplete", 422)
	}
	if envelope.RunID != "" {
		var run model.AgentRun
		if err := tx.Where("id=? AND user_id=? AND status='completed'", envelope.RunID, owner).First(&run).Error; err != nil {
			return nil, nil, nil, body, artifact.Err("answer_incomplete", 422)
		}
	}
	var items []model.SourceSnapshotItem
	if err := tx.Where("manifest_id=?", v.ManifestID).Find(&items).Error; err != nil {
		return nil, nil, nil, body, err
	}
	byIdentity := map[string]string{}
	for _, item := range items {
		byIdentity[item.Modality+"\x00"+item.SourceIdentity+"\x00"+item.ContentHash] = item.ID
	}
	preview := &AnswerPreview{MessageID: messageID, Content: message.Content, AfterBlockID: afterBlockID, Mapped: []artifact.Ref{}, Unmapped: []string{}, VersionID: v.ID}
	if len(envelope.Citations) == 0 {
		preview.Unmapped = append(preview.Unmapped, "回答没有可核对的聊天引用")
	}
	seen := map[string]bool{}
	for i, citation := range envelope.Citations {
		label := citation.CitationID
		if label == "" {
			label = "citation-" + artifact.JSON(i+1)
		}
		mapped := len(citation.SourceRefs) > 0
		if citation.TaskID == m.SourceID {
			for _, ref := range citation.SourceRefs {
				if ref.StableID == "" || ref.ContentHash == "" || ref.SourceType == "" {
					mapped = false
					continue
				}
				id := byIdentity[ref.SourceType+"\x00"+ref.SourceType+":"+ref.StableID+"\x00"+ref.ContentHash]
				if id != "" {
					if !seen[id+"\x00"+label] {
						preview.Mapped = append(preview.Mapped, artifact.Ref{EvidenceID: id, Relation: "context", CitationID: label})
						seen[id+"\x00"+label] = true
					}
				} else {
					mapped = false
				}
			}
		} else {
			mapped = false
		}
		if !mapped {
			preview.Unmapped = append(preview.Unmapped, label)
		}
	}
	return preview, a, v, body, nil
}

func (r *ArtifactRepository) PreviewAnswer(ctx context.Context, owner, messageID int64, artifactID, afterBlockID string, expected int64) (*AnswerPreview, error) {
	var preview *AnswerPreview
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		preview, _, _, _, err = answerPreview(tx, owner, messageID, artifactID, afterBlockID, expected)
		return err
	})
	return preview, err
}

func (r *ArtifactRepository) ImportAnswer(ctx context.Context, owner, messageID int64, artifactID, afterBlockID string, expected int64, key string, personal bool) (string, error) {
	if err := artifact.ValidateKey(key); err != nil {
		return "", err
	}
	hash := artifact.Hash(artifact.JSON([]any{artifactID, afterBlockID, messageID, expected, personal}))
	// Discover the source before the transaction so all write paths lock the
	// video row before the artifact row, including deletion and ordinary edits.
	_, currentVersion, lookupErr := r.Get(ctx, owner, artifactID)
	if lookupErr != nil {
		return "", lookupErr
	}
	if currentVersion == nil {
		return "", artifact.Err("version_conflict", 409)
	}
	var source model.SourceManifest
	if lookupErr = r.db.WithContext(ctx).Where("id=? AND user_id=?", currentVersion.ManifestID, owner).First(&source).Error; lookupErr != nil {
		return "", hideMissing(lookupErr)
	}
	result := ""
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old model.AnswerImport
		err := tx.Where("user_id=? AND key=?", owner, key).First(&old).Error
		if err == nil {
			if old.RequestHash != hash {
				return artifact.Err("idempotency_conflict", 409)
			}
			result = old.VersionID
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if _, err := sourceLock(tx, owner, source.SourceID); err != nil {
			return err
		}
		// Lock the artifact before checking its head and recording the key.
		if _, err := ownedArtifact(tx, owner, artifactID, true); err != nil {
			return err
		}
		preview, a, v, body, err := answerPreview(tx, owner, messageID, artifactID, afterBlockID, expected)
		if err != nil {
			return err
		}
		if len(preview.Unmapped) > 0 && !personal {
			return artifact.Err("citations_unmapped", 422)
		}
		if personal && len(preview.Unmapped) == 0 {
			return artifact.Err("invalid_request", 400)
		}
		if len(body.Blocks) >= 200 {
			return artifact.Err("invalid_request", 400)
		}
		refs := preview.Mapped
		if personal {
			refs = []artifact.Ref{}
		}
		content := preview.Content
		if utf8.RuneCountInString(content) > 8000 {
			return artifact.Err("answer_too_long", 422)
		}
		block := artifact.Block{BlockID: uuid.NewString(), Type: "note", Title: "问答补充 · 待核对", Content: content, ClaimOrigin: "user", EvidenceRefs: refs}
		index := 0
		for i, b := range body.Blocks {
			if b.BlockID == afterBlockID {
				block.ParentID = b.ParentID
				index = i + 1
				break
			}
		}
		parents := map[string]*string{}
		for _, b := range body.Blocks {
			parents[b.BlockID] = b.ParentID
		}
		for index < len(body.Blocks) {
			parent := body.Blocks[index].ParentID
			descendant := false
			for parent != nil {
				if *parent == afterBlockID {
					descendant = true
					break
				}
				parent = parents[*parent]
			}
			if !descendant {
				break
			}
			index++
		}
		body.Blocks = append(body.Blocks, artifact.Block{})
		copy(body.Blocks[index+1:], body.Blocks[index:])
		body.Blocks[index] = block
		body.Warnings = append(body.Warnings, "human_edited_unverified")
		allowed, err := allowedEvidence(tx, v.ManifestID)
		if err != nil {
			return err
		}
		if err = body.Validate(allowed); err != nil {
			return err
		}
		revision, err := insertRevision(tx, a, body, v.ManifestID, "user", expected, nil, true)
		if err != nil {
			return err
		}
		result = revision.ID
		return tx.Create(&model.AnswerImport{UserID: owner, Key: key, RequestHash: hash, ArtifactID: artifactID, VersionID: result}).Error
	})
	return result, err
}
