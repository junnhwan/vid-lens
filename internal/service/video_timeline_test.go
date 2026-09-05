package service

import (
	"context"
	"testing"

	"vid-lens/internal/model"
)

func TestMediaServiceGetVideoTimelineScopesOwnerAndKeepsLegacyTranscript(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "timeline.mp4", FileURL: "videos/timeline.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "旧任务的完整转写", Words: 7}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}

	svc := &MediaService{repo: repos}
	timeline, err := svc.GetVideoTimeline(context.Background(), 7, task.ID)
	if err != nil {
		t.Fatalf("GetVideoTimeline: %v", err)
	}
	if timeline.Title != task.Filename || len(timeline.Atoms) != 1 || timeline.Atoms[0].Content != "旧任务的完整转写" {
		t.Fatalf("timeline = %#v", timeline)
	}
	if timeline.Atoms[0].TimeRangeStatus != model.ChunkTimeRangeUnknown {
		t.Fatalf("legacy timeline range = %#v, want unknown", timeline.Atoms[0])
	}
	if _, err := svc.GetVideoTimeline(context.Background(), 8, task.ID); err == nil {
		t.Fatal("cross-owner timeline read should fail")
	}
}

func TestBuildVideoTimelineKeepsModalitiesTimesAndSourceRefs(t *testing.T) {
	timeline := BuildVideoTimeline(42,
		[]model.VideoTranscriptionChunk{
			{ID: 7, TaskID: 42, ChunkIndex: 1, SegmentKey: "seg-1", Status: model.TranscriptionChunkStatusCompleted,
				Content: "第二段字幕", CoreStartMS: 2000, CoreEndMS: 5000, WindowStartMS: 0, WindowEndMS: 7000},
			{ID: 8, TaskID: 42, ChunkIndex: 2, Status: model.TranscriptionChunkStatusFailed, Content: "不应公开"},
		},
		[]model.VideoVisualFrame{
			{ID: 9, TaskID: 42, FrameIndex: 0, FrameKey: "frame-1", TimeMs: 1000, StartMS: 1000, EndMS: 1001,
				ObjectKey: "visual-frames/task-42/frame.jpg", Source: "scene", Status: model.VisualFrameStatusCompleted,
				OCRText: "画面标题", VisionCaption: "一张幻灯片"},
			{ID: 10, TaskID: 42, FrameIndex: 1, TimeMs: 8000, Status: model.VisualFrameStatusFailed, OCRText: "不应公开"},
		},
	)

	if len(timeline.Atoms) != 3 {
		t.Fatalf("atoms = %#v, want transcript + OCR + caption", timeline.Atoms)
	}
	if timeline.Atoms[0].Modality != model.ChunkModalityVisualOCR || timeline.Atoms[0].StartMS != 1000 {
		t.Fatalf("first atom = %#v, want earliest visual OCR", timeline.Atoms[0])
	}
	transcript := timeline.Atoms[2]
	if transcript.Modality != model.ChunkModalityTranscript || transcript.StartMS != 2000 || transcript.EndMS != 5000 {
		t.Fatalf("transcript atom = %#v, want core-owned coarse range", transcript)
	}
	if transcript.SourceRefs[0].StableID != "seg-1" || transcript.SourceRefs[0].TimeRangeStatus != model.ChunkTimeRangeCoarse {
		t.Fatalf("transcript source ref = %#v", transcript.SourceRefs)
	}
	if timeline.Atoms[1].SourceRefs[0].ObjectKey != "visual-frames/task-42/frame.jpg" {
		t.Fatalf("visual source ref lost object key: %#v", timeline.Atoms[1].SourceRefs)
	}
}

func TestBuildVideoTimelineDoesNotInventUnknownTime(t *testing.T) {
	timeline := BuildVideoTimeline(1, []model.VideoTranscriptionChunk{{
		ChunkIndex: 0, Status: model.TranscriptionChunkStatusCompleted, Content: "没有时间戳的转写",
	}}, nil)
	if len(timeline.Atoms) != 1 {
		t.Fatalf("atoms = %#v", timeline.Atoms)
	}
	atom := timeline.Atoms[0]
	if atom.StartMS != 0 || atom.EndMS != 0 || atom.TimeRangeStatus != model.ChunkTimeRangeUnknown {
		t.Fatalf("atom = %#v, want unknown range", atom)
	}
}
