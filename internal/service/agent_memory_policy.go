package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

var ErrMemoryPolicyVersionConflict = repository.ErrMemoryPolicyVersionConflict

type MemoryPreferenceView struct {
	Enabled           bool   `json:"enabled"`
	Version           int64  `json:"version"`
	CapabilityEnabled bool   `json:"capability_enabled"`
	EffectiveEnabled  bool   `json:"effective_enabled"`
	Reason            string `json:"reason"`
}

type SessionMemoryPolicyView struct {
	SessionID             int64                       `json:"session_id"`
	Policy                string                      `json:"policy"`
	Version               int64                       `json:"version"`
	EffectiveMemoryPolicy model.EffectiveMemoryPolicy `json:"effective_memory_policy"`
}

// MemoryPolicyService is the single seam for resolving raw capability, user
// preference and session override inputs into an effective policy.
type MemoryPolicyService struct {
	repository        *repository.MemoryRepository
	capabilityEnabled bool
}

func NewMemoryPolicyService(memory *repository.MemoryRepository, capabilityEnabled bool) *MemoryPolicyService {
	return &MemoryPolicyService{repository: memory, capabilityEnabled: capabilityEnabled}
}

func (s *MemoryPolicyService) CapabilityEnabled() bool {
	return s != nil && s.capabilityEnabled
}

func (s *MemoryPolicyService) GetPreference(ctx context.Context, userID int64) (MemoryPreferenceView, error) {
	if s == nil || s.repository == nil {
		return MemoryPreferenceView{}, errors.New("memory policy repository unavailable")
	}
	preference, err := s.repository.GetMemoryPreference(ctx, userID)
	if err != nil {
		return MemoryPreferenceView{}, err
	}
	return memoryPreferenceView(preference, s.capabilityEnabled), nil
}

func (s *MemoryPolicyService) UpdatePreference(ctx context.Context, userID int64, enabled bool, expectedVersion int64) (MemoryPreferenceView, error) {
	if s == nil || s.repository == nil {
		return MemoryPreferenceView{}, errors.New("memory policy repository unavailable")
	}
	preference, err := s.repository.UpdateMemoryPreference(ctx, userID, enabled, expectedVersion, s.capabilityEnabled)
	if err != nil {
		return MemoryPreferenceView{}, err
	}
	return memoryPreferenceView(preference, s.capabilityEnabled), nil
}

func (s *MemoryPolicyService) Resolve(ctx context.Context, userID, sessionID int64) (model.EffectiveMemoryPolicy, error) {
	if s == nil || s.repository == nil {
		return model.EffectiveMemoryPolicy{}, errors.New("memory policy repository unavailable")
	}
	inputs, err := s.repository.ResolveMemoryPolicyInputs(ctx, userID, sessionID)
	if err != nil {
		return model.EffectiveMemoryPolicy{}, err
	}
	return ResolveEffectiveMemoryPolicy(s.capabilityEnabled, inputs.UserEnabled, inputs.UserPreferenceVersion, inputs.SessionPolicy, inputs.SessionPolicyVersion), nil
}

func (s *MemoryPolicyService) GetSessionPolicy(ctx context.Context, userID, sessionID int64) (SessionMemoryPolicyView, error) {
	policy, err := s.Resolve(ctx, userID, sessionID)
	if err != nil {
		return SessionMemoryPolicyView{}, err
	}
	return SessionMemoryPolicyView{
		SessionID: sessionID, Policy: policy.SessionPolicy, Version: policy.SessionPolicyVersion,
		EffectiveMemoryPolicy: policy,
	}, nil
}

func (s *MemoryPolicyService) UpdateSessionPolicy(ctx context.Context, userID, sessionID int64, policy string, expectedVersion int64) (SessionMemoryPolicyView, error) {
	if s == nil || s.repository == nil {
		return SessionMemoryPolicyView{}, errors.New("memory policy repository unavailable")
	}
	policy = NormalizeSessionMemoryPolicy(policy)
	if !ValidSessionMemoryPolicy(policy) {
		return SessionMemoryPolicyView{}, fmt.Errorf("memory policy 必须为 inherit、enabled 或 disabled")
	}
	_, inputs, err := s.repository.UpdateSessionMemoryPolicy(ctx, userID, sessionID, policy, expectedVersion, s.capabilityEnabled)
	if err != nil {
		return SessionMemoryPolicyView{}, err
	}
	effective := ResolveEffectiveMemoryPolicy(
		s.capabilityEnabled, inputs.UserEnabled, inputs.UserPreferenceVersion,
		inputs.SessionPolicy, inputs.SessionPolicyVersion,
	)
	return SessionMemoryPolicyView{
		SessionID: sessionID, Policy: effective.SessionPolicy, Version: effective.SessionPolicyVersion,
		EffectiveMemoryPolicy: effective,
	}, nil
}

func (s *MemoryPolicyService) AttachToSession(ctx context.Context, session *model.ChatSession) error {
	if session == nil {
		return nil
	}
	policy, err := s.Resolve(ctx, session.UserID, session.ID)
	if err != nil {
		return err
	}
	session.MemoryPolicy = policy.SessionPolicy
	session.MemoryPolicyVersion = policy.SessionPolicyVersion
	session.EffectiveMemoryPolicy = &policy
	return nil
}

func (s *MemoryPolicyService) AttachToSessions(ctx context.Context, userID int64, sessions []model.ChatSession) error {
	if s == nil || s.repository == nil {
		return errors.New("memory policy repository unavailable")
	}
	preference, err := s.repository.GetMemoryPreference(ctx, userID)
	if err != nil {
		return err
	}
	for index := range sessions {
		if sessions[index].UserID != userID {
			return gorm.ErrInvalidData
		}
		policy := ResolveEffectiveMemoryPolicy(
			s.capabilityEnabled, preference.Enabled, preference.Version,
			sessions[index].MemoryPolicy, sessions[index].MemoryPolicyVersion,
		)
		sessions[index].MemoryPolicy = policy.SessionPolicy
		sessions[index].EffectiveMemoryPolicy = &policy
	}
	return nil
}

func (s *MemoryPolicyService) FailClosed(session *model.ChatSession) model.EffectiveMemoryPolicy {
	capabilityEnabled := s != nil && s.capabilityEnabled
	policy, version := model.MemorySessionPolicyInherit, int64(0)
	if session != nil {
		policy = NormalizeSessionMemoryPolicy(session.MemoryPolicy)
		version = session.MemoryPolicyVersion
	}
	return model.EffectiveMemoryPolicy{
		CapabilityEnabled: capabilityEnabled, SessionPolicy: policy, SessionPolicyVersion: version,
		EffectiveEnabled: false, Reason: model.MemoryPolicyReasonUnavailable,
	}
}

func (s *ChatService) effectiveMemoryPolicyForRequest(ctx context.Context, session *model.ChatSession) model.EffectiveMemoryPolicy {
	if s == nil || s.memoryPolicy == nil || session == nil {
		var policyService *MemoryPolicyService
		if s != nil {
			policyService = s.memoryPolicy
		}
		return policyService.FailClosed(session)
	}
	policy, err := s.memoryPolicy.Resolve(ctx, session.UserID, session.ID)
	if err != nil {
		return s.memoryPolicy.FailClosed(session)
	}
	return policy
}

func ResolveEffectiveMemoryPolicy(capabilityEnabled, userEnabled bool, userVersion int64, sessionPolicy string, sessionVersion int64) model.EffectiveMemoryPolicy {
	sessionPolicy = NormalizeSessionMemoryPolicy(sessionPolicy)
	policy := model.EffectiveMemoryPolicy{
		CapabilityEnabled: capabilityEnabled, UserEnabled: userEnabled, UserPreferenceVersion: userVersion,
		SessionPolicy: sessionPolicy, SessionPolicyVersion: sessionVersion,
	}
	if !capabilityEnabled {
		policy.Reason = model.MemoryPolicyReasonCapabilityDisabled
		return policy
	}
	switch sessionPolicy {
	case model.MemorySessionPolicyDisabled:
		policy.Reason = model.MemoryPolicyReasonSessionDisabled
	case model.MemorySessionPolicyEnabled:
		policy.EffectiveEnabled = true
		policy.Reason = model.MemoryPolicyReasonSessionEnabled
	default:
		policy.EffectiveEnabled = userEnabled
		if userEnabled {
			policy.Reason = model.MemoryPolicyReasonUserEnabled
		} else {
			policy.Reason = model.MemoryPolicyReasonUserDisabled
		}
	}
	return policy
}

func NormalizeSessionMemoryPolicy(policy string) string {
	policy = strings.TrimSpace(strings.ToLower(policy))
	if policy == "" {
		return model.MemorySessionPolicyInherit
	}
	return policy
}

func ValidSessionMemoryPolicy(policy string) bool {
	switch NormalizeSessionMemoryPolicy(policy) {
	case model.MemorySessionPolicyInherit, model.MemorySessionPolicyEnabled, model.MemorySessionPolicyDisabled:
		return true
	default:
		return false
	}
}

func memoryPreferenceView(preference model.AgentMemoryPreference, capabilityEnabled bool) MemoryPreferenceView {
	view := MemoryPreferenceView{
		Enabled: preference.Enabled, Version: preference.Version, CapabilityEnabled: capabilityEnabled,
		EffectiveEnabled: capabilityEnabled && preference.Enabled,
	}
	if !capabilityEnabled {
		view.Reason = model.MemoryPolicyReasonCapabilityDisabled
	} else if preference.Enabled {
		view.Reason = model.MemoryPolicyReasonUserEnabled
	} else {
		view.Reason = model.MemoryPolicyReasonUserDisabled
	}
	return view
}
