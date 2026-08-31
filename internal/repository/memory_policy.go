package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/model"
)

var ErrMemoryPolicyVersionConflict = errors.New("memory policy version conflict")

var memoryPolicyLocks [256]sync.Mutex

type MemoryPolicyInputs struct {
	UserEnabled           bool
	UserPreferenceVersion int64
	SessionPolicy         string
	SessionPolicyVersion  int64
}

func (r *MemoryRepository) GetMemoryPreference(ctx context.Context, userID int64) (model.AgentMemoryPreference, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return model.AgentMemoryPreference{}, gorm.ErrInvalidData
	}
	var preference model.AgentMemoryPreference
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&preference).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AgentMemoryPreference{UserID: userID, Enabled: false, Version: 0}, nil
	}
	return preference, err
}

// ResolveMemoryPolicyInputs reads the owner-scoped session and user default in
// one statement so callers never assemble a mixed policy from two snapshots.
func (r *MemoryRepository) ResolveMemoryPolicyInputs(ctx context.Context, userID, sessionID int64) (MemoryPolicyInputs, error) {
	if r == nil || r.db == nil || userID <= 0 || sessionID <= 0 {
		return MemoryPolicyInputs{}, gorm.ErrInvalidData
	}
	var row struct {
		SessionPolicy         string
		SessionPolicyVersion  int64
		UserEnabled           bool
		UserPreferenceVersion int64
	}
	err := r.db.WithContext(ctx).Table("chat_sessions AS cs").
		Select("cs.memory_policy AS session_policy, cs.memory_policy_version AS session_policy_version, "+
			"COALESCE(mp.enabled, ?) AS user_enabled, COALESCE(mp.version, 0) AS user_preference_version", false).
		Joins("LEFT JOIN agent_memory_preferences AS mp ON mp.user_id = cs.user_id").
		Where("cs.id = ? AND cs.user_id = ?", sessionID, userID).
		Take(&row).Error
	if err != nil {
		return MemoryPolicyInputs{}, err
	}
	policy := normalizeSessionMemoryPolicy(row.SessionPolicy)
	if !validSessionMemoryPolicy(policy) {
		return MemoryPolicyInputs{}, gorm.ErrInvalidData
	}
	return MemoryPolicyInputs{
		UserEnabled: row.UserEnabled, UserPreferenceVersion: row.UserPreferenceVersion,
		SessionPolicy: policy, SessionPolicyVersion: row.SessionPolicyVersion,
	}, nil
}

func (r *MemoryRepository) UpdateMemoryPreference(ctx context.Context, userID int64, enabled bool, expectedVersion int64, capabilityEnabled bool) (model.AgentMemoryPreference, error) {
	if r == nil || r.db == nil || userID <= 0 || expectedVersion < 0 {
		return model.AgentMemoryPreference{}, gorm.ErrInvalidData
	}
	lock := memoryPolicyLock(userID)
	lock.Lock()
	defer lock.Unlock()

	var updated model.AgentMemoryPreference
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMemoryPolicyUser(tx, userID); err != nil {
			return err
		}
		var current model.AgentMemoryPreference
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedVersion != 0 {
				return ErrMemoryPolicyVersionConflict
			}
			if !enabled {
				updated = model.AgentMemoryPreference{UserID: userID, Enabled: false, Version: 0}
				return nil
			}
			updated = model.AgentMemoryPreference{UserID: userID, Enabled: true, Version: 1}
			if err := tx.Create(&updated).Error; err != nil {
				return err
			}
			return createMemoryPolicyEvent(tx, memoryPolicyEventRequest{
				UserID: userID, TargetType: model.MemoryPolicyTargetUser, TargetID: strconv.FormatInt(userID, 10),
				PreviousValue: model.MemorySessionPolicyDisabled, NewValue: model.MemorySessionPolicyEnabled, Version: updated.Version,
				CapabilityEnabled: capabilityEnabled, EffectiveBefore: false, EffectiveAfter: capabilityEnabled,
			})
		}
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return ErrMemoryPolicyVersionConflict
		}
		if current.Enabled == enabled {
			updated = current
			return nil
		}
		previous := current.Enabled
		current.Enabled = enabled
		current.Version++
		result := tx.Model(&model.AgentMemoryPreference{}).
			Where("user_id = ? AND version = ?", userID, expectedVersion).
			Updates(map[string]any{"enabled": current.Enabled, "version": current.Version})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMemoryPolicyVersionConflict
		}
		if err := createMemoryPolicyEvent(tx, memoryPolicyEventRequest{
			UserID: userID, TargetType: model.MemoryPolicyTargetUser, TargetID: strconv.FormatInt(userID, 10),
			PreviousValue: enabledPolicyValue(previous), NewValue: enabledPolicyValue(enabled), Version: current.Version,
			CapabilityEnabled: capabilityEnabled, EffectiveBefore: capabilityEnabled && previous, EffectiveAfter: capabilityEnabled && enabled,
		}); err != nil {
			return err
		}
		updated = current
		return nil
	})
	return updated, err
}

func (r *MemoryRepository) UpdateSessionMemoryPolicy(ctx context.Context, userID, sessionID int64, policy string, expectedVersion int64, capabilityEnabled bool) (model.ChatSession, MemoryPolicyInputs, error) {
	policy = normalizeSessionMemoryPolicy(policy)
	if r == nil || r.db == nil || userID <= 0 || sessionID <= 0 || expectedVersion < 0 || !validSessionMemoryPolicy(policy) {
		return model.ChatSession{}, MemoryPolicyInputs{}, gorm.ErrInvalidData
	}
	lock := memoryPolicyLock(userID)
	lock.Lock()
	defer lock.Unlock()

	var updated model.ChatSession
	var resolvedInputs MemoryPolicyInputs
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMemoryPolicyUser(tx, userID); err != nil {
			return err
		}
		var session model.ChatSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
			return err
		}
		currentPolicy := normalizeSessionMemoryPolicy(session.MemoryPolicy)
		if !validSessionMemoryPolicy(currentPolicy) {
			return gorm.ErrInvalidData
		}
		if session.MemoryPolicyVersion != expectedVersion {
			return ErrMemoryPolicyVersionConflict
		}
		preference, err := getMemoryPreferenceInTransaction(tx, userID)
		if err != nil {
			return err
		}
		if currentPolicy == policy {
			session.MemoryPolicy = currentPolicy
			updated = session
			resolvedInputs = MemoryPolicyInputs{
				UserEnabled: preference.Enabled, UserPreferenceVersion: preference.Version,
				SessionPolicy: currentPolicy, SessionPolicyVersion: session.MemoryPolicyVersion,
			}
			return nil
		}
		before := effectiveMemoryEnabled(capabilityEnabled, preference.Enabled, currentPolicy)
		after := effectiveMemoryEnabled(capabilityEnabled, preference.Enabled, policy)
		session.MemoryPolicy = policy
		session.MemoryPolicyVersion++
		result := tx.Model(&model.ChatSession{}).
			Where("id = ? AND user_id = ? AND memory_policy_version = ?", sessionID, userID, expectedVersion).
			Updates(map[string]any{"memory_policy": policy, "memory_policy_version": session.MemoryPolicyVersion})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMemoryPolicyVersionConflict
		}
		if err := createMemoryPolicyEvent(tx, memoryPolicyEventRequest{
			UserID: userID, TargetType: model.MemoryPolicyTargetSession, TargetID: strconv.FormatInt(sessionID, 10),
			PreviousValue: currentPolicy, NewValue: policy, Version: session.MemoryPolicyVersion,
			CapabilityEnabled: capabilityEnabled, EffectiveBefore: before, EffectiveAfter: after,
		}); err != nil {
			return err
		}
		updated = session
		resolvedInputs = MemoryPolicyInputs{
			UserEnabled: preference.Enabled, UserPreferenceVersion: preference.Version,
			SessionPolicy: policy, SessionPolicyVersion: session.MemoryPolicyVersion,
		}
		return nil
	})
	return updated, resolvedInputs, err
}

// AppendCaptured rechecks current authorization and appends the item in the
// same transaction. Policy updates use the same user/session locks, giving a
// deterministic ordering against concurrent opt-out requests.
func (r *MemoryRepository) AppendCaptured(ctx context.Context, sessionID int64, item *model.AgentMemoryItem) (MemoryAppendResult, bool, error) {
	if r == nil || r.db == nil || item == nil || item.UserID <= 0 || sessionID <= 0 {
		return MemoryAppendResult{}, false, gorm.ErrInvalidData
	}
	lock := memoryPolicyLock(item.UserID)
	lock.Lock()
	defer lock.Unlock()

	var appended MemoryAppendResult
	allowed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMemoryPolicyUser(tx, item.UserID); err != nil {
			return err
		}
		var session model.ChatSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", sessionID, item.UserID).First(&session).Error; err != nil {
			return err
		}
		preference, err := getMemoryPreferenceInTransaction(tx, item.UserID)
		if err != nil {
			return err
		}
		policy := normalizeSessionMemoryPolicy(session.MemoryPolicy)
		if !validSessionMemoryPolicy(policy) || !effectiveMemoryEnabled(true, preference.Enabled, policy) {
			return nil
		}
		if err := validateCapturedMemoryScope(tx, &session, item); err != nil {
			return err
		}
		appended, err = NewMemoryRepository(tx).Append(ctx, item)
		if err != nil {
			return err
		}
		allowed = true
		return nil
	})
	return appended, allowed, err
}

func (r *MemoryRepository) ListMemoryPolicyEvents(ctx context.Context, userID int64) ([]model.AgentMemoryPolicyEvent, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, gorm.ErrInvalidData
	}
	var events []model.AgentMemoryPolicyEvent
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("occurred_at ASC, id ASC").Find(&events).Error
	return events, err
}

func memoryPolicyLock(userID int64) *sync.Mutex {
	key := memoryLockHash(fmt.Sprintf("memory-policy-user:%d", userID))
	return &memoryPolicyLocks[key%uint64(len(memoryPolicyLocks))]
}

func lockMemoryPolicyUser(tx *gorm.DB, userID int64) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	key := memoryLockHash(fmt.Sprintf("memory-policy-user:%d", userID))
	return tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(key)).Error
}

func getMemoryPreferenceInTransaction(tx *gorm.DB, userID int64) (model.AgentMemoryPreference, error) {
	var preference model.AgentMemoryPreference
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("user_id = ?", userID).First(&preference).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AgentMemoryPreference{UserID: userID, Enabled: false, Version: 0}, nil
	}
	return preference, err
}

func validateCapturedMemoryScope(tx *gorm.DB, session *model.ChatSession, item *model.AgentMemoryItem) error {
	if session == nil || item == nil || session.UserID != item.UserID {
		return gorm.ErrInvalidData
	}
	scopeID := strings.TrimSpace(item.ScopeID)
	switch item.ScopeType {
	case model.MemoryScopeUser:
		if scopeID != strconv.FormatInt(item.UserID, 10) {
			return gorm.ErrInvalidData
		}
	case model.MemoryScopeVideo:
		if session.ScopeType != model.ChatScopeVideo || scopeID != strconv.FormatInt(session.TaskID, 10) {
			return gorm.ErrInvalidData
		}
	case model.MemoryScopeKnowledgeBase:
		if session.ScopeType != model.ChatScopeKnowledgeBase || scopeID != strconv.FormatInt(session.KnowledgeBaseID, 10) {
			return gorm.ErrInvalidData
		}
	case model.MemoryScopeRun:
		var count int64
		if err := tx.Model(&model.AgentRun{}).
			Where("id = ? AND user_id = ? AND session_id = ?", scopeID, item.UserID, session.ID).
			Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrInvalidData
		}
	default:
		return gorm.ErrInvalidData
	}
	return nil
}

type memoryPolicyEventRequest struct {
	UserID            int64
	TargetType        string
	TargetID          string
	PreviousValue     string
	NewValue          string
	Version           int64
	CapabilityEnabled bool
	EffectiveBefore   bool
	EffectiveAfter    bool
}

func createMemoryPolicyEvent(tx *gorm.DB, request memoryPolicyEventRequest) error {
	return tx.Create(&model.AgentMemoryPolicyEvent{
		ID: uuid.NewString(), UserID: request.UserID, ActorUserID: request.UserID,
		TargetType: request.TargetType, TargetID: request.TargetID,
		PreviousValue: request.PreviousValue, NewValue: request.NewValue, Version: request.Version,
		CapabilityEnabled: request.CapabilityEnabled, EffectiveBefore: request.EffectiveBefore, EffectiveAfter: request.EffectiveAfter,
		OccurredAt: time.Now().UTC(),
	}).Error
}

func normalizeSessionMemoryPolicy(policy string) string {
	policy = strings.TrimSpace(strings.ToLower(policy))
	if policy == "" {
		return model.MemorySessionPolicyInherit
	}
	return policy
}

func validSessionMemoryPolicy(policy string) bool {
	switch policy {
	case model.MemorySessionPolicyInherit, model.MemorySessionPolicyEnabled, model.MemorySessionPolicyDisabled:
		return true
	default:
		return false
	}
}

func effectiveMemoryEnabled(capabilityEnabled, userEnabled bool, sessionPolicy string) bool {
	if !capabilityEnabled {
		return false
	}
	switch normalizeSessionMemoryPolicy(sessionPolicy) {
	case model.MemorySessionPolicyEnabled:
		return true
	case model.MemorySessionPolicyDisabled:
		return false
	default:
		return userEnabled
	}
}

func enabledPolicyValue(enabled bool) string {
	if enabled {
		return model.MemorySessionPolicyEnabled
	}
	return model.MemorySessionPolicyDisabled
}
