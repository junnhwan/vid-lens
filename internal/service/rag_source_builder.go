package service

import (
	"fmt"
	"strings"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/transcript"
)

func buildTranscriptIndexChunks(content string, rows []model.VideoTranscriptionChunk, chunkSize, overlap int) []TextChunk {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	completed := make([]model.VideoTranscriptionChunk, 0, len(rows))
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Status != model.TranscriptionChunkStatusCompleted || strings.TrimSpace(row.Content) == "" {
			continue
		}
		completed = append(completed, row)
		parts = append(parts, strings.TrimSpace(row.Content))
	}
	if len(completed) == 0 {
		return SplitObservationsIntoChunks([]SourceTextObservation{{Content: content, Modality: model.ChunkModalityTranscript}}, chunkSize, overlap)
	}

	assembled := transcript.Assemble(rows)
	// Published legacy text can be upgraded deterministically from its raw
	// observations. Arbitrary edits still cannot acquire invented provenance.
	legacy := strings.Join(parts, "\n\n")
	if transcriptionRowsOverlap(completed) {
		legacy = transcript.Stitch(parts).Content
	}
	if strings.TrimSpace(assembled.Content) != content && strings.TrimSpace(legacy) != content {
		return SplitObservationsIntoChunks([]SourceTextObservation{{Content: content, Modality: model.ChunkModalityTranscript}}, chunkSize, overlap)
	}
	return SplitObservationsIntoChunks(assembledTranscriptObservations(rows), chunkSize, overlap)
}

func assembledTranscriptObservations(rows []model.VideoTranscriptionChunk) []SourceTextObservation {
	assembled := transcript.Assemble(rows)
	observations := make([]SourceTextObservation, 0, len(assembled.Contributions))
	for _, contribution := range assembled.Contributions {
		row := rows[contribution.PartIndex]
		spans, _ := transcriptObservations(row, contribution.Content, contribution.StartRune, contribution.EndRune)
		observations = append(observations, spans...)
	}
	return observations
}

func transcriptionRowsOverlap(rows []model.VideoTranscriptionChunk) bool {
	if len(rows) < 2 {
		return false
	}
	for i := 1; i < len(rows); i++ {
		previous, current := rows[i-1], rows[i]
		if !transcript.AdjacentOverlap(previous, current) {
			return false
		}
	}
	return true
}

func transcriptSourceRef(row model.VideoTranscriptionChunk) ChunkSourceRef {
	stableID := strings.TrimSpace(row.SegmentKey)
	if stableID == "" && row.ID > 0 {
		stableID = fmt.Sprintf("transcription-chunk:%d", row.ID)
	}
	if stableID == "" {
		stableID = fmt.Sprintf("transcription-chunk:%d", row.ChunkIndex)
	}
	ref := ChunkSourceRef{
		SourceType: model.ChunkModalityTranscript, StableID: stableID, ContentHash: artifact.Hash(strings.TrimSpace(row.Content)), SegmentKey: strings.TrimSpace(row.SegmentKey),
		SourceRowID: row.ID, TimeRangeStatus: model.ChunkTimeRangeUnknown,
	}
	switch {
	case row.WindowEndMS > row.WindowStartMS && row.WindowStartMS >= 0:
		ref.StartMS, ref.EndMS, ref.TimeRangeStatus = row.WindowStartMS, row.WindowEndMS, model.ChunkTimeRangeCoarse
	case row.CoreEndMS > row.CoreStartMS && row.CoreStartMS >= 0:
		ref.StartMS, ref.EndMS, ref.TimeRangeStatus = row.CoreStartMS, row.CoreEndMS, model.ChunkTimeRangeCoarse
	case row.EndSecond > row.StartSecond && row.StartSecond >= 0:
		ref.StartMS, ref.EndMS, ref.TimeRangeStatus = int64(row.StartSecond)*1000, int64(row.EndSecond)*1000, model.ChunkTimeRangeCoarse
	}
	return ref
}
