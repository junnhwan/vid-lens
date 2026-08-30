package service

import (
	"fmt"
	"strings"

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

	var rebuilt string
	contributions := make([]transcript.Contribution, 0, len(parts))
	if transcriptionRowsOverlap(completed) {
		stitched := transcript.Stitch(parts)
		rebuilt, contributions = stitched.Content, stitched.Contributions
	} else {
		var builder strings.Builder
		for i, part := range parts {
			retained := part
			if i > 0 {
				retained = "\n\n" + retained
			}
			builder.WriteString(retained)
			contributions = append(contributions, transcript.Contribution{PartIndex: i, Content: retained})
		}
		rebuilt = builder.String()
	}
	if strings.TrimSpace(rebuilt) != content {
		return SplitObservationsIntoChunks([]SourceTextObservation{{Content: content, Modality: model.ChunkModalityTranscript}}, chunkSize, overlap)
	}

	observations := make([]SourceTextObservation, 0, len(contributions))
	for _, contribution := range contributions {
		if contribution.PartIndex < 0 || contribution.PartIndex >= len(completed) || contribution.Content == "" {
			continue
		}
		row := completed[contribution.PartIndex]
		ref := transcriptSourceRef(row)
		observations = append(observations, SourceTextObservation{
			Content: contribution.Content, Modality: model.ChunkModalityTranscript, Refs: []ChunkSourceRef{ref},
		})
	}
	return SplitObservationsIntoChunks(observations, chunkSize, overlap)
}

func transcriptionRowsOverlap(rows []model.VideoTranscriptionChunk) bool {
	if len(rows) < 2 {
		return false
	}
	for i := 1; i < len(rows); i++ {
		previous, current := rows[i-1], rows[i]
		if strings.TrimSpace(previous.SegmentKey) == "" || strings.TrimSpace(current.SegmentKey) == "" ||
			strings.TrimSpace(previous.SegmenterVersion) == "" || strings.TrimSpace(current.SegmenterVersion) == "" ||
			previous.WindowEndMS <= current.WindowStartMS {
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
	ref := ChunkSourceRef{
		SourceType: model.ChunkModalityTranscript, StableID: stableID, SegmentKey: strings.TrimSpace(row.SegmentKey),
		SourceRowID: row.ID, TimeRangeStatus: model.ChunkTimeRangeUnknown,
	}
	switch {
	case row.WindowEndMS > row.WindowStartMS && row.WindowStartMS >= 0:
		ref.StartMS, ref.EndMS, ref.TimeRangeStatus = row.WindowStartMS, row.WindowEndMS, model.ChunkTimeRangeCoarse
	case row.EndSecond > row.StartSecond && row.StartSecond >= 0:
		ref.StartMS, ref.EndMS, ref.TimeRangeStatus = int64(row.StartSecond)*1000, int64(row.EndSecond)*1000, model.ChunkTimeRangeCoarse
	}
	return ref
}
