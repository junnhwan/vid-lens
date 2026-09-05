package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
)

type investigatorVisionClient struct {
	calls    int
	response string
}

func (c *investigatorVisionClient) CaptionImage(_ context.Context, _, _ string) (string, error) {
	c.calls++
	return c.response, nil
}

func TestVisualInvestigatorCapturesReplayableEvidenceWithinBudget(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "chart.mp4", FileURL: "videos/chart.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{{
		TaskID: task.ID, FrameIndex: 0, TimeMs: 1000, StartMS: 1000, EndMS: 1001,
		TimeStatus: model.ChunkTimeRangeExact, Source: "scene", OCRText: "图表", Status: model.VisualFrameStatusCompleted,
	}}); err != nil {
		t.Fatal(err)
	}

	vision := &investigatorVisionClient{response: `{"facts":["图表显示同比增长 20%"],"gaps":[]}`}
	uploadedKeys := make([]string, 0)
	inv := NewVisualInvestigator(repos, nil, "ffmpeg")
	inv.SetVideoDownloader(func(_ context.Context, _ string) (string, error) {
		path := filepath.Join(t.TempDir(), "source.mp4")
		if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
			return "", err
		}
		return path, nil
	})
	inv.SetFrameMaterializer(func(_ context.Context, _, _ string, times []int64) ([]ffmpeg.KeyFrame, string, error) {
		workDir := t.TempDir()
		frames := make([]ffmpeg.KeyFrame, 0, len(times))
		for index, timeMS := range times {
			path := filepath.Join(workDir, "frame-"+string(rune('a'+index))+".jpg")
			if err := os.WriteFile(path, []byte("frame-content"), 0o600); err != nil {
				return nil, "", err
			}
			frames = append(frames, ffmpeg.KeyFrame{Path: path, TimeMs: timeMS, Source: "query", Args: []string{"-ss", "query-time"}})
		}
		return frames, workDir, nil
	})
	inv.SetFrameUploader(func(_ context.Context, path, objectKey, _ string) (int64, error) {
		uploadedKeys = append(uploadedKeys, objectKey)
		info, err := os.Stat(path)
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	})
	inv.SetVisionResolver(func(context.Context, int64) (ai.VisionClient, error) { return vision, nil })
	inv.SetVisionModelResolver(func(context.Context, int64) (string, error) { return "vision-test", nil })

	result, err := inv.Inspect(context.Background(), InspectRequest{
		UserID: 7, TaskID: task.ID, Goal: "图表显示同比增长多少？",
		RequiredFacts: []RequiredFact{{Name: "同比增长"}},
		SeedWindows:   []VisualTimeRange{{StartMS: 900, EndMS: 1300}},
		Budget:        VisualBudget{MaxFrames: 2, MaxVLMCalls: 1, MaxWindows: 1, MaxWindowMS: 1000, MaxTotalMS: 1000},
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if result.Status != "budget_exhausted" || !result.Budget.BudgetExhausted {
		t.Fatalf("status/budget = %+v", result)
	}
	if result.Budget.FramesCaptured != 2 || result.Budget.VLMCalls != 1 || result.Budget.BytesUploaded == 0 {
		t.Fatalf("budget usage = %+v", result.Budget)
	}
	if len(result.Observations) != 2 || len(uploadedKeys) != 2 || vision.calls != 1 {
		t.Fatalf("observations=%+v uploaded=%v vision_calls=%d", result.Observations, uploadedKeys, vision.calls)
	}
	if result.Observations[0].Source != queryVisualSource || result.Observations[0].ArtifactKind != model.VisualArtifactKindFrame || result.Observations[0].ObjectKey == "" || result.Observations[0].Model != "vision-test" || result.Observations[0].EndMS != result.Observations[0].StartMS+1 || len(result.Observations[0].FFmpegArgs) != 2 {
		t.Fatalf("observation provenance = %+v", result.Observations[0])
	}
	if len(result.ClaimBindings) != 1 || result.ClaimBindings[0].Status != "unverified" {
		t.Fatalf("claim bindings = %+v", result.ClaimBindings)
	}
	rows, err := repos.VisualObservation.ListByTaskID(context.Background(), 7, task.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("persisted observations = %+v, err=%v", rows, err)
	}
	provenanceFound := false
	for _, row := range rows {
		if row.CapturePolicyVersion == queryVisualCapturePolicyVersion && row.PromptVersion == queryVisualPromptVersion && row.RawResponseHash != "" && row.FFmpegArgs != "" {
			provenanceFound = true
			break
		}
	}
	if !provenanceFound {
		t.Fatalf("persisted provenance = %+v", rows)
	}
}

func TestVisualInvestigatorReusesAppendOnlyObservationCache(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "cache.mp4", FileURL: "videos/cache.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{{
		TaskID: task.ID, FrameIndex: 0, TimeMs: 1000, StartMS: 1000, EndMS: 1001,
		TimeStatus: model.ChunkTimeRangeExact, Source: "scene", OCRText: "anchor", Status: model.VisualFrameStatusCompleted,
	}}); err != nil {
		t.Fatal(err)
	}
	vision := &investigatorVisionClient{response: `{"facts":["可见按钮"],"gaps":[]}`}
	uploads := 0
	inv := NewVisualInvestigator(repos, nil, "ffmpeg")
	inv.SetVideoDownloader(func(_ context.Context, _ string) (string, error) {
		path := filepath.Join(t.TempDir(), "source.mp4")
		return path, os.WriteFile(path, []byte("source"), 0o600)
	})
	inv.SetFrameMaterializer(func(_ context.Context, _, _ string, times []int64) ([]ffmpeg.KeyFrame, string, error) {
		workDir := t.TempDir()
		path := filepath.Join(workDir, "frame.jpg")
		if err := os.WriteFile(path, []byte("same-frame"), 0o600); err != nil {
			return nil, "", err
		}
		return []ffmpeg.KeyFrame{{Path: path, TimeMs: times[0], Source: "query"}}, workDir, nil
	})
	inv.SetFrameUploader(func(_ context.Context, _, _ string, _ string) (int64, error) { uploads++; return 10, nil })
	inv.SetVisionResolver(func(context.Context, int64) (ai.VisionClient, error) { return vision, nil })

	req := InspectRequest{UserID: 7, TaskID: task.ID, Goal: "按钮是什么？", SeedWindows: []VisualTimeRange{{StartMS: 900, EndMS: 1100}}, Budget: VisualBudget{MaxWindows: 1, MaxFrames: 1, MaxVLMCalls: 1, MaxWindowMS: 1000, MaxTotalMS: 1000}}
	first, err := inv.Inspect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := inv.Inspect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Budget.FramesCaptured != 1 || second.Budget.FramesReused != 1 || second.Budget.VLMCalls != 0 || vision.calls != 1 || uploads != 1 {
		t.Fatalf("first=%+v second=%+v vision_calls=%d uploads=%d", first.Budget, second.Budget, vision.calls, uploads)
	}
	rows, err := repos.VisualObservation.ListByTaskID(context.Background(), 7, task.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("cache rows = %+v, err=%v", rows, err)
	}
}

func TestVisualInvestigatorRejectsUnlocatedSeedWindow(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "cccccccccccccccccccccccccccccccc", Filename: "unlocated.mp4", FileURL: "videos/unlocated.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	inv := NewVisualInvestigator(repos, nil, "ffmpeg")
	_, err := inv.Inspect(context.Background(), InspectRequest{
		UserID: 7, TaskID: task.ID, Goal: "看画面", SeedWindows: []VisualTimeRange{{StartMS: 10_000, EndMS: 11_000}},
	})
	if err == nil || !containsAny(err.Error(), "not covered", "未覆盖", "existing video evidence") {
		t.Fatalf("error = %v, want fail-closed location validation", err)
	}
}
