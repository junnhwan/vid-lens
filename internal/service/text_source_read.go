package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
)

// UnifiedTextSource carries one frozen source. LegacyChunks are actual ASR
// windows only; subtitle cues never acquire fabricated ASR rows.
type UnifiedTextSource struct {
	Snapshot      *textsource.Snapshot
	Transcription *model.VideoTranscription
	LegacyChunks  []model.VideoTranscriptionChunk
	Observations  []SourceTextObservation
}

func taskTextSource(ctx context.Context, repos *repository.Repositories, task *model.VideoTask) (*UnifiedTextSource, error) {
	if task == nil {
		return nil, fmt.Errorf("文字来源任务不存在")
	}
	if task.ActiveTextSourceID != "" {
		if repos.TextSource == nil {
			return nil, fmt.Errorf("文字来源存储未配置")
		}
		// Read the pointer from this task snapshot. A concurrent refresh cannot mix
		// one source's text with another source's observations within this read.
		snapshot, err := repos.TextSource.Read(ctx, task.UserID, task.ID, task.ActiveTextSourceID)
		if err != nil {
			return nil, err
		}
		if snapshot == nil || snapshot.Identity.MediaFingerprint != task.FileMD5 {
			return nil, fmt.Errorf("文字来源与视频身份不一致")
		}
		return &UnifiedTextSource{Snapshot: snapshot, Transcription: &model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: snapshot.CanonicalText, Words: len([]rune(snapshot.CanonicalText)), SourceID: snapshot.ID, SourceKind: snapshot.Kind, SourceDigest: snapshot.SourceDigest}, Observations: textSourceObservations(snapshot)}, nil
	}
	if !repository.LegacyResultReuseAllowed(task) {
		// New imports publish a source atomically. Partial ASR windows are
		// progress, never the authoritative text before publication.
		return &UnifiedTextSource{}, nil
	}
	transcription, rows, err := taskTranscriptSource(repos, task)
	if err != nil {
		return nil, err
	}
	return &UnifiedTextSource{Transcription: transcription, LegacyChunks: rows, Observations: legacySourceObservations(rows)}, nil
}

func textSourceObservations(snapshot *textsource.Snapshot) []SourceTextObservation {
	observations := make([]SourceTextObservation, 0, len(snapshot.Cues))
	for i, cue := range snapshot.Cues {
		ref := ChunkSourceRef{SourceType: model.ChunkModalityTranscript, SourceID: snapshot.ID, SourceDigest: snapshot.SourceDigest, SourceKind: snapshot.Kind, MediaFingerprint: snapshot.Identity.MediaFingerprint, CueIDs: []string{cue.ID}, TimingMethod: cue.TimingMethod, StableID: "text-source:" + snapshot.Kind + ":" + snapshot.ID + ":" + snapshot.SourceDigest + ":" + cue.ID, Content: cue.Text, ContentHash: artifact.Hash(cue.Text), TimeRangeStatus: model.ChunkTimeRangeUnknown}
		if cue.StartMS != nil && cue.EndMS != nil && *cue.StartMS >= 0 && *cue.EndMS > *cue.StartMS {
			ref.StartMS, ref.EndMS, ref.TimeRangeStatus = *cue.StartMS, *cue.EndMS, model.ChunkTimeRangeCoarse
			if snapshot.Kind == textsource.KindASR && (cue.TimingMethod == "forced_alignment" || cue.TimingMethod == "provider_segments" || cue.TimingMethod == "provider_segment" || cue.TimingMethod == "native_segment" || cue.TimingMethod == "asr_native") {
				ref.TimeRangeStatus = model.ChunkTimeRangeExact
			}
		} else {
			ref.TimingMethod = textsource.TimingUnknown
		}
		content := cue.Text
		if i > 0 {
			separator := "\n"
			if cue.JoinBefore != nil {
				separator = *cue.JoinBefore
			}
			content = separator + content
		}
		observations = append(observations, SourceTextObservation{Content: content, Modality: model.ChunkModalityTranscript, Refs: []ChunkSourceRef{ref}})
	}
	return observations
}

func legacySourceObservations(rows []model.VideoTranscriptionChunk) []SourceTextObservation {
	observations := assembledTranscriptObservations(rows)
	for i := range observations {
		for j := range observations[i].Refs {
			ref := &observations[i].Refs[j]
			ref.SourceKind = textsource.KindASR
			ref.TimingMethod = textsource.TimingUnknown
			if ref.TimeRangeStatus == model.ChunkTimeRangeCoarse {
				ref.TimingMethod = "asr_window"
			}
			if ref.TimeRangeStatus != model.ChunkTimeRangeExact {
				continue
			}
			ref.TimingMethod = "provider_segment"
			// Existing exact IDs identify the retained provider segment. Preserve its
			// actual alignment method; sentence grouping retains the first segment ID.
			at := strings.LastIndex(ref.StableID, ":asr:")
			if at < 0 {
				continue
			}
			ordinal := strings.Split(ref.StableID[at+len(":asr:"):], ":")[0]
			index, err := strconv.Atoi(ordinal)
			if err != nil {
				continue
			}
			for _, row := range rows {
				if row.ID != ref.SourceRowID || !strings.HasPrefix(ref.StableID, transcriptSourceRef(row).StableID+":asr:") {
					continue
				}
				var segments []model.TranscriptionSegment
				if json.Unmarshal([]byte(row.TimedSegments), &segments) == nil && index >= 0 && index < len(segments) && segments[index].Method != "" {
					ref.TimingMethod = segments[index].Method
				}
				break
			}
		}
	}
	return observations
}

func retainLegacyTiming(chunks []TextChunk, observations []SourceTextObservation) {
	refs := map[string]ChunkSourceRef{}
	for _, observation := range observations {
		for _, ref := range observation.Refs {
			refs[ref.StableID] = ref
		}
	}
	for i := range chunks {
		for j := range chunks[i].SourceRefs {
			ref := &chunks[i].SourceRefs[j]
			if original, ok := refs[ref.StableID]; ok {
				ref.TimingMethod, ref.SourceKind = original.TimingMethod, original.SourceKind
			}
		}
	}
}
