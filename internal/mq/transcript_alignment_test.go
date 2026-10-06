package mq

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"unicode"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type testTranscriptAligner func(context.Context, string, []model.VideoTranscriptionChunk) ([]model.VideoTranscriptionChunk, error)

func (f testTranscriptAligner) Align(ctx context.Context, audio string, rows []model.VideoTranscriptionChunk) ([]model.VideoTranscriptionChunk, error) {
	return f(ctx, audio, rows)
}

func fakeCompleteAlignment(_ context.Context, _ string, rows []model.VideoTranscriptionChunk) ([]model.VideoTranscriptionChunk, error) {
	out := append([]model.VideoTranscriptionChunk(nil), rows...)
	for i, row := range out {
		var words []model.TranscriptionSegment
		for j, r := range []rune(row.Content) {
			if unicode.IsLetter(r) {
				words = append(words, model.TranscriptionSegment{Text: string(r), TextStart: j, TextEnd: j + 1, StartMS: row.WindowStartMS + int64(j)*100, EndMS: row.WindowStartMS + int64(j+1)*100, Method: "forced_alignment"})
			}
		}
		b, _ := json.Marshal(words)
		out[i].TimedSegments = string(b)
	}
	return out, nil
}

func seedAlignmentRows(t *testing.T, repos *repository.Repositories, taskID int64) {
	t.Helper()
	for i, content := range []string{"第一句。", "第二句。"} {
		if err := repos.TranscriptionChunk.UpsertCompletedWithTimeline(taskID, i, "audio", content, repository.TranscriptionChunkTimeline{SegmentKey: content, SegmenterVersion: "v", WindowStartMS: int64(i) * 2000, WindowEndMS: int64(i+1) * 2000, CoreStartMS: int64(i) * 2000, CoreEndMS: int64(i+1) * 2000}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAlignExistingTranscriptPublishesVerbatimTimingAndCorpusWithoutASR(t *testing.T) {
	repos, c, task, _, _ := newTranscribeCompletionFixture(t)
	seedAlignmentRows(t, repos, task.ID)
	c.transcriptAligner = testTranscriptAligner(fakeCompleteAlignment)
	// No AI strategy exists: an alignment-only retry must never invoke ASR.
	text, err := c.transcription().alignExistingTranscript(context.Background(), task.ID, "audio")
	if err != nil || text != "第一句。\n\n第二句。" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	current, _ := repos.Transcription.FindByTaskID(task.ID)
	rows, _ := repos.TranscriptionChunk.ListByTaskID(task.ID)
	if current.Content != text || rows[0].TimedSegments == "" || rows[1].TimedSegments == "" || rows[0].Content != "第一句。" {
		t.Fatal("timing and corpus not published together")
	}
}

func TestAlignmentPublicationRollbackKeepsPreviouslyPublishedEvidence(t *testing.T) {
	repos, db := newConsumerLoopTestRepositories(t)
	task := &model.VideoTask{UserID: 1, FileMD5: "alignment-rollback", Status: model.TaskStatusCompleted, Stage: model.TaskStageNone}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	seedAlignmentRows(t, repos, task.ID)
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "previous"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_alignment_publication BEFORE UPDATE ON video_transcriptions BEGIN SELECT RAISE(ABORT, 'publication failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	c := &Consumer{repo: repos, transcriptAligner: testTranscriptAligner(fakeCompleteAlignment)}
	if _, err := c.transcription().alignExistingTranscript(context.Background(), task.ID, "audio"); err == nil {
		t.Fatal("fault did not reach publication")
	}
	current, _ := repos.Transcription.FindByTaskID(task.ID)
	rows, _ := repos.TranscriptionChunk.ListByTaskID(task.ID)
	if current.Content != "previous" || rows[0].TimedSegments != "" || rows[1].TimedSegments != "" {
		t.Fatal("failed publication leaked replacement source evidence")
	}
}

func TestAlignmentFailureAndInvalidResultPreserveCompletedASR(t *testing.T) {
	for _, name := range []string{"failure", "cancel", "edited", "window", "missing"} {
		t.Run(name, func(t *testing.T) {
			repos, c, task, _, _ := newTranscribeCompletionFixture(t)
			seedAlignmentRows(t, repos, task.ID)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.transcriptAligner = testTranscriptAligner(func(ctx context.Context, audio string, rows []model.VideoTranscriptionChunk) ([]model.VideoTranscriptionChunk, error) {
				if name == "failure" {
					return nil, errors.New("model failed")
				}
				aligned, err := fakeCompleteAlignment(ctx, audio, rows)
				switch name {
				case "cancel":
					cancel()
				case "edited":
					aligned[0].Content = "被改写"
				case "window":
					aligned[0].CoreEndMS++
				case "missing":
					aligned[0].TimedSegments = "[]"
				}
				return aligned, err
			})
			if _, err := c.transcription().alignExistingTranscript(ctx, task.ID, "audio"); err == nil {
				t.Fatal("invalid alignment accepted")
			}
			current, _ := repos.Transcription.FindByTaskID(task.ID)
			rows, _ := repos.TranscriptionChunk.ListByTaskID(task.ID)
			if current.Content != "transcript" || rows[0].Status != "completed" || rows[0].TimedSegments != "" {
				t.Fatal("failed alignment discarded published evidence or ASR reuse")
			}
		})
	}
}
