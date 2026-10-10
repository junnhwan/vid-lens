package service

import (
	"fmt"
	"sort"
	"strings"

	"vid-lens/internal/artifact"
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
	AlignmentAvailable bool            `json:"alignment_available"`
	TaskID             int64           `json:"task_id"`
	Title              string          `json:"title,omitempty"`
	Atoms              []TimelineAtom  `json:"atoms"`
	VisualCoverage     *VisualCoverage `json:"visual_coverage,omitempty"`
	StudySourceReady   bool            `json:"study_source_ready"`
	StudySourceReason  string          `json:"study_source_reason,omitempty"`
}

// Shared by the read-only capability projection and generation admission.
func studySourceReason(task *model.VideoTask, rows []model.VideoTranscriptionChunk, timeline VideoTimeline) string {
	if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
		return "processing"
	}
	for _, row := range rows {
		if row.Status != model.TranscriptionChunkStatusCompleted {
			return "incomplete_transcript"
		}
	}
	if len(timeline.Atoms) == 0 {
		return "no_content"
	}
	size := 0
	for _, atom := range timeline.Atoms {
		size += len(atom.Content)
	}
	// Native speech spans are far smaller than the old multi-minute windows.
	// The content cap and generation budgets still bound actual source size.
	if len(timeline.Atoms) > 10000 || size > 2*1024*1024 {
		return "source_limit_exceeded"
	}
	return ""
}

// VisualCoverage reports persisted extraction results, including frames with
// no readable OCR/caption. It is not a percentage or an expected-work count.
type VisualCoverage struct {
	SampledFrames   int    `json:"sampled_frames"`
	PreviewFrames   int    `json:"preview_frames"`
	EvidenceFrames  int    `json:"evidence_frames"`
	FirstMS         int64  `json:"first_ms"`
	LastMS          int64  `json:"last_ms"`
	LargestGapMS    int64  `json:"largest_gap_ms"`
	EvidenceFirstMS *int64 `json:"evidence_first_ms,omitempty"`
	EvidenceLastMS  *int64 `json:"evidence_last_ms,omitempty"`
}

func visualCoverage(frames []model.VideoVisualFrame) *VisualCoverage {
	if len(frames) == 0 {
		return nil
	}
	times := make([]int64, 0, len(frames))
	coverage := &VisualCoverage{SampledFrames: len(frames)}
	for _, frame := range frames {
		if frame.TimeMs >= 0 {
			times = append(times, frame.TimeMs)
		}
		if strings.TrimSpace(frame.ObjectKey) != "" {
			coverage.PreviewFrames++
		}
		if frame.Status == model.VisualFrameStatusCompleted && (strings.TrimSpace(frame.OCRText) != "" || strings.TrimSpace(frame.VisionCaption) != "") {
			coverage.EvidenceFrames++
			if coverage.EvidenceFirstMS == nil || frame.TimeMs < *coverage.EvidenceFirstMS {
				first := frame.TimeMs
				coverage.EvidenceFirstMS = &first
			}
			if coverage.EvidenceLastMS == nil || frame.TimeMs > *coverage.EvidenceLastMS {
				last := frame.TimeMs
				coverage.EvidenceLastMS = &last
			}
		}
	}
	if len(times) > 0 {
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		coverage.FirstMS, coverage.LastMS = times[0], times[len(times)-1]
		for i := 1; i < len(times); i++ {
			if gap := times[i] - times[i-1]; gap > coverage.LargestGapMS {
				coverage.LargestGapMS = gap
			}
		}
	}
	return coverage
}

// BuildVideoTimeline projects the existing ASR window and visual-frame rows
// into one ordered timeline. It does not manufacture precise timestamps when
// the provider did not persist them.
func BuildVideoTimeline(taskID int64, transcriptRows []model.VideoTranscriptionChunk, frames []model.VideoVisualFrame) VideoTimeline {
	return BuildVideoTimelineFromObservations(taskID, legacySourceObservations(transcriptRows), frames)
}

// BuildVideoTimelineFromObservations projects the frozen source directly.
func BuildVideoTimelineFromObservations(taskID int64, observations []SourceTextObservation, frames []model.VideoVisualFrame) VideoTimeline {
	atoms := make([]TimelineAtom, 0, len(observations)+len(frames)*2)
	textOrder := make(map[string]int64)
	order := int64(0)
	for _, observation := range observations {
		if strings.TrimSpace(observation.Content) == "" || len(observation.Refs) == 0 {
			continue
		}
		ref := observation.Refs[0]
		// Independent untimed observations are separate paragraphs. The
		// assembler's separator is structural, not part of their source wording.
		content := strings.TrimPrefix(observation.Content, "\n\n")
		ref.Content = content
		id := "transcript:" + ref.StableID
		order = max(order, ref.StartMS)
		textOrder[id] = order
		atoms = append(atoms, TimelineAtom{
			ID: id, Modality: model.ChunkModalityTranscript,
			Content: content, StartMS: ref.StartMS, EndMS: ref.EndMS,
			TimeRangeStatus: ref.TimeRangeStatus, Source: ref.SourceKind, SourceRefs: []ChunkSourceRef{ref},
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
				SourceType: modality, StableID: stableID, ContentHash: artifact.Hash(content), SourceRowID: frame.ID,
				StartMS: startMS, EndMS: endMS, TimeRangeStatus: status,
				ObjectKey: frame.ObjectKey, ArtifactKind: model.VisualArtifactKindFrame, CaptionMethod: method,
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
		left, right := atoms[i].StartMS, atoms[j].StartMS
		if order, ok := textOrder[atoms[i].ID]; ok {
			left = order
		}
		if order, ok := textOrder[atoms[j].ID]; ok {
			right = order
		}
		if left != right {
			return left < right
		}
		if atoms[i].Modality != atoms[j].Modality {
			return timelineModalityRank(atoms[i].Modality) < timelineModalityRank(atoms[j].Modality)
		}
		unknownI := atoms[i].TimeRangeStatus == model.ChunkTimeRangeUnknown
		unknownJ := atoms[j].TimeRangeStatus == model.ChunkTimeRangeUnknown
		if unknownI || unknownJ {
			// Repository transcript rows arrive in chunk_index order. Preserve that
			// order when timestamps cannot establish chronology; lexical IDs cannot.
			// Group unknowns separately from known zero-time atoms to keep the
			// comparator transitive while SliceStable preserves their input order.
			return unknownI && !unknownJ
		}
		if atoms[i].Modality == model.ChunkModalityTranscript {
			return false
		}
		return atoms[i].ID < atoms[j].ID
	})
	return VideoTimeline{TaskID: taskID, Atoms: atoms}
}

func transcriptTimelineRange(row model.VideoTranscriptionChunk) (int64, int64, string) {
	// Untimed text can include speech from either overlap margin. The core
	// identifies processing ownership, not the bounds of every retained word.
	// Use the same measured audio window as citation source references.
	if row.WindowEndMS > row.WindowStartMS && row.WindowStartMS >= 0 {
		return row.WindowStartMS, row.WindowEndMS, model.ChunkTimeRangeCoarse
	}
	if row.CoreEndMS > row.CoreStartMS && row.CoreStartMS >= 0 {
		return row.CoreStartMS, row.CoreEndMS, model.ChunkTimeRangeCoarse
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
