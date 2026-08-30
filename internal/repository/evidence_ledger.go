package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

type EvidenceLedgerRepository struct {
	db *gorm.DB
}

type EvidenceLedgerBatch struct {
	Claims   []model.AgentClaim
	Evidence []model.AgentEvidence
	Links    []model.AgentClaimEvidence
}

type EvidenceLedgerRecords struct {
	Claims   []model.AgentClaim         `json:"claims"`
	Evidence []model.AgentEvidence      `json:"evidence"`
	Links    []model.AgentClaimEvidence `json:"claim_evidence"`
}

func NewEvidenceLedgerRepository(db *gorm.DB) *EvidenceLedgerRepository {
	return &EvidenceLedgerRepository{db: db}
}

// Append stores one immutable answer-ledger batch. Deterministic IDs make a
// retry of the same completed Agent run idempotent.
func (r *EvidenceLedgerRepository) Append(ctx context.Context, batch EvidenceLedgerBatch) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(batch.Evidence) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&batch.Evidence).Error; err != nil {
				return err
			}
		}
		if len(batch.Claims) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&batch.Claims).Error; err != nil {
				return err
			}
		}
		if len(batch.Links) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&batch.Links).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *EvidenceLedgerRepository) ListRun(ctx context.Context, userID int64, runID string) (EvidenceLedgerRecords, error) {
	runID = strings.TrimSpace(runID)
	if r == nil || r.db == nil || userID <= 0 || runID == "" {
		return EvidenceLedgerRecords{}, gorm.ErrInvalidData
	}
	result := EvidenceLedgerRecords{Claims: []model.AgentClaim{}, Evidence: []model.AgentEvidence{}, Links: []model.AgentClaimEvidence{}}
	if err := r.db.WithContext(ctx).Where("user_id = ? AND run_id = ?", userID, runID).
		Order("root_claim_id ASC, revision ASC, created_at ASC").Find(&result.Claims).Error; err != nil {
		return EvidenceLedgerRecords{}, err
	}
	if err := r.db.WithContext(ctx).Where("user_id = ? AND run_id = ?", userID, runID).
		Order("created_at ASC, id ASC").Find(&result.Evidence).Error; err != nil {
		return EvidenceLedgerRecords{}, err
	}
	if len(result.Claims) == 0 {
		return result, nil
	}
	claimIDs := make([]string, 0, len(result.Claims))
	for _, claim := range result.Claims {
		claimIDs = append(claimIDs, claim.ID)
	}
	if err := r.db.WithContext(ctx).Where("claim_id IN ?", claimIDs).
		Order("created_at ASC, claim_id ASC, evidence_id ASC").Find(&result.Links).Error; err != nil {
		return EvidenceLedgerRecords{}, err
	}
	return result, nil
}

// AppendCorrection preserves the original claim and creates a new terminal
// revision. The latest revision's evidence bindings are copied for auditability.
func (r *EvidenceLedgerRepository) AppendCorrection(ctx context.Context, userID int64, claimID, correctedText, reason string) (*model.AgentClaim, error) {
	claimID, correctedText, reason = strings.TrimSpace(claimID), strings.TrimSpace(correctedText), strings.TrimSpace(reason)
	if r == nil || r.db == nil || userID <= 0 || claimID == "" || correctedText == "" || reason == "" {
		return nil, gorm.ErrInvalidData
	}
	var correction model.AgentClaim
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var selected model.AgentClaim
		if err := tx.Where("id = ? AND user_id = ?", claimID, userID).First(&selected).Error; err != nil {
			return err
		}
		var latest model.AgentClaim
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("root_claim_id = ? AND user_id = ?", selected.RootClaimID, userID).
			Order("revision DESC").First(&latest).Error; err != nil {
			return err
		}
		correction = latest
		correction.ID = uuid.NewString()
		correction.Revision = latest.Revision + 1
		correction.SupersedesClaimID = latest.ID
		correction.Text = correctedText
		correction.Status = model.ClaimStatusCorrected
		if correction.Confidence > 0.5 {
			correction.Confidence = 0.5
		}
		correction.ValidationNote = reason
		correction.CreatedAt = time.Now().UTC()
		if err := tx.Create(&correction).Error; err != nil {
			return err
		}

		var links []model.AgentClaimEvidence
		if err := tx.Where("claim_id = ?", latest.ID).Find(&links).Error; err != nil {
			return err
		}
		for i := range links {
			links[i].ClaimID = correction.ID
			links[i].CreatedAt = correction.CreatedAt
			links[i].Relation = model.ClaimEvidenceContext
			links[i].VerificationStatus = model.ClaimStatusUncertain
			links[i].ValidationReason = "inherited as context from corrected claim revision: " + latest.ID
		}
		if len(links) > 0 {
			return tx.Create(&links).Error
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &correction, nil
}
