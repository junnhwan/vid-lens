package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/ffmpeg"
	"vid-lens/internal/pkg/ocr"
	"vid-lens/internal/repository"
	"vid-lens/internal/storage"
)

// VisualIndexConfig controls keyframe sampling and caption cost/quality trade-offs.
// Business intent: surface on-screen content ASR cannot hear (PPT/board), not every frame.
type VisualIndexConfig struct {
	Enabled         bool
	OCRCommand      string
	OCRLang         string
	SceneThreshold  float64
	IntervalSeconds int
	MaxFrames       int
	ScaleWidth      int
	// FailOpen: visual pipeline errors do not fail the parent task.
	FailOpen bool
}

func DefaultVisualIndexConfig() VisualIndexConfig {
	return VisualIndexConfig{
		Enabled:         true,
		OCRCommand:      "tesseract",
		OCRLang:         "chi_sim+eng",
		SceneThreshold:  0.30,
		IntervalSeconds: 30,
		MaxFrames:       120,
		ScaleWidth:      960,
		FailOpen:        true,
	}
}

// VisualIndexService builds task-owned visual evidence (keyframes + caption/OCR text).
// Caption order: multimodal Vision API (if user configured) → local Tesseract OCR fallback.
type VisualIndexService struct {
	repos   *repository.Repositories
	storage *storage.MinIOStorage
	ffmpeg  string
	cfg     VisualIndexConfig
	ocr     *ocr.Recognizer
	// resolveVision returns a vision client for the task owner; nil client means skip vision.
	resolveVision func(ctx context.Context, userID int64) (ai.VisionClient, error)
	extract       func(ctx context.Context, ffmpegPath, inputPath string, opts ffmpeg.ExtractKeyFramesOptions) ([]ffmpeg.KeyFrame, string, error)
	recognizeOCR  func(ctx context.Context, imagePath string) (string, error)
}

func NewVisualIndexService(
	repos *repository.Repositories,
	store *storage.MinIOStorage,
	ffmpegPath string,
	cfg VisualIndexConfig,
) *VisualIndexService {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	defaults := DefaultVisualIndexConfig()
	if strings.TrimSpace(cfg.OCRCommand) == "" {
		cfg.OCRCommand = defaults.OCRCommand
	}
	if strings.TrimSpace(cfg.OCRLang) == "" {
		cfg.OCRLang = defaults.OCRLang
	}
	if cfg.SceneThreshold <= 0 {
		cfg.SceneThreshold = defaults.SceneThreshold
	}
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = defaults.IntervalSeconds
	}
	if cfg.MaxFrames <= 0 {
		cfg.MaxFrames = defaults.MaxFrames
	}
	if cfg.ScaleWidth <= 0 {
		cfg.ScaleWidth = defaults.ScaleWidth
	}
	recognizer := ocr.NewRecognizer(cfg.OCRCommand, cfg.OCRLang)
	svc := &VisualIndexService{
		repos:   repos,
		storage: store,
		ffmpeg:  ffmpegPath,
		cfg:     cfg,
		ocr:     recognizer,
		extract: ffmpeg.ExtractKeyFrames,
	}
	svc.recognizeOCR = recognizer.Recognize
	return svc
}

// SetVisionResolver injects BYOK vision client resolution (optional).
func (s *VisualIndexService) SetVisionResolver(fn func(ctx context.Context, userID int64) (ai.VisionClient, error)) {
	if s != nil {
		s.resolveVision = fn
	}
}

// BuildTaskVisualIndex downloads the task video, extracts keyframes, captions them
// (vision then OCR), uploads evidence frames, and replaces video_visual_frames.
// Returns count of frames that produced non-empty text.
func (s *VisualIndexService) BuildTaskVisualIndex(ctx context.Context, task *model.VideoTask) (int, error) {
	if s == nil || !s.cfg.Enabled {
		return 0, nil
	}
	if task == nil {
		return 0, fmt.Errorf("task is nil")
	}
	if s.storage == nil {
		return 0, fmt.Errorf("object storage is unavailable")
	}
	if s.repos == nil || s.repos.VisualFrame == nil {
		return 0, fmt.Errorf("visual frame repository is unavailable")
	}

	vision, visionErr := s.loadVisionClient(ctx, task.UserID)
	ocrOK := s.ocr != nil && s.ocr.Available(ctx)
	if vision == nil && !ocrOK {
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "visual index skipped: no vision profile and ocr unavailable",
			slog.String("ocr_command", s.cfg.OCRCommand),
			slog.String("vision_error", errString(visionErr)))
		return 0, nil
	}
	if vision == nil && visionErr != nil {
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "vision not used; will try ocr fallback",
			slog.String("reason", errString(visionErr)))
	}

	videoPath, err := s.storage.DownloadToTemp(ctx, task.FileURL)
	if err != nil {
		return 0, fmt.Errorf("download video for visual index: %w", err)
	}
	defer os.Remove(videoPath)

	frames, workDir, err := s.extract(ctx, s.ffmpeg, videoPath, ffmpeg.ExtractKeyFramesOptions{
		SceneThreshold:  s.cfg.SceneThreshold,
		IntervalSeconds: s.cfg.IntervalSeconds,
		MaxFrames:       s.cfg.MaxFrames,
		ScaleWidth:      s.cfg.ScaleWidth,
	})
	if err != nil {
		return 0, err
	}
	if workDir != "" {
		defer os.RemoveAll(workDir)
	}

	rows := make([]model.VideoVisualFrame, 0, len(frames))
	textCount := 0
	for i, frame := range frames {
		if err := ctx.Err(); err != nil {
			return textCount, err
		}
		row := model.VideoVisualFrame{
			TaskID:     task.ID,
			FrameIndex: i,
			TimeMs:     frame.TimeMs,
			Source:     frame.Source,
			Status:     model.VisualFrameStatusPending,
		}

		text, method, capErr := s.captionFrame(ctx, vision, ocrOK, frame.Path)
		if capErr != nil {
			row.Status = model.VisualFrameStatusFailed
			row.ErrorMsg = truncateVisualErr(capErr.Error())
		} else {
			row.OCRText = text
			row.CaptionMethod = method
			if strings.TrimSpace(text) == "" {
				row.Status = model.VisualFrameStatusSkipped
			} else {
				row.Status = model.VisualFrameStatusCompleted
				textCount++
			}
		}

		objectKey := visualFrameObjectKey(task.ID, i, frame.TimeMs)
		if _, upErr := s.storage.UploadFromPath(ctx, frame.Path, objectKey, "image/jpeg"); upErr != nil {
			if row.ErrorMsg == "" {
				row.ErrorMsg = truncateVisualErr(upErr.Error())
			}
		} else {
			row.ObjectKey = objectKey
		}
		rows = append(rows, row)
	}

	if err := s.repos.VisualFrame.ReplaceTaskFrames(task.ID, rows); err != nil {
		return 0, fmt.Errorf("persist visual frames: %w", err)
	}
	return textCount, nil
}

func (s *VisualIndexService) loadVisionClient(ctx context.Context, userID int64) (ai.VisionClient, error) {
	if s.resolveVision == nil {
		return nil, fmt.Errorf("vision resolver not configured")
	}
	client, err := s.resolveVision(ctx, userID)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (s *VisualIndexService) captionFrame(ctx context.Context, vision ai.VisionClient, ocrOK bool, imagePath string) (text, method string, err error) {
	if vision != nil {
		caption, vErr := vision.CaptionImage(ctx, imagePath, ai.DefaultVisionCaptionPrompt)
		if vErr == nil {
			caption = strings.TrimSpace(caption)
			if caption != "" {
				return caption, "vision", nil
			}
			// empty vision output: fall through to OCR if available
		} else if !ocrOK {
			return "", "", vErr
		} else {
			observability.Log(ctx, slog.Default(), slog.LevelDebug, "vision caption failed; trying ocr",
				slog.String("error", observability.SafeError(vErr)))
		}
	}
	if ocrOK && s.recognizeOCR != nil {
		ocrText, oErr := s.recognizeOCR(ctx, imagePath)
		if oErr != nil {
			return "", "", oErr
		}
		return strings.TrimSpace(ocrText), "ocr", nil
	}
	return "", "", fmt.Errorf("no caption method available")
}

func visualFrameObjectKey(taskID int64, frameIndex int, timeMs int64) string {
	return filepath.ToSlash(filepath.Join(
		"visual-frames",
		fmt.Sprintf("task-%d", taskID),
		fmt.Sprintf("frame-%04d-%dms.jpg", frameIndex, timeMs),
	))
}

func truncateVisualErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 500 {
		return msg[:500]
	}
	return msg
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// FormatOCRChunksForIndex turns completed visual frames into TextChunks so the
// existing RAG indexer can embed them alongside speech transcription.
func FormatOCRChunksForIndex(frames []model.VideoVisualFrame) []TextChunk {
	out := make([]TextChunk, 0, len(frames))
	for _, frame := range frames {
		text := strings.TrimSpace(frame.OCRText)
		if text == "" {
			continue
		}
		sec := frame.TimeMs / 1000
		mm := sec / 60
		ss := sec % 60
		label := "画面"
		switch frame.CaptionMethod {
		case "vision":
			label = "画面理解"
		case "ocr":
			label = "画面OCR"
		}
		content := fmt.Sprintf("[%s %02d:%02d]\n%s", label, mm, ss, text)
		out = append(out, TextChunk{Content: content})
	}
	return out
}
