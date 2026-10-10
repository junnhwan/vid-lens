package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
)

func publishReadFixture(t *testing.T, repos *repository.Repositories, task *model.VideoTask, start int64) *model.VideoTask {
	t.Helper()
	snapshot, err := textsource.ParseSRT(context.Background(), []byte(fmt.Sprintf("1\n00:00:%02d,000 --> 00:00:%02d,000\n当前字幕说明缓存策略。\n\n2\n00:00:%02d,000 --> 00:00:%02d,000\n第二句说明连接限制。\n", start, start+2, start+4, start+6)), textsource.ParseOptions{Identity: textsource.Identity{Platform: "bilibili", BVID: "BV-source", CID: 123, PartIndex: 2, MediaFingerprint: task.FileMD5}, Language: "zh", TrackKey: "zh", SubtitleKind: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repos.PublishTextSource(context.Background(), repository.PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, ExpectedActiveSourceID: task.ActiveTextSourceID, Snapshot: snapshot}, nil); err != nil {
		t.Fatal(err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func TestUnifiedTextSourceSubtitleIgnoresStaleASRAndFreezesSharedReferences(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "source-media", Filename: "lesson.mp4", FileURL: "videos/lesson.mp4", Status: model.TaskStatusCompleted, Stage: model.TaskStageUploaded, ProcessingIntentJSON: "{}"}
	identity, err := json.Marshal(textsource.Identity{Platform: "bilibili", BVID: "BV-source", CID: 123, PartIndex: 2, MediaFingerprint: task.FileMD5})
	if err != nil {
		t.Fatal(err)
	}
	task.MediaIdentityJSON = string(identity)
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "旧 ASR 文字。"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.TranscriptionChunk.UpsertCompletedWithTimeline(task.ID, 0, "old-audio", "旧 ASR 文字。", repository.TranscriptionChunkTimeline{SegmentKey: "old-asr", WindowStartMS: 0, WindowEndMS: 20000, CoreStartMS: 0, CoreEndMS: 20000}); err != nil {
		t.Fatal(err)
	}
	task = publishReadFixture(t, repos, task, 10)
	source, err := taskTextSource(context.Background(), repos, task)
	if err != nil {
		t.Fatal(err)
	}
	if source.Snapshot == nil || len(source.LegacyChunks) != 0 || len(source.Observations) != 2 || strings.Contains(source.Transcription.Content, "旧") {
		t.Fatalf("mixed sources: %+v", source)
	}
	compat, rows, err := taskTranscriptSource(repos, task)
	if err != nil || len(rows) != 0 || compat.SourceID != task.ActiveTextSourceID {
		t.Fatalf("compat=%+v rows=%v err=%v", compat, rows, err)
	}
	timeline := BuildVideoTimelineFromObservations(task.ID, source.Observations, nil)
	if len(timeline.Atoms) != 2 || timeline.Atoms[0].Source != textsource.KindSubtitle || timeline.Atoms[0].TimeRangeStatus != model.ChunkTimeRangeCoarse {
		t.Fatalf("subtitle timeline=%+v", timeline)
	}
	apiTimeline, err := (&MediaService{repo: repos}).GetVideoTimeline(context.Background(), task.UserID, task.ID)
	if err != nil || apiTimeline.AlignmentAvailable || !apiTimeline.StudySourceReady || len(apiTimeline.Atoms) != 2 {
		t.Fatalf("subtitle timeline API=%+v err=%v", apiTimeline, err)
	}
	ref := timeline.Atoms[0].SourceRefs[0]
	if ref.SourceID != task.ActiveTextSourceID || ref.SourceDigest != source.Snapshot.SourceDigest || ref.TimingMethod != textsource.TimingSubtitle || ref.MediaFingerprint != task.FileMD5 || len(ref.CueIDs) != 1 || ref.CueIDs[0] != source.Snapshot.Cues[0].ID || ref.StartMS != 10000 {
		t.Fatalf("lost provenance=%+v", ref)
	}
	index := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 800})
	chunks, err := index.loadTaskIndexChunks(task.UserID, task)
	if err != nil || len(chunks) != 1 || len(chunks[0].SourceRefs) != 2 {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
	if chunks[0].SourceRefs[0].StableID != ref.StableID || chunks[0].SourceRefs[0].TimingMethod != textsource.TimingSubtitle {
		t.Fatalf("index disagrees with timeline: %+v", chunks)
	}
	digest, items, err := artifactSource(context.Background(), repos, task.UserID, task.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("artifact=%+v err=%v", items, err)
	}
	if items[0].SourceIdentity != timeline.Atoms[0].ID || items[0].TextSourceID != ref.SourceID || items[0].TextSourceDigest != ref.SourceDigest || items[0].TimingMethod != textsource.TimingSubtitle || items[0].MediaFingerprint != task.FileMD5 || items[0].TimeRangeStatus != "coarse" {
		t.Fatalf("artifact lost source=%+v", items[0])
	}
	questions := &MediaService{repo: repos}
	before, err := questions.VideoQuestions(task.UserID, task.ID)
	if err != nil || len(before.Questions) == 0 || before.Questions[0].Source != "字幕" || before.Questions[0].TimeMS == nil || *before.Questions[0].TimeMS != 10000 {
		t.Fatalf("question timing=%+v err=%v", before, err)
	}
	task = publishReadFixture(t, repos, task, 12)
	changed, err := taskTextSource(context.Background(), repos, task)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Snapshot.CanonicalText != source.Snapshot.CanonicalText || changed.Snapshot.SourceDigest == source.Snapshot.SourceDigest || changed.Observations[0].Refs[0].StableID == ref.StableID {
		t.Fatal("timing-only refresh failed to change source identity")
	}
	updatedDigest, _, err := artifactSource(context.Background(), repos, task.UserID, task.ID)
	if err != nil || updatedDigest == digest {
		t.Fatalf("artifact refresh digest=%s err=%v", updatedDigest, err)
	}
	after, err := questions.VideoQuestions(task.UserID, task.ID)
	if err != nil || after.ContentVersion == before.ContentVersion {
		t.Fatalf("question evidence failed to change on refresh: %v", err)
	}
	missing := *task
	missing.ActiveTextSourceID = "missing-source"
	if invalid, readErr := taskTextSource(context.Background(), repos, &missing); readErr == nil || invalid != nil {
		t.Fatal("missing active source fell back to ASR")
	}
	// A captured task retains its original source even after refresh.
	old, err := repos.TextSource.Read(context.Background(), task.UserID, task.ID, source.Snapshot.ID)
	if err != nil || old.SourceDigest != source.Snapshot.SourceDigest {
		t.Fatalf("historical source=%+v err=%v", old, err)
	}
}

func TestUnifiedTextSourceTimingMethodsAndLegacyCompatibility(t *testing.T) {
	start, end := int64(1000), int64(2000)
	for _, test := range []struct {
		kind, method, status string
		timed                bool
	}{{textsource.KindSubtitle, textsource.TimingSubtitle, "coarse", true}, {textsource.KindASR, "forced_alignment", "exact", true}, {textsource.KindASR, "provider_segment", "exact", true}, {textsource.KindASR, "asr_window", "coarse", true}, {textsource.KindLegacy, textsource.TimingUnknown, "unknown", false}} {
		cue := textsource.Cue{ID: "cue-1", Text: "内容。", TimingMethod: test.method}
		if test.timed {
			cue.StartMS, cue.EndMS = &start, &end
		}
		observations := textSourceObservations(&textsource.Snapshot{ID: "source-a", Kind: test.kind, SourceDigest: "digest-a", Cues: []textsource.Cue{cue}})
		if observations[0].Refs[0].TimingMethod != test.method || observations[0].Refs[0].TimeRangeStatus != test.status {
			t.Fatalf("timing %+v => %+v", test, observations)
		}
	}
	row := timedSourceRow(t, "真实句子。", []model.TranscriptionSegment{{Text: "真实句子。", StartMS: 1000, EndMS: 2000, Method: "forced_alignment"}})
	observations := legacySourceObservations([]model.VideoTranscriptionChunk{row})
	if len(observations) != 1 || observations[0].Refs[0].TimingMethod != "forced_alignment" || observations[0].Refs[0].TimeRangeStatus != "exact" {
		t.Fatalf("legacy timing lost=%+v", observations)
	}
	repos := newRAGIndexTestRepositories(t)
	original := &model.VideoTask{UserID: 8, FileMD5: "legacy-file", Filename: "old.mp4"}
	legacy := &model.VideoTask{UserID: 7, FileMD5: original.FileMD5, Filename: "copy.mp4"}
	for _, task := range []*model.VideoTask{original, legacy} {
		if err := repos.Task.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: original.ID, FileMD5: original.FileMD5, Content: "旧全文。"}); err != nil {
		t.Fatal(err)
	}
	source, err := taskTextSource(context.Background(), repos, legacy)
	if err != nil || source.Transcription == nil || source.Transcription.Content != "旧全文。" || len(source.Observations) != 1 || source.Observations[0].Refs[0].TimeRangeStatus != "unknown" {
		t.Fatalf("legacy source=%+v err=%v", source, err)
	}
	scoped := *legacy
	scoped.ProcessingIntentJSON = "{}"
	private, err := taskTextSource(context.Background(), repos, &scoped)
	if err != nil || private.Transcription != nil || len(private.Observations) != 0 {
		t.Fatalf("new intent reused legacy source=%+v err=%v", private, err)
	}
}
