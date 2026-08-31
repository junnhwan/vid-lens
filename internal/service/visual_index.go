package service

import (
	"context"
	"crypto/sha256"
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

const visualSamplingVersion = "scene-interval-v2"

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

// BuildTaskVisualIndex downloads the task video, extracts keyframes, runs OCR
// and Vision as independent observations, uploads evidence frames, and replaces
// video_visual_frames.
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
		startMS, endMS := frame.TimeMs, frame.TimeMs+1
		row := model.VideoVisualFrame{
			TaskID: task.ID, FrameIndex: i, FrameKey: stableVisualFrameKey(task.ID, frame.TimeMs, frame.Source),
			TimeMs: frame.TimeMs, StartMS: startMS, EndMS: endMS, TimeStatus: model.ChunkTimeRangeExact,
			Source: frame.Source, SamplingVersion: visualSamplingVersion,
			Status: model.VisualFrameStatusPending, OCRStatus: model.VisualFrameStatusPending, VisionStatus: model.VisualFrameStatusPending,
		}

		// OCR and Vision are separate observations of the same sampled frame.
		// Running both prevents a caption from erasing slide/subtitle text and
		// preserves disagreements for retrieval and answer-time uncertainty.
		if ocrOK && s.recognizeOCR != nil {
			ocrText, ocrErr := s.recognizeOCR(ctx, frame.Path)
			if ocrErr != nil {
				row.OCRStatus, row.OCRError = model.VisualFrameStatusFailed, truncateVisualErr(ocrErr.Error())
			} else if row.OCRText = strings.TrimSpace(ocrText); row.OCRText != "" {
				row.OCRStatus = model.VisualFrameStatusCompleted
			} else {
				row.OCRStatus = model.VisualFrameStatusSkipped
			}
		} else {
			row.OCRStatus = model.VisualFrameStatusSkipped
		}
		if vision != nil {
			caption, captionErr := vision.CaptionImage(ctx, frame.Path, ai.DefaultVisionCaptionPrompt)
			if captionErr != nil {
				row.VisionStatus, row.VisionError = model.VisualFrameStatusFailed, truncateVisualErr(captionErr.Error())
			} else if row.VisionCaption = strings.TrimSpace(caption); row.VisionCaption != "" {
				row.VisionStatus = model.VisualFrameStatusCompleted
			} else {
				row.VisionStatus = model.VisualFrameStatusSkipped
			}
		} else {
			row.VisionStatus = model.VisualFrameStatusSkipped
		}
		row.CaptionMethod = visualCaptionMethod(row)
		switch {
		case row.OCRText != "" || row.VisionCaption != "":
			row.Status = model.VisualFrameStatusCompleted
			textCount++
		case row.OCRStatus == model.VisualFrameStatusFailed || row.VisionStatus == model.VisualFrameStatusFailed:
			row.Status = model.VisualFrameStatusFailed
			row.ErrorMsg = firstVisualError(row.VisionError, row.OCRError)
		default:
			row.Status = model.VisualFrameStatusSkipped
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

func visualFrameObjectKey(taskID int64, frameIndex int, timeMs int64) string {
	return filepath.ToSlash(filepath.Join(
		"visual-frames",
		fmt.Sprintf("task-%d", taskID),
		fmt.Sprintf("frame-%04d-%dms.jpg", frameIndex, timeMs),
	))
}

func stableVisualFrameKey(taskID, timeMS int64, source string) string {
	payload := fmt.Sprintf("%s:%d:%d:%s", visualSamplingVersion, taskID, timeMS, strings.TrimSpace(source))
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("vf_%x", sum[:12])
}

func visualCaptionMethod(frame model.VideoVisualFrame) string {
	switch {
	case strings.TrimSpace(frame.VisionCaption) != "" && strings.TrimSpace(frame.OCRText) != "":
		return "vision+ocr"
	case strings.TrimSpace(frame.VisionCaption) != "":
		return "vision"
	case strings.TrimSpace(frame.OCRText) != "":
		return "ocr"
	default:
		return ""
	}
}

func firstVisualError(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "visual analysis produced no evidence"
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

// FormatOCRChunksForIndex is retained for callers while emitting every visual
// observation. OCR and Vision captions remain separate modalities.
func FormatOCRChunksForIndex(frames []model.VideoVisualFrame) []TextChunk {
	return formatOCRChunksForIndex(frames, 800)
}

func formatOCRChunksForIndex(frames []model.VideoVisualFrame, chunkSize int) []TextChunk {
	out := make([]TextChunk, 0, len(frames))
	for _, frame := range frames {
		frameChunkStart := len(out)
		startMS, endMS, timeStatus := visualFrameRange(frame)
		stableID := visualFrameStableID(frame)
		appendObservation := func(text, label, modality, method string) {
			text = strings.TrimSpace(text)
			if text == "" {
				return
			}
			sec := startMS / 1000
			content := fmt.Sprintf("[%s %02d:%02d]\n%s", label, sec/60, sec%60, text)
			observation := SourceTextObservation{Content: content, Modality: modality, Refs: []ChunkSourceRef{{
				SourceType: modality, StableID: stableID, SourceRowID: frame.ID,
				StartMS: startMS, EndMS: endMS, TimeRangeStatus: timeStatus,
				ObjectKey: frame.ObjectKey, CaptionMethod: method,
			}}}
			out = append(out, SplitObservationsIntoChunks([]SourceTextObservation{observation}, chunkSize, 0)...)
		}
		appendObservation(frame.OCRText, "画面OCR", model.ChunkModalityVisualOCR, "ocr")
		// Historical rows stored a Vision caption in OCRText with method=vision.
		if strings.TrimSpace(frame.VisionCaption) != "" {
			appendObservation(frame.VisionCaption, "画面理解", model.ChunkModalityVisualCaption, "vision")
		} else if frame.CaptionMethod == "vision" {
			// Remove the OCR copy emitted above for this legacy row.
			out = out[:frameChunkStart]
			appendObservation(frame.OCRText, "画面理解", model.ChunkModalityVisualCaption, "vision")
		}
	}
	return out
}

func visualFrameStableID(frame model.VideoVisualFrame) string {
	if key := strings.TrimSpace(frame.FrameKey); key != "" {
		return "visual-frame:" + key
	}
	return fmt.Sprintf("visual-frame:%d", frame.ID)
}

func visualFrameRange(frame model.VideoVisualFrame) (int64, int64, string) {
	if frame.EndMS > frame.StartMS && frame.StartMS >= 0 && (frame.TimeStatus == model.ChunkTimeRangeExact || frame.TimeStatus == model.ChunkTimeRangeCoarse) {
		return frame.StartMS, frame.EndMS, frame.TimeStatus
	}
	if frame.TimeMs >= 0 {
		return frame.TimeMs, frame.TimeMs + 1, model.ChunkTimeRangeExact
	}
	return 0, 0, model.ChunkTimeRangeUnknown
}
