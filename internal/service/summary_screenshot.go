package service

import (
	"context"
	"vid-lens/internal/artifact"
	"vid-lens/internal/storage"
)

// OpenSummaryScreenshot resolves only opaque registered refs under an
// authenticated owner. Object keys never enter the public document.
func (s *MediaService) OpenSummaryScreenshot(ctx context.Context, owner, taskID int64, id string) (storage.Object, error) {
	ref, err := s.repo.ReadSummaryScreenshot(ctx, owner, taskID, id)
	if err != nil {
		return nil, err
	}
	if s.storage == nil {
		return nil, artifact.Err("image_unavailable", 503)
	}
	return s.storage.OpenObject(ctx, ref.ObjectKey)
}
