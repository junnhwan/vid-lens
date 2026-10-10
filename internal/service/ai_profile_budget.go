package service

import (
	"fmt"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

type ResolvedConversationProfile struct {
	Profile              *ai.Profile
	ProfileID            int64
	AgentBudget          *model.AgentBudgetOverride
	EffectiveAgentBudget config.ResolvedAgentBudget
}

func (s *AIProfileService) WithAgentBudgetConfig(cfg config.AgentBudgetConfig) *AIProfileService {
	s.budgetConfig = cfg
	return s
}
func (s *AIProfileService) AgentBudgetOptions() config.AgentBudgetConfig { return s.budgetConfig }
func (s *AIProfileService) resolveStoredBudget(profile *model.UserAIProfile) (*model.AgentBudgetOverride, *config.ResolvedAgentBudget, error) {
	var override *model.AgentBudgetOverride
	if profile.AgentBudgetJSON != nil {
		var err error
		override, err = model.DecodeAgentBudget([]byte(*profile.AgentBudgetJSON))
		if err != nil {
			return nil, nil, fmt.Errorf("profile %d: %w", profile.ID, err)
		}
	}
	effective, err := s.budgetConfig.Resolve(override)
	if err != nil {
		return override, nil, err
	}
	return override, &effective, nil
}

// GetDefaultConversationProfile freezes provider identity and budget from one database read.
func (s *AIProfileService) GetDefaultConversationProfile(userID int64) (*ResolvedConversationProfile, error) {
	return s.GetConversationProfile(userID, 0)
}

// GetConversationProfile resolves an explicit owner-scoped profile without
// consulting the current default when resuming a frozen import.
func (s *AIProfileService) GetConversationProfile(userID, profileID int64) (*ResolvedConversationProfile, error) {
	var profile *model.UserAIProfile
	var err error
	if profileID == 0 {
		profile, err = s.repo.FindDefaultByUserID(userID)
	} else {
		profile, err = s.repo.FindByIDForUser(userID, profileID)
	}
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrAIProfileRequired
	}
	decrypted, err := s.decryptProfile(profile)
	if err != nil {
		return nil, err
	}
	override, effective, err := s.resolveStoredBudget(profile)
	if err != nil {
		return nil, err
	}
	return &ResolvedConversationProfile{Profile: providerFromDecrypted(decrypted), ProfileID: profile.ID, AgentBudget: override, EffectiveAgentBudget: *effective}, nil
}

func (s *AIProfileService) GetAIProfileByID(userID, profileID int64) (*ai.Profile, error) {
	resolved, err := s.GetConversationProfile(userID, profileID)
	if err != nil {
		return nil, err
	}
	return resolved.Profile, nil
}
