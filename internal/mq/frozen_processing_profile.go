package mq

import (
	"fmt"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

type frozenProfileResolver interface {
	GetAIProfileByID(int64, int64) (*ai.Profile, error)
}

func (c *Consumer) processingProfile(task *model.VideoTask) (*ai.Profile, error) {
	if task.ProcessingIntentJSON == "" {
		return c.profiles.GetDefaultAIProfile(task.UserID)
	}
	intent, err := processing.Decode(task.ProcessingIntentJSON)
	if err != nil {
		return nil, err
	}
	resolver, ok := c.profiles.(frozenProfileResolver)
	if !ok || intent.ProfileID <= 0 {
		return nil, fmt.Errorf("frozen AI profile unavailable")
	}
	profile, err := resolver.GetAIProfileByID(task.UserID, intent.ProfileID)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.ID != intent.ProfileID || processing.FingerprintProfile(*profile) != intent.ProfileFingerprint {
		return nil, fmt.Errorf("frozen AI profile changed; submit a new generation")
	}
	return profile, nil
}
