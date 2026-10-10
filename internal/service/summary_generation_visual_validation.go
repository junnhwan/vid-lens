package service

import (
	"errors"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/textsource"
)

// Historic text publications may lack block refs even with fully timed ASR.
// Preserve their readable result but report the failed block/source association
// honestly instead of claiming the source has no real time information.
func summaryGenerationVisualReferenceFailure(err error, source *textsource.Snapshot, summary *model.AISummary) error {
	var failure *artifact.Error
	if !errors.As(err, &failure) || failure.Code != "visual_location_missing" || source == nil || summary == nil {
		return err
	}
	doc, parseErr := summarydoc.Parse([]byte(summary.DocumentJSON))
	if parseErr != nil {
		return err
	}
	for _, block := range doc.Blocks {
		if len(block.SourceRefs) > 0 {
			return err
		}
	}
	for _, cue := range source.Cues {
		if cue.StartMS != nil && cue.EndMS != nil && *cue.EndMS > *cue.StartMS {
			return artifact.Err("visual_source_refs_missing", 422)
		}
	}
	return err
}
