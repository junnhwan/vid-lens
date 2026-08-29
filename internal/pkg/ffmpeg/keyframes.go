package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// KeyFrame is one extracted still image with an approximate media timestamp.
type KeyFrame struct {
	Path   string
	TimeMs int64
	Source string // scene | interval
}

// ExtractKeyFramesOptions controls scene-change and fallback interval sampling.
// Product goal: catch PPT/board transitions without dumping every video frame.
type ExtractKeyFramesOptions struct {
	// SceneThreshold is the FFmpeg scene score threshold (0..1). Default 0.30.
	SceneThreshold float64
	// IntervalSeconds is the guarantee sampling period for static slides. Default 30.
	IntervalSeconds int
	// MaxFrames caps how many frames are kept after merge+dedupe. Default 120.
	MaxFrames int
	// ScaleWidth downscales frames before OCR (0 keeps original). Default 960.
	ScaleWidth int
}

func (o ExtractKeyFramesOptions) normalized() ExtractKeyFramesOptions {
	if o.SceneThreshold <= 0 {
		o.SceneThreshold = 0.30
	}
	if o.IntervalSeconds <= 0 {
		o.IntervalSeconds = 30
	}
	if o.MaxFrames <= 0 {
		o.MaxFrames = 120
	}
	if o.ScaleWidth <= 0 {
		o.ScaleWidth = 960
	}
	return o
}

// ExtractKeyFrames writes JPEG stills under a temp directory.
// Strategy:
//  1. scene-change frames (catches slide flips)
//  2. fixed-interval frames (catches static boards that never trip scene detect)
//
// Callers own cleanup of returned paths' parent directory (os.RemoveAll).
func ExtractKeyFrames(ctx context.Context, ffmpegPath, inputPath string, opts ExtractKeyFramesOptions) ([]KeyFrame, string, error) {
	opts = opts.normalized()
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	outputDir, err := os.MkdirTemp("", "vidlens_keyframes_*")
	if err != nil {
		return nil, "", err
	}

	sceneDir := filepath.Join(outputDir, "scene")
	intervalDir := filepath.Join(outputDir, "interval")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
		os.RemoveAll(outputDir)
		return nil, "", err
	}
	if err := os.MkdirAll(intervalDir, 0o755); err != nil {
		os.RemoveAll(outputDir)
		return nil, "", err
	}

	sceneFrames, sceneErr := extractSceneFrames(ctx, ffmpegPath, inputPath, sceneDir, opts)
	intervalFrames, intervalErr := extractIntervalFrames(ctx, ffmpegPath, inputPath, intervalDir, opts)
	if sceneErr != nil && intervalErr != nil {
		os.RemoveAll(outputDir)
		return nil, "", fmt.Errorf("关键帧抽取失败: scene=%v; interval=%v", sceneErr, intervalErr)
	}

	merged := mergeKeyFrames(sceneFrames, intervalFrames, opts.MaxFrames)
	if len(merged) == 0 {
		os.RemoveAll(outputDir)
		return nil, "", fmt.Errorf("关键帧抽取完成但没有输出帧")
	}
	return merged, outputDir, nil
}

func extractSceneFrames(ctx context.Context, ffmpegPath, inputPath, outDir string, opts ExtractKeyFramesOptions) ([]KeyFrame, error) {
	// select scene frames; fps=1/Interval as soft upper bound per second of dense cuts.
	filter := fmt.Sprintf(
		"select='gt(scene\\,%.3f)',scale=%d:-2,showinfo",
		opts.SceneThreshold, opts.ScaleWidth,
	)
	pattern := filepath.Join(outDir, "frame_%04d.jpg")
	args := []string{
		"-hide_banner",
		"-y",
		"-i", inputPath,
		"-vf", filter,
		"-vsync", "vfr",
		"-q:v", "3",
		pattern,
	}
	stderr, err := runFFmpegCapture(ctx, ffmpegPath, args)
	if err != nil {
		// Some builds fail hard when select yields zero frames; treat as empty, not fatal.
		if strings.Contains(stderr, "Output file is empty") || strings.Contains(err.Error(), "exit status") {
			files, _ := filepath.Glob(filepath.Join(outDir, "frame_*.jpg"))
			if len(files) == 0 {
				return nil, nil
			}
		} else {
			return nil, fmt.Errorf("scene keyframes: %w (%s)", err, trimLog(stderr))
		}
	}
	pts := parseShowinfoPTS(stderr)
	files, err := filepath.Glob(filepath.Join(outDir, "frame_*.jpg"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	frames := make([]KeyFrame, 0, len(files))
	for i, path := range files {
		timeMs := int64(i) * int64(opts.IntervalSeconds) * 1000
		if i < len(pts) {
			timeMs = pts[i]
		}
		frames = append(frames, KeyFrame{Path: path, TimeMs: timeMs, Source: "scene"})
	}
	return frames, nil
}

func extractIntervalFrames(ctx context.Context, ffmpegPath, inputPath, outDir string, opts ExtractKeyFramesOptions) ([]KeyFrame, error) {
	filter := fmt.Sprintf("fps=1/%d,scale=%d:-2", opts.IntervalSeconds, opts.ScaleWidth)
	pattern := filepath.Join(outDir, "frame_%04d.jpg")
	args := []string{
		"-hide_banner",
		"-y",
		"-i", inputPath,
		"-vf", filter,
		"-q:v", "3",
		pattern,
	}
	if _, err := runFFmpegCapture(ctx, ffmpegPath, args); err != nil {
		files, _ := filepath.Glob(filepath.Join(outDir, "frame_*.jpg"))
		if len(files) == 0 {
			return nil, fmt.Errorf("interval keyframes: %w", err)
		}
	}
	files, err := filepath.Glob(filepath.Join(outDir, "frame_*.jpg"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	frames := make([]KeyFrame, 0, len(files))
	for i, path := range files {
		timeMs := int64(i) * int64(opts.IntervalSeconds) * 1000
		frames = append(frames, KeyFrame{Path: path, TimeMs: timeMs, Source: "interval"})
	}
	return frames, nil
}

func mergeKeyFrames(scene, interval []KeyFrame, maxFrames int) []KeyFrame {
	all := append([]KeyFrame{}, scene...)
	all = append(all, interval...)
	if len(all) == 0 {
		return nil
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].TimeMs == all[j].TimeMs {
			// Prefer scene over interval at same timestamp.
			return all[i].Source == "scene" && all[j].Source != "scene"
		}
		return all[i].TimeMs < all[j].TimeMs
	})
	// Drop near-duplicates within 2s window, keep first (scene preferred by sort).
	const windowMs int64 = 2000
	out := make([]KeyFrame, 0, len(all))
	var lastMs int64 = -windowMs - 1
	for _, f := range all {
		if f.TimeMs-lastMs < windowMs {
			continue
		}
		out = append(out, f)
		lastMs = f.TimeMs
		if maxFrames > 0 && len(out) >= maxFrames {
			break
		}
	}
	return out
}

func runFFmpegCapture(ctx context.Context, ffmpegPath string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// parseShowinfoPTS extracts pts_time values written by FFmpeg's showinfo filter.
func parseShowinfoPTS(log string) []int64 {
	const marker = "pts_time:"
	var out []int64
	for _, line := range strings.Split(log, "\n") {
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(marker):]
		end := 0
		for end < len(rest) {
			c := rest[end]
			if (c >= '0' && c <= '9') || c == '.' || c == '-' {
				end++
				continue
			}
			break
		}
		if end == 0 {
			continue
		}
		sec, err := strconv.ParseFloat(rest[:end], 64)
		if err != nil {
			continue
		}
		out = append(out, int64(sec*1000))
	}
	return out
}

func trimLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[len(s)-400:]
	}
	return s
}

// ProbeDurationMs returns media duration in milliseconds via ffprobe when available,
// or 0 when probing is not possible. Soft dependency for sampling bounds only.
func ProbeDurationMs(ctx context.Context, ffprobePath, inputPath string) (int64, error) {
	if ffprobePath == "" {
		// Try sibling of common ffmpeg path is caller's concern; default name.
		ffprobePath = "ffprobe"
	}
	args := []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	}
	cmd := exec.CommandContext(ctx, ffprobePath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe duration: %w (%s)", err, trimLog(stderr.String()))
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(stdout.String()), 64)
	if err != nil {
		return 0, err
	}
	return int64(sec * 1000), nil
}
