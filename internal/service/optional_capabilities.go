package service

import (
	"context"
	"errors"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
)

var ErrRerankUnavailable = errors.New("当前 AI 服务未配置可用的检索重排，请先检查 AI 配置")

type optionalCapabilityProfileResolver interface {
	GetDefaultAIProfile(int64) (*ai.Profile, error)
}

type userOptionalCapabilities struct {
	profiles            optionalCapabilityProfileResolver
	ragEnabled          bool
	retrieval           RAGRetrievalConfig
	alignmentConfigured bool
	tools               config.ToolsConfig
}

type OptionalCapabilitiesView struct {
	Capabilities        []CapabilityState      `json:"capabilities"`
	Actions             map[string]ActionState `json:"actions"`
	RerankEnabled       bool                   `json:"rerank_enabled"`
	RerankAvailable     bool                   `json:"rerank_available"`
	RerankMode          string                 `json:"rerank_mode"`
	RerankModel         string                 `json:"rerank_model,omitempty"`
	RerankReason        string                 `json:"rerank_reason,omitempty"`
	AlignmentConfigured bool                   `json:"alignment_configured"`
}

func (s *UserService) WithCapabilityTools(tools config.ToolsConfig) *UserService {
	if s.optionalCapabilities != nil {
		s.optionalCapabilities.tools = tools
	}
	return s
}

func (s *UserService) WithOptionalCapabilities(profiles optionalCapabilityProfileResolver, ragEnabled bool, retrieval RAGRetrievalConfig, alignmentConfigured bool) *UserService {
	s.optionalCapabilities = &userOptionalCapabilities{profiles: profiles, ragEnabled: ragEnabled, retrieval: retrieval, alignmentConfigured: alignmentConfigured}
	return s
}

func (s *UserService) OptionalCapabilities(ctx context.Context, userID int64) (*OptionalCapabilitiesView, error) {
	enabled, err := s.repo.RerankEnabled(ctx, userID)
	if err != nil {
		return nil, err
	}
	view := &OptionalCapabilitiesView{RerankEnabled: enabled, RerankMode: RerankerModeNone}
	defer func() { s.projectCapabilities(view) }()
	cfg := s.optionalCapabilities
	if cfg == nil {
		view.RerankReason = "rerank_not_configured"
		return view, nil
	}
	view.AlignmentConfigured = cfg.alignmentConfigured
	if cfg.profiles == nil {
		view.RerankReason = "ai_profile_required"
		return view, nil
	}
	profile, err := cfg.profiles.GetDefaultAIProfile(userID)
	if errors.Is(err, ErrAIProfileRequired) || errors.Is(err, ErrAIProfileNotFound) || errors.Is(err, ErrHostedAIUnavailable) || err == nil && profile == nil {
		view.RerankReason = "ai_profile_required"
		if errors.Is(err, ErrHostedAIUnavailable) {
			view.RerankReason = "hosted_paused"
		}
		return view, nil
	}
	if err != nil {
		return nil, err
	}
	view.Capabilities = modelCapabilityStates(*profile)
	if !cfg.ragEnabled {
		view.RerankReason = "rag_disabled"
		return view, nil
	}
	view.RerankMode, view.RerankModel = cfg.retrieval.RerankerMode, cfg.retrieval.RerankerVersion
	if strings.TrimSpace(profile.RerankModel) != "" {
		view.RerankMode, view.RerankModel = RerankerModeModel, profile.RerankModel
	}
	switch view.RerankMode {
	case RerankerModeDeterministic:
		view.RerankAvailable = true
		view.RerankModel = ""
	case RerankerModeModel:
		copy := *profile
		copy.RerankModel = view.RerankModel
		if view.RerankModel != "" {
			_, err = ai.NewFactory().NewRerankClient(copy)
			view.RerankAvailable = err == nil
		}
		if !view.RerankAvailable {
			view.RerankReason = "rerank_connection_unavailable"
		}
	default:
		view.RerankReason = "rerank_not_configured"
	}
	return view, nil
}

func (s *UserService) SetRerankPreference(ctx context.Context, userID int64, enabled bool) (*OptionalCapabilitiesView, error) {
	view, err := s.OptionalCapabilities(ctx, userID)
	if err != nil {
		return nil, err
	}
	if enabled && !view.RerankAvailable {
		return nil, ErrRerankUnavailable
	}
	if err := s.repo.SetRerankEnabled(ctx, userID, enabled); err != nil {
		return nil, err
	}
	view.RerankEnabled = enabled
	s.projectCapabilities(view)
	return view, nil
}
