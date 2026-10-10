package service

import (
	"context"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/textsource"
)

func (s *MediaService) WithSummaryExperience(policy config.SummaryExperienceConfig) *MediaService {
	s.cfg.SummaryExperience = policy
	return s
}

func (s *MediaService) requireV2GenerationAdmission() error {
	if !s.cfg.SummaryExperience.GenerationEnabled() {
		return artifact.Err("summary_generation_disabled", 503)
	}
	return nil
}

// The queue continues accepted frozen generations. Only its optional visual
// hook follows the current server switch, so text can still be published.
func WithSummaryVisualPolicy(inner SummaryVisualEnricher, policy config.SummaryExperienceConfig) SummaryVisualEnricher {
	return summaryVisualPolicy{inner: inner, enabled: policy.VisualEnabled()}
}

type summaryVisualPolicy struct {
	inner   SummaryVisualEnricher
	enabled bool
}

func (p summaryVisualPolicy) Enrich(ctx context.Context, task *model.VideoTask, job *model.TaskJob, frozen processing.GenerationSnapshot, profile ai.Profile, source *textsource.Snapshot, summary *model.AISummary, token string) error {
	if !p.enabled {
		return artifact.Err("visual_disabled", 409)
	}
	if p.inner == nil {
		return artifact.Err("vision_unavailable", 409)
	}
	return p.inner.Enrich(ctx, task, job, frozen, profile, source, summary, token)
}
