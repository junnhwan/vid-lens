package repository

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

var (
	ErrUsageLedgerTerminal = errors.New("usage ledger is already terminal")
	ErrUsageReplayConflict = errors.New("usage ledger replay conflicts with persisted data")
)

type UsageLedgerRepository struct{ db *gorm.DB }

func NewUsageLedgerRepository(db *gorm.DB) *UsageLedgerRepository {
	return &UsageLedgerRepository{db: db}
}

type UsageReservation struct {
	IdempotencyKey                                    string
	UserID, TaskID, JobID                             int64
	Kind, Operation, Provider, Model, Unit, UsageDate string
	ReservedUnits                                     float64
	ExpiresAt, Now                                    time.Time
}
type UsageSettlement struct {
	ActualUnits                                            *float64
	PromptTokens, CompletionTokens, TotalTokens            *int64
	ASRSeconds, EstimatedCost                              *float64
	UsageSource, Currency, PriceVersion, ProviderRequestID string
	Now                                                    time.Time
}

func validateReservation(x UsageReservation) error {
	if strings.TrimSpace(x.IdempotencyKey) == "" || x.UserID <= 0 || strings.TrimSpace(x.Kind) == "" || strings.TrimSpace(x.Unit) == "" || strings.TrimSpace(x.UsageDate) == "" || x.ReservedUnits < 0 || x.ExpiresAt.IsZero() {
		return fmt.Errorf("invalid usage reservation")
	}
	return nil
}
func sameReservation(a model.AIUsageLedger, b UsageReservation) bool {
	return a.UserID == b.UserID && a.TaskID == b.TaskID && a.JobID == b.JobID && a.Kind == strings.TrimSpace(b.Kind) && a.Operation == strings.TrimSpace(b.Operation) && a.Provider == strings.TrimSpace(b.Provider) && a.ModelName == strings.TrimSpace(b.Model) && a.Unit == strings.TrimSpace(b.Unit) && a.UsageDate == strings.TrimSpace(b.UsageDate) && almostEqual(a.ReservedUnits, b.ReservedUnits)
}
func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func (r *UsageLedgerRepository) Reserve(req UsageReservation) (*model.AIUsageLedger, bool, *model.QuotaCompensation, error) {
	if err := validateReservation(req); err != nil {
		return nil, false, nil, err
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	var ledger model.AIUsageLedger
	var created bool
	var event *model.QuotaCompensation
	err := r.db.Transaction(func(tx *gorm.DB) error {
		ledger = model.AIUsageLedger{IdempotencyKey: strings.TrimSpace(req.IdempotencyKey), UserID: req.UserID, TaskID: req.TaskID, JobID: req.JobID, Kind: strings.TrimSpace(req.Kind), Operation: strings.TrimSpace(req.Operation), Provider: strings.TrimSpace(req.Provider), ModelName: strings.TrimSpace(req.Model), UsageDate: strings.TrimSpace(req.UsageDate), Unit: strings.TrimSpace(req.Unit), Status: model.UsageLedgerReserved, ReservedUnits: req.ReservedUnits, UsageSource: model.UsageSourceUnknown, ReservedAt: req.Now, ExpiresAt: req.ExpiresAt, CreatedAt: req.Now, UpdatedAt: req.Now}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ledger)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := tx.First(&ledger, "idempotency_key = ?", req.IdempotencyKey).Error; err != nil {
				return err
			}
			if !sameReservation(ledger, req) {
				return ErrUsageReplayConflict
			}
			return nil
		}
		created = true
		e := newCompensation(ledger, "reserve", req.ReservedUnits, req.Now)
		if err := tx.Create(&e).Error; err != nil {
			return err
		}
		event = &e
		return nil
	})
	return &ledger, created, event, err
}

func (r *UsageLedgerRepository) GetByIdempotencyKey(key string) (*model.AIUsageLedger, error) {
	var row model.AIUsageLedger
	if err := r.db.First(&row, "idempotency_key = ?", strings.TrimSpace(key)).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *UsageLedgerRepository) Settle(key string, s UsageSettlement) (*model.AIUsageLedger, bool, *model.QuotaCompensation, error) {
	if s.Now.IsZero() {
		s.Now = time.Now()
	}
	if s.ActualUnits != nil && *s.ActualUnits < 0 {
		return nil, false, nil, fmt.Errorf("actual usage cannot be negative")
	}
	if s.UsageSource == "" {
		s.UsageSource = model.UsageSourceUnknown
	}
	if s.UsageSource != model.UsageSourceActual && s.UsageSource != model.UsageSourceEstimated && s.UsageSource != model.UsageSourceUnknown {
		return nil, false, nil, fmt.Errorf("invalid usage source")
	}
	if s.UsageSource == model.UsageSourceUnknown {
		s.ActualUnits = nil
		s.PromptTokens = nil
		s.CompletionTokens = nil
		s.TotalTokens = nil
		s.ASRSeconds = nil
		s.EstimatedCost = nil
		s.Currency = ""
		s.PriceVersion = ""
	}
	var ledger model.AIUsageLedger
	var changed bool
	var event *model.QuotaCompensation
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ledger, "idempotency_key = ?", strings.TrimSpace(key)).Error; err != nil {
			return err
		}
		if ledger.Status == model.UsageLedgerSettled {
			if !settlementMatches(ledger, s) {
				return ErrUsageReplayConflict
			}
			return nil
		}
		if ledger.Status != model.UsageLedgerReserved {
			return ErrUsageLedgerTerminal
		}
		updates := map[string]any{"status": model.UsageLedgerSettled, "actual_units": s.ActualUnits, "usage_source": s.UsageSource, "prompt_tokens": s.PromptTokens, "completion_tokens": s.CompletionTokens, "total_tokens": s.TotalTokens, "asr_seconds": s.ASRSeconds, "estimated_cost": s.EstimatedCost, "currency": strings.TrimSpace(s.Currency), "price_version": strings.TrimSpace(s.PriceVersion), "provider_request_id": strings.TrimSpace(s.ProviderRequestID), "settled_at": s.Now, "updated_at": s.Now}
		if err := tx.Model(&model.AIUsageLedger{}).Where("id = ? AND status = ?", ledger.ID, model.UsageLedgerReserved).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&ledger, ledger.ID).Error; err != nil {
			return err
		}
		delta := 0.0
		if s.ActualUnits != nil {
			delta = *s.ActualUnits - ledger.ReservedUnits
		}
		e := newCompensation(ledger, "settle", delta, s.Now)
		if err := tx.Create(&e).Error; err != nil {
			return err
		}
		event = &e
		changed = true
		return nil
	})
	return &ledger, changed, event, err
}
func settlementMatches(a model.AIUsageLedger, s UsageSettlement) bool {
	return floatPointersEqual(a.ActualUnits, s.ActualUnits) && intPointersEqual(a.PromptTokens, s.PromptTokens) && intPointersEqual(a.CompletionTokens, s.CompletionTokens) && intPointersEqual(a.TotalTokens, s.TotalTokens) && floatPointersEqual(a.ASRSeconds, s.ASRSeconds) && floatPointersEqual(a.EstimatedCost, s.EstimatedCost) && a.UsageSource == s.UsageSource && a.Currency == strings.TrimSpace(s.Currency) && a.PriceVersion == strings.TrimSpace(s.PriceVersion) && a.ProviderRequestID == strings.TrimSpace(s.ProviderRequestID)
}
func floatPointersEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return almostEqual(*a, *b)
}
func intPointersEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (r *UsageLedgerRepository) Release(key, reason string, now time.Time) (*model.AIUsageLedger, bool, *model.QuotaCompensation, error) {
	if now.IsZero() {
		now = time.Now()
	}
	var ledger model.AIUsageLedger
	var changed bool
	var event *model.QuotaCompensation
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ledger, "idempotency_key = ?", strings.TrimSpace(key)).Error; err != nil {
			return err
		}
		if ledger.Status == model.UsageLedgerReleased {
			return nil
		}
		if ledger.Status != model.UsageLedgerReserved {
			return ErrUsageLedgerTerminal
		}
		if err := tx.Model(&model.AIUsageLedger{}).Where("id = ? AND status = ?", ledger.ID, model.UsageLedgerReserved).Updates(map[string]any{"status": model.UsageLedgerReleased, "release_reason": truncateGovernanceError(reason), "released_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.First(&ledger, ledger.ID).Error; err != nil {
			return err
		}
		e := newCompensation(ledger, "release", -ledger.ReservedUnits, now)
		if err := tx.Create(&e).Error; err != nil {
			return err
		}
		event = &e
		changed = true
		return nil
	})
	return &ledger, changed, event, err
}
func newCompensation(l model.AIUsageLedger, action string, delta float64, now time.Time) model.QuotaCompensation {
	return model.QuotaCompensation{EventKey: "usage:" + l.IdempotencyKey + ":" + action, LedgerID: l.ID, UserID: l.UserID, UsageDate: l.UsageDate, Kind: l.Kind, Unit: l.Unit, Action: action, DeltaUnits: delta, Status: model.CompensationPending, CreatedAt: now, UpdatedAt: now}
}
func truncateGovernanceError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500]
	}
	return s
}
