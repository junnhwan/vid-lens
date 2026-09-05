package service

import (
	"fmt"
	"sort"
	"strings"

	"vid-lens/internal/model"
)

// TimelineAtom is the read-only projection of one canonical source
// observation. Retrieval chunks and generation context are derived from these
// observations; neither is used as the playback source of truth.
type TimelineAtom struct {
	ID              string           `json:"id"`
	Modality        string           `json:"modality"`
	Content         string           `json:"content"`
	StartMS         int64            `json:"start_ms"`
	EndMS           int64            `json:"end_ms"`
	TimeRangeStatus string           `json:"time_range_status"`
	Source          string           `json:"source,omitempty"`
	SourceRefs      []ChunkSourceRef `json:"source_refs,omitempty"`
}

type VideoTimeline struct {
	TaskID int64          `json:"task_id"`
	Title  string         `json:"title,omitempty"`
	Atoms  []TimelineAtom `json:"atoms"`
}

// BuildVideoTimeline projects the existing ASR window and visual-frame rows
// into one ordered timeline. It does not manufacture precise timestamps when
// the provider did not persist them.
func BuildVideoTimeline(taskID int64, transcriptRows []model.VideoTranscriptionChunk, frames []model.VideoVisualFrame) VideoTimeline {
	atoms := make([]TimelineAtom, 0, len(transcriptRows)+len(frames)*2)
	for _, row := range transcriptRows {
		content := strings.TrimSpace(row.Content)
		if row.Status != model.TranscriptionChunkStatusCompleted || content == "" {
			continue
		}
		startMS, endMS, status := transcriptTimelineRange(row)
		stableID := strings.TrimSpace(row.SegmentKey)
		if stableID == "" {
			if row.ID > 0 {
				stableID = fmt.Sprintf("transcription-chunk:%d", row.ID)
			} else {
				stableID = fmt.Sprintf("transcription-chunk:%d", row.ChunkIndex)
			}
		}
		ref := ChunkSourceRef{
			SourceType: model.ChunkModalityTranscript, StableID: stableID,
			SegmentKey: strings.TrimSpace(row.SegmentKey), SourceRowID: row.ID,
			StartMS: startMS, EndMS: endMS, TimeRangeStatus: status,
		}
		atoms = append(atoms, TimelineAtom{
			ID: "transcript:" + stableID, Modality: model.ChunkModalityTranscript,
			Content: content, StartMS: startMS, EndMS: endMS,
			TimeRangeStatus: status, Source: "asr", SourceRefs: []ChunkSourceRef{ref},
		})
	}

	for _, frame := range frames {
		if frame.Status != model.VisualFrameStatusCompleted {
			continue
		}
		startMS, endMS, status := visualFrameRange(frame)
		stableID := visualFrameStableID(frame)
		appendVisual := func(content, modality, method string) {
			content = strings.TrimSpace(content)
			if content == "" {
				return
			}
			ref := ChunkSourceRef{
				SourceType: modality, StableID: stableID, SourceRowID: frame.ID,
				StartMS: startMS, EndMS: endMS, TimeRangeStatus: status,
				ObjectKey: frame.ObjectKey, CaptionMethod: method,
			}
			atoms = append(atoms, TimelineAtom{
				ID: fmt.Sprintf("%s:%s", modality, stableID), Modality: modality,
				Content: content, StartMS: startMS, EndMS: endMS,
				TimeRangeStatus: status, Source: frame.Source, SourceRefs: []ChunkSourceRef{ref},
			})
		}
		appendVisual(frame.OCRText, model.ChunkModalityVisualOCR, "ocr")
		appendVisual(frame.VisionCaption, model.ChunkModalityVisualCaption, "vision")
	}

	sort.SliceStable(atoms, func(i, j int) bool {
		if atoms[i].StartMS != atoms[j].StartMS {
			return atoms[i].StartMS < atoms[j].StartMS
		}
		if atoms[i].Modality != atoms[j].Modality {
			return timelineModalityRank(atoms[i].Modality) < timelineModalityRank(atoms[j].Modality)
		}
		return atoms[i].ID < atoms[j].ID
	})
	return VideoTimeline{TaskID: taskID, Atoms: atoms}
}

func transcriptTimelineRange(row model.VideoTranscriptionChunk) (int64, int64, string) {
	if row.CoreEndMS > row.CoreStartMS && row.CoreStartMS >= 0 {
		return row.CoreStartMS, row.CoreEndMS, model.ChunkTimeRangeCoarse
	}
	if row.WindowEndMS > row.WindowStartMS && row.WindowStartMS >= 0 {
		return row.WindowStartMS, row.WindowEndMS, model.ChunkTimeRangeCoarse
	}
	if row.EndSecond > row.StartSecond && row.StartSecond >= 0 {
		return int64(row.StartSecond) * 1000, int64(row.EndSecond) * 1000, model.ChunkTimeRangeCoarse
	}
	return 0, 0, model.ChunkTimeRangeUnknown
}

func timelineModalityRank(modality string) int {
	switch modality {
	case model.ChunkModalityTranscript:
		return 0
	case model.ChunkModalityVisualOCR:
		return 1
	case model.ChunkModalityVisualCaption:
		return 2
	default:
		return 3
	}
}
