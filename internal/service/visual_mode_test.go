package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/ffmpeg"
)

type modeVisualStore struct {
	path      string
	downloads int
}

func (s *modeVisualStore) DownloadToTemp(context.Context, string) (string, error) {
	s.downloads++
	return s.path, nil
}
func (s *modeVisualStore) UploadFromPath(context.Context, string, string, string) (int64, error) {
	return 1, nil
}

func TestVisualBuildCallsOnlySelectedProvidersAndIndexesOnlyTheirObservations(t *testing.T) {
	for _, mode := range []string{model.VisualModeOff, model.VisualModeOCR, model.VisualModeCaption, model.VisualModeBoth} {
		t.Run(mode, func(t *testing.T) {
			repos := newMediaTestRepositories(t)
			task := &model.VideoTask{UserID: 7, FileMD5: "mode-" + mode, Filename: "slides.mp4", FileURL: "videos/slides", VisualMode: mode}
			if err := repos.Task.Create(task); err != nil {
				t.Fatal(err)
			}
			videoPath := filepath.Join(t.TempDir(), "video.mp4")
			if err := os.WriteFile(videoPath, []byte("video"), 0600); err != nil {
				t.Fatal(err)
			}
			store := &modeVisualStore{path: videoPath}
			svc := NewVisualIndexService(repos, nil, "ffmpeg", DefaultVisualIndexConfig())
			svc.storage = store
			svc.extract = func(context.Context, string, string, ffmpeg.ExtractKeyFramesOptions) ([]ffmpeg.KeyFrame, string, error) {
				return []ffmpeg.KeyFrame{{Path: "frame.jpg", TimeMs: 1000, Source: "interval"}}, "", nil
			}
			ocrCalls, ocrChecks, visionResolves := 0, 0, 0
			svc.ocrAvailable = func(context.Context) bool { ocrChecks++; return true }
			svc.recognizeOCR = func(context.Context, string) (string, error) { ocrCalls++; return "课件上的文字", nil }
			vision := &investigatorVisionClient{response: "图表的趋势"}
			svc.SetVisionResolver(func(context.Context, int64) (ai.VisionClient, error) { visionResolves++; return vision, nil })
			count, err := svc.BuildTaskVisualIndex(context.Background(), task)
			if err != nil {
				t.Fatal(err)
			}
			wantOCR, wantCaption := 0, 0
			if task.VisualOCRAllowed() {
				wantOCR = 1
			}
			if task.VisualCaptionAllowed() {
				wantCaption = 1
			}
			if ocrCalls != wantOCR || ocrChecks != wantOCR || visionResolves != wantCaption || vision.calls != wantCaption {
				t.Fatalf("provider calls OCR=%d checks=%d vision resolve=%d calls=%d", ocrCalls, ocrChecks, visionResolves, vision.calls)
			}
			frames, err := repos.VisualFrame.ListByTaskID(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			chunks := FormatOCRChunksForIndex(frames)
			if len(chunks) != wantOCR+wantCaption {
				t.Fatalf("chunks=%+v", chunks)
			}
			if mode == model.VisualModeOff {
				if count != 0 || store.downloads != 0 {
					t.Fatalf("off downloaded video count=%d downloads=%d", count, store.downloads)
				}
			} else if count != 1 {
				t.Fatalf("frame count=%d", count)
			}
		})
	}
}

func TestFailedVisualRebuildKeepsPublishedEvidence(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "failed-rebuild", Filename: "slides.mp4", FileURL: "videos/slides", VisualMode: model.VisualModeOCR}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{{TaskID: task.ID, FrameIndex: 0, OCRText: "原有证据", Status: model.VisualFrameStatusCompleted}}); err != nil {
		t.Fatal(err)
	}
	svc := NewVisualIndexService(repos, nil, "ffmpeg", DefaultVisualIndexConfig())
	svc.storage = &modeVisualStore{path: filepath.Join(t.TempDir(), "video.mp4")}
	svc.extract = func(context.Context, string, string, ffmpeg.ExtractKeyFramesOptions) ([]ffmpeg.KeyFrame, string, error) {
		return []ffmpeg.KeyFrame{{Path: "frame.jpg", TimeMs: 1000}}, "", nil
	}
	svc.ocrAvailable = func(context.Context) bool { return true }
	svc.recognizeOCR = func(context.Context, string) (string, error) { return "", errors.New("OCR failed") }
	if _, err := svc.BuildTaskVisualIndex(context.Background(), task); err == nil {
		t.Fatal("failed rebuild succeeded")
	}
	frames, err := repos.VisualFrame.ListByTaskID(task.ID)
	if err != nil || len(frames) != 1 || frames[0].OCRText != "原有证据" {
		t.Fatalf("old evidence replaced: %+v %v", frames, err)
	}
}

func TestVisualInvestigatorRejectsOCROnlyBeforeDownloadingOrResolvingModel(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "ocr-only", Filename: "slides.mp4", FileURL: "videos/slides", VisualMode: model.VisualModeOCR}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	inv := NewVisualInvestigator(repos, nil, "ffmpeg")
	called := false
	inv.SetVideoDownloader(func(context.Context, string) (string, error) { called = true; return "", nil })
	inv.SetVisionResolver(func(context.Context, int64) (ai.VisionClient, error) { called = true; return nil, nil })
	_, err := inv.Inspect(context.Background(), InspectRequest{UserID: 7, TaskID: task.ID, Goal: "理解图表", SeedWindows: []VisualTimeRange{{StartMS: 0, EndMS: 1000}}})
	if err == nil || called {
		t.Fatalf("OCR-only attempted vision: %v called=%v", err, called)
	}
}
