package ai

import (
	"context"

	"vid-lens/internal/model"
)

// TranscriptionResult keeps native speech timing alongside the provider text.
// Segments use milliseconds relative to the submitted audio, never estimates
// derived from text length. Missing timing leaves Segments empty.
type TranscriptionResult struct {
	Text     string
	Segments []model.TranscriptionSegment
}

// TimedTranscriptionStrategy is additive so legacy adapters remain usable.
type TimedTranscriptionStrategy interface {
	TranscribeDetailed(context.Context, string) (TranscriptionResult, error)
}

func TranscribeDetailed(ctx context.Context, base AudioTranscriptionClient, audioPath string) (TranscriptionResult, error) {
	if timed, ok := base.(TimedTranscriptionStrategy); ok {
		return timed.TranscribeDetailed(ctx, audioPath)
	}
	text, err := base.Transcribe(ctx, audioPath)
	return TranscriptionResult{Text: text}, err
}
