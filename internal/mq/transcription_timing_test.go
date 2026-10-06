package mq

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
)

type timedRecordingASR struct {
	recordingAI
	segments map[string][]model.TranscriptionSegment
}

func (s *timedRecordingASR) TranscribeDetailed(ctx context.Context, path string) (ai.TranscriptionResult, error) {
	text, err := s.recordingAI.Transcribe(ctx, path)
	return ai.TranscriptionResult{Text: text, Segments: s.segments[path]}, err
}

func TestTranscribeAudioPersistsValidatedAbsoluteTimingAndReusesIt(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	strategy := &timedRecordingASR{
		recordingAI: recordingAI{transcripts: map[string]string{"timed.mp3": "第一句。第二句。"}},
		segments: map[string][]model.TranscriptionSegment{"timed.mp3": {
			{Text: "第一句。", StartMS: 500, EndMS: 1500},
			{Text: "负数", StartMS: -1, EndMS: 2},
			{Text: "超出窗口", StartMS: 2000, EndMS: 25000},
			{Text: "第二句。", StartMS: 2000, EndMS: 3000},
			{Text: "逆序", StartMS: 1000, EndMS: 1200},
		}},
	}
	consumer := &Consumer{repo: repos, ai: strategy,
		splitAudioWindows: func(_ context.Context, _, _ string, seconds, overlap int) ([]ffmpeg.AudioSegment, string, error) {
			if seconds != 20 || overlap != 2 {
				t.Fatalf("production bounds=%d/%d", seconds, overlap)
			}
			return []ffmpeg.AudioSegment{{Path: "timed.mp3", WindowStartMS: 18000, WindowEndMS: 42000, CoreStartMS: 20000, CoreEndMS: 40000, SegmentKey: "timed-window", Version: ffmpeg.AudioSegmenterVersion}}, "", nil
		},
	}
	result, err := consumer.transcribeAudio(context.Background(), 909, "audio.mp3", strategy)
	if err != nil || result != "第一句。第二句。" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	stored, err := repos.TranscriptionChunk.FindByTaskAndIndex(909, 0)
	if err != nil || stored == nil {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	var segments []model.TranscriptionSegment
	if err := json.Unmarshal([]byte(stored.TimedSegments), &segments); err != nil {
		t.Fatal(err)
	}
	want := []model.TranscriptionSegment{{Text: "第一句。", StartMS: 18500, EndMS: 19500}, {Text: "第二句。", StartMS: 20000, EndMS: 21000}}
	if !reflect.DeepEqual(segments, want) {
		t.Fatalf("segments=%+v want=%+v", segments, want)
	}
	if stored.WindowStartMS != 18000 || stored.WindowEndMS != 42000 {
		t.Fatalf("coarse range lost=%+v", stored)
	}
	if _, err := consumer.transcribeAudio(context.Background(), 909, "audio.mp3", strategy); err != nil {
		t.Fatal(err)
	}
	if len(strategy.transcribeInput) != 1 {
		t.Fatalf("completed timing was retranscribed: inputs=%v", strategy.transcribeInput)
	}
	resumed, err := repos.TranscriptionChunk.FindByTaskAndIndex(909, 0)
	if err != nil || resumed.TimedSegments != stored.TimedSegments {
		t.Fatalf("resume lost timing=%+v err=%v", resumed, err)
	}
}

func TestTranscribeAudioKeepsCoarseRangeForTextOnlyProvider(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	strategy := &recordingAI{transcripts: map[string]string{"plain.mp3": "没有真实时间戳。"}}
	consumer := &Consumer{repo: repos, splitAudioWindows: func(context.Context, string, string, int, int) ([]ffmpeg.AudioSegment, string, error) {
		return []ffmpeg.AudioSegment{{Path: "plain.mp3", WindowEndMS: 22000, CoreEndMS: 20000, SegmentKey: "plain-window", Version: ffmpeg.AudioSegmenterVersion}}, "", nil
	}}
	consumer.transcriptAligner = testTranscriptAligner(func(context.Context, string, []model.VideoTranscriptionChunk) ([]model.VideoTranscriptionChunk, error) {
		t.Fatal("ordinary transcription must not invoke an installed local model")
		return nil, nil
	})
	if _, err := consumer.transcribeAudio(context.Background(), 910, "audio.mp3", strategy); err != nil {
		t.Fatal(err)
	}
	stored, err := repos.TranscriptionChunk.FindByTaskAndIndex(910, 0)
	if err != nil || stored.TimedSegments != "" || stored.WindowEndMS != 22000 {
		t.Fatalf("invented/invalid timing=%+v err=%v", stored, err)
	}
	if got := absoluteTranscriptionSegments([]model.TranscriptionSegment{{Text: "unverified", StartMS: 0, EndMS: 1000}}, ffmpeg.AudioSegment{}); len(got) != 0 {
		t.Fatalf("unknown audio bounds accepted=%v", got)
	}
}

func TestTranscribeAudioCompletesAndReusesSilentWindowBetweenSpeech(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	strategy := &recordingAI{transcripts: map[string]string{"first.mp3": "重复的完整句子。", "silent.mp3": "", "last.mp3": "重复的完整句子。"}}
	windows := []ffmpeg.AudioSegment{
		{Path: "first.mp3", WindowEndMS: 22000, CoreEndMS: 20000, SegmentKey: "first", Version: ffmpeg.AudioSegmenterVersion},
		{Path: "silent.mp3", WindowStartMS: 18000, WindowEndMS: 42000, CoreStartMS: 20000, CoreEndMS: 40000, SegmentKey: "silent", Version: ffmpeg.AudioSegmenterVersion},
		{Path: "last.mp3", WindowStartMS: 38000, WindowEndMS: 60000, CoreStartMS: 40000, CoreEndMS: 60000, SegmentKey: "last", Version: ffmpeg.AudioSegmenterVersion},
	}
	consumer := &Consumer{repo: repos, splitAudioWindows: func(context.Context, string, string, int, int) ([]ffmpeg.AudioSegment, string, error) {
		return windows, "", nil
	}}
	want := "重复的完整句子。\n\n重复的完整句子。"
	for attempt := 0; attempt < 2; attempt++ {
		result, err := consumer.transcribeAudio(context.Background(), 911, "audio.mp3", strategy)
		if err != nil || result != want {
			t.Fatalf("attempt=%d result=%q err=%v", attempt, result, err)
		}
	}
	rows, err := repos.TranscriptionChunk.ListByTaskID(911)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[1].Status != model.TranscriptionChunkStatusCompleted || rows[1].Content != "" || rows[1].TimedSegments != "" {
		t.Fatalf("silent checkpoint not completed=%+v", rows[1])
	}
	if len(strategy.transcribeInput) != 3 {
		t.Fatalf("silent or completed window replayed=%v", strategy.transcribeInput)
	}
}

func TestTranscribeAudioRejectsWholeSilentVideoAfterCompletingWindow(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	strategy := &recordingAI{transcripts: map[string]string{"silent.mp3": ""}}
	consumer := &Consumer{repo: repos, splitAudioWindows: func(context.Context, string, string, int, int) ([]ffmpeg.AudioSegment, string, error) {
		return []ffmpeg.AudioSegment{{Path: "silent.mp3", WindowEndMS: 10000, CoreEndMS: 10000, SegmentKey: "all-silent", Version: ffmpeg.AudioSegmenterVersion}}, "", nil
	}}
	if _, err := consumer.transcribeAudio(context.Background(), 912, "audio.mp3", strategy); err == nil {
		t.Fatal("whole silent video accepted as transcript")
	}
	row, err := repos.TranscriptionChunk.FindByTaskAndIndex(912, 0)
	if err != nil || row == nil || row.Status != model.TranscriptionChunkStatusCompleted {
		t.Fatalf("successful silent window lost=%+v err=%v", row, err)
	}
}
