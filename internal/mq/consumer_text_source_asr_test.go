package mq

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
	"vid-lens/internal/transcript"
)

func asrFixtureRow(index int, text string, segments ...model.TranscriptionSegment) model.VideoTranscriptionChunk {
	raw, _ := json.Marshal(segments)
	return model.VideoTranscriptionChunk{TaskID: 1, ChunkIndex: index, Status: model.TranscriptionChunkStatusCompleted, Content: text, WindowStartMS: int64(index) * 10000, WindowEndMS: int64(index+1) * 10000, TimedSegments: string(raw)}
}

func TestASRSourceNativeMixedGapsAndStableDigest(t *testing.T) {
	row := asrFixtureRow(0, "第一句。 未对齐 末句。", model.TranscriptionSegment{Text: "第一句。", TextStart: 0, TextEnd: 4, StartMS: 1100, EndMS: 2300, Method: "asr_native"}, model.TranscriptionSegment{Text: "末句。", TextStart: 9, TextEnd: 12, StartMS: 7500, EndMS: 8700, Method: "asr_native"})
	task := &model.VideoTask{ID: 1, FileMD5: "media"}
	source, err := asrTextSourceSnapshot(task, row.Content, []model.VideoTranscriptionChunk{row})
	if err != nil {
		t.Fatal(err)
	}
	if source.CanonicalText != row.Content || len(source.Cues) != 3 {
		t.Fatalf("wording lost: %+v", source)
	}
	if *source.Cues[0].StartMS != 1100 || *source.Cues[2].EndMS != 8700 || source.Cues[1].TimingMethod != "asr_window" || len(source.Warnings) == 0 {
		t.Fatalf("precision lost: %+v", source)
	}
	if source.Cues[0].RawRefs[0].RawText != row.Content || source.Cues[1].RawRefs[0].RawText != "" || *source.Cues[2].RawRefs[0].TextStart != 9 {
		t.Fatal("original window/rune mapping lost")
	}
	row.ID = 999
	retry, err := asrTextSourceSnapshot(task, row.Content, []model.VideoTranscriptionChunk{row})
	if err != nil || retry.SourceDigest != source.SourceDigest {
		t.Fatal("database metadata changed immutable evidence digest")
	}
	var changed []model.TranscriptionSegment
	_ = json.Unmarshal([]byte(row.TimedSegments), &changed)
	changed[0].StartMS++
	b, _ := json.Marshal(changed)
	row.TimedSegments = string(b)
	retry, err = asrTextSourceSnapshot(task, row.Content, []model.VideoTranscriptionChunk{row})
	if err != nil || retry.SourceDigest == source.SourceDigest {
		t.Fatal("native timing omitted from evidence digest")
	}
}

func TestASRSourceRepeatedQuotesRemainWindowAndUnknownRemainsNull(t *testing.T) {
	row := asrFixtureRow(0, "重复 重复", model.TranscriptionSegment{Text: "重复", StartMS: 1000, EndMS: 1500})
	task := &model.VideoTask{ID: 1, FileMD5: "media"}
	source, err := asrTextSourceSnapshot(task, row.Content, []model.VideoTranscriptionChunk{row})
	if err != nil || len(source.Cues) != 1 || source.Cues[0].TimingMethod != "asr_window" || len(source.Warnings) != 1 {
		t.Fatalf("ambiguous occurrence got timestamp: %+v %v", source, err)
	}
	row.WindowEndMS = 0
	source, err = asrTextSourceSnapshot(task, row.Content, []model.VideoTranscriptionChunk{row})
	if err != nil || source.Cues[0].StartMS != nil || source.Cues[0].EndMS != nil || source.Cues[0].TimingMethod != textsource.TimingUnknown {
		t.Fatalf("unknown timing fabricated: %+v %v", source, err)
	}
	row.Status = model.TranscriptionChunkStatusFailed
	if _, err := asrTextSourceSnapshot(task, row.Content, []model.VideoTranscriptionChunk{row}); err == nil {
		t.Fatal("incomplete observation published")
	}
}

func asrAlignedFixtureRow(index int, text string, windowStart, windowEnd, coreStart, coreEnd int64, times [][2]int64) model.VideoTranscriptionChunk {
	var words []model.TranscriptionSegment
	for i, r := range []rune(text) {
		words = append(words, model.TranscriptionSegment{Text: string(r), TextStart: i, TextEnd: i + 1, StartMS: times[i][0], EndMS: times[i][1], Method: "forced_alignment"})
	}
	row := asrFixtureRow(index, text, words...)
	row.SegmentKey, row.SegmenterVersion = text, "v2"
	row.WindowStartMS, row.WindowEndMS, row.CoreStartMS, row.CoreEndMS = windowStart, windowEnd, coreStart, coreEnd
	return row
}

func TestASRSourceAlignedOverlapPreservesNativeProvenanceAndRepeatedSpeech(t *testing.T) {
	rows := []model.VideoTranscriptionChunk{
		asrAlignedFixtureRow(0, "甲甲", 0, 22000, 0, 20000, [][2]int64{{1000, 2000}, {19000, 19500}}),
		asrAlignedFixtureRow(1, "甲甲乙", 18000, 42000, 20000, 40000, [][2]int64{{19000, 19500}, {21000, 21500}, {30000, 31000}}),
	}
	content := transcript.Assemble(rows).Content
	source, err := asrTextSourceSnapshot(&model.VideoTask{ID: 1, FileMD5: "media"}, content, rows)
	if err != nil {
		t.Fatal(err)
	}
	if source.CanonicalText != "甲甲甲乙" || len(source.Cues) < 2 {
		t.Fatalf("repeated speech changed: %+v", source)
	}
	var retainedSecond bool
	for _, cue := range source.Cues {
		if cue.TimingMethod != "forced_alignment" || cue.JoinBefore == nil || *cue.JoinBefore != "" {
			t.Fatalf("native timing/joins lost: %+v", cue)
		}
		ref := cue.RawRefs[0]
		if ref.ObservationOrder == 2 && *ref.TextStart == 1 && len(ref.NativeTimings) > 0 {
			retainedSecond = true
		}
	}
	if !retainedSecond {
		t.Fatal("overlap retained offset or native provenance lost")
	}
}

func TestASRSourceAlignedWordsGroupSentencesPreservingAllNativeIntervals(t *testing.T) {
	row := asrAlignedFixtureRow(0, "甲乙。丙丁！", 0, 10000, 0, 10000, [][2]int64{{100, 200}, {200, 300}, {300, 350}, {400, 500}, {500, 600}, {600, 650}})
	source, err := asrTextSourceSnapshot(&model.VideoTask{ID: 1, FileMD5: "media"}, row.Content, []model.VideoTranscriptionChunk{row})
	if err != nil || source.CanonicalText != row.Content || len(source.Cues) != 2 {
		t.Fatalf("sentence grouping: %+v %v", source, err)
	}
	if len(source.Cues[0].RawRefs[0].NativeTimings) != 3 || len(source.Cues[1].RawRefs[0].NativeTimings) != 3 {
		t.Fatal("original acoustic intervals lost")
	}
}

func TestASRSourcePublicationCompletesLeaseFreezesSummaryAndFencesReplay(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "handoff", true: "rollback"}[rollback], func(t *testing.T) {
			c, repos, db, task, payload, _, producer, _ := sourceWorkerFixture(t, true, "force_asr")
			if err := c.handleTextSource(context.Background(), payload); err != nil {
				t.Fatal(err)
			}
			claim, err := c.claimTaskForMessage(task.ID, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, producer.token)
			if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
				t.Fatalf("ASR claim %+v %v", claim, err)
			}
			current, _ := repos.Task.FindByID(task.ID)
			intent, _ := processing.Decode(current.ProcessingIntentJSON)
			row := asrFixtureRow(0, "真实音频文字。", model.TranscriptionSegment{Text: "真实音频文字。", TextStart: 0, TextEnd: 7, StartMS: 1000, EndMS: 2000})
			row.TaskID = task.ID
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			ctx := withProcessingLeaseOwner(context.Background(), &processingLeaseOwner{repos: repos, taskID: task.ID, jobType: model.TaskJobTypeTranscribe, token: claim.Token, now: c.currentTime})
			if rollback {
				if err := db.Migrator().DropTable(&model.SummaryPart{}); err != nil {
					t.Fatal(err)
				}
			}
			err = c.publishAutomaticASRSource(ctx, current, intent, claim.Token, row.Content)
			after, _ := repos.Task.FindByID(task.ID)
			if rollback {
				if err == nil || after.ActiveTextSourceID != "" || after.ProcessingToken != claim.Token || producer.summaryCalls != 0 {
					t.Fatalf("publication not rolled back %+v %v", after, err)
				}
				var n int64
				db.Model(&model.VideoTextSource{}).Count(&n)
				if n != 0 {
					t.Fatal("uncommitted source leaked")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			source, err := repos.TextSource.Active(context.Background(), task.UserID, task.ID)
			if err != nil || source.Kind != textsource.KindASR || source.CanonicalText != row.Content || after.ProcessingToken != "" || after.Status != model.TaskStatusCompleted {
				t.Fatalf("ASR not published %+v %+v %v", source, after, err)
			}
			job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
			if job == nil || job.InputSourceID != after.ActiveTextSourceID || !strings.Contains(job.InputSnapshotJSON, source.SourceDigest) || producer.summaryCalls != 1 {
				t.Fatalf("summary input not frozen %+v", job)
			}
			if err := c.publishAutomaticASRSource(ctx, current, intent, claim.Token, row.Content); err == nil || producer.summaryCalls != 1 {
				t.Fatal("released ASR lease overwrote source or dispatched again")
			}
		})
	}
}

func TestASRSourceRetainsOnlyActualAssemblyRowsWhenOldRecipeRowsRemain(t *testing.T) {
	c, repos, db, task, _, _, _, _ := sourceWorkerFixture(t, false, "force_asr")
	stale := asrFixtureRow(99, "旧分段配置留下的文字")
	stale.TaskID = task.ID
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	strategy := &recordingAI{transcripts: map[string]string{"fixture-audio": "本次真正识别的文字"}}
	c.splitAudioWindows = func(context.Context, string, string, int, int) ([]ffmpeg.AudioSegment, string, error) {
		return []ffmpeg.AudioSegment{{Path: "fixture-audio", WindowStartMS: 0, WindowEndMS: 10000, CoreStartMS: 0, CoreEndMS: 10000, SegmentKey: "current-recipe", Version: ffmpeg.AudioSegmenterVersion}}, "", nil
	}
	var retained []model.VideoTranscriptionChunk
	content, err := c.transcription().transcribeAudio(context.Background(), task.ID, "unused-fixture", strategy, &retained)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := repos.TranscriptionChunk.ListByTaskID(task.ID)
	if len(all) != 2 || len(retained) != 1 || retained[0].SegmentKey != "current-recipe" {
		t.Fatalf("wrong retained rows: %+v all %+v", retained, all)
	}
	source, err := asrTextSourceSnapshot(&task, content, retained)
	if err != nil || source.CanonicalText != "本次真正识别的文字" || strings.Contains(source.CanonicalText, "旧分段") {
		t.Fatalf("stale recognition entered source %+v %v", source, err)
	}
}
