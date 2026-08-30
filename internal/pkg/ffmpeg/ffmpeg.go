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
	"time"
)

const (
	DefaultAudioSegmentSeconds        = 300
	DefaultAudioSegmentOverlapSeconds = 5
	AudioSegmenterVersion             = "overlap_windows_v1"
)

// AudioSegment describes both the actual audio window sent to ASR and the
// non-overlapping core range owned by that segment. Adjacent windows overlap;
// adjacent core ranges do not.
type AudioSegment struct {
	Index         int
	Path          string
	WindowStartMS int64
	WindowEndMS   int64
	CoreStartMS   int64
	CoreEndMS     int64
	SegmentKey    string
	Version       string
}

type audioSegmentWindow struct {
	WindowStartMS int64
	WindowEndMS   int64
	CoreStartMS   int64
	CoreEndMS     int64
}

// ExtractAudio 从视频中提取音频
// Audio extraction is CPU-intensive and is therefore handled by the async pipeline.
func ExtractAudio(ctx context.Context, ffmpegPath, inputPath string) (string, error) {
	outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("vidlens_%d.mp3", time.Now().UnixNano()))

	cmd := exec.CommandContext(ctx, ffmpegPath, buildExtractAudioArgs(inputPath, outputPath)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(outputPath)
		return "", fmt.Errorf("FFmpeg 转码失败: %w, stderr: %s", err, stderr.String())
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return "", fmt.Errorf("FFmpeg 转码完成但输出文件不存在")
	}

	return outputPath, nil
}

func buildExtractAudioArgs(inputPath, outputPath string) []string {
	return []string{
		"-y",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-acodec", "libmp3lame",
		"-b:a", "32k",
		outputPath,
	}
}

// SplitAudio 按固定时长把音频切成多个片段，避免 ASR 单次请求体过大。
func SplitAudio(ctx context.Context, ffmpegPath, inputPath string, segmentSeconds int) ([]string, error) {
	if segmentSeconds <= 0 {
		segmentSeconds = DefaultAudioSegmentSeconds
	}

	outputDir, err := os.MkdirTemp("", "vidlens_audio_chunks_*")
	if err != nil {
		return nil, err
	}

	pattern := filepath.Join(outputDir, "chunk_%03d.mp3")
	cmd := exec.CommandContext(ctx, ffmpegPath, buildSplitAudioArgs(inputPath, pattern, segmentSeconds)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.RemoveAll(outputDir)
		return nil, fmt.Errorf("FFmpeg 音频分片失败: %w, stderr: %s", err, stderr.String())
	}

	chunks, err := filepath.Glob(filepath.Join(outputDir, "chunk_*.mp3"))
	if err != nil {
		os.RemoveAll(outputDir)
		return nil, err
	}
	sort.Strings(chunks)
	if len(chunks) == 0 {
		os.RemoveAll(outputDir)
		return nil, fmt.Errorf("FFmpeg 音频分片完成但没有输出片段")
	}

	return chunks, nil
}

func buildSplitAudioArgs(inputPath, outputPattern string, segmentSeconds int) []string {
	return []string{
		"-y",
		"-i", inputPath,
		"-f", "segment",
		"-segment_time", fmt.Sprintf("%d", segmentSeconds),
		"-reset_timestamps", "1",
		"-acodec", "copy",
		outputPattern,
	}
}

// SplitAudioWindows produces ASR inputs with context on both sides of every
// logical boundary. It returns the exact temp directory so callers never have
// to infer a recursive-cleanup target from a generated filename.
func SplitAudioWindows(ctx context.Context, ffmpegPath, inputPath string, segmentSeconds, overlapSeconds int) ([]AudioSegment, string, error) {
	if segmentSeconds <= 0 {
		segmentSeconds = DefaultAudioSegmentSeconds
	}
	if overlapSeconds < 0 {
		overlapSeconds = 0
	}
	if overlapSeconds*2 >= segmentSeconds {
		return nil, "", fmt.Errorf("ASR overlap 必须小于分片时长的一半")
	}
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	durationMS, err := ProbeDurationMs(ctx, companionFFprobePath(ffmpegPath), inputPath)
	if err != nil {
		return nil, "", fmt.Errorf("探测 ASR 音频时长失败: %w", err)
	}
	windows := planAudioSegmentWindows(durationMS, int64(segmentSeconds)*1000, int64(overlapSeconds)*1000)
	if len(windows) == 0 {
		return nil, "", fmt.Errorf("ASR 音频时长无效")
	}

	outputDir, err := os.MkdirTemp("", "vidlens_audio_windows_*")
	if err != nil {
		return nil, "", err
	}
	segments := make([]AudioSegment, 0, len(windows))
	for i, window := range windows {
		outputPath := filepath.Join(outputDir, fmt.Sprintf("chunk_%03d.mp3", i))
		cmd := exec.CommandContext(ctx, ffmpegPath, buildExtractAudioWindowArgs(inputPath, outputPath, window.WindowStartMS, window.WindowEndMS)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			os.RemoveAll(outputDir)
			return nil, "", fmt.Errorf("FFmpeg 音频重叠分片失败: %w, stderr: %s", err, stderr.String())
		}
		if info, err := os.Stat(outputPath); err != nil || info.Size() == 0 {
			os.RemoveAll(outputDir)
			return nil, "", fmt.Errorf("FFmpeg 音频重叠分片完成但第 %d 段无有效输出", i+1)
		}
		segments = append(segments, AudioSegment{
			Index: i, Path: outputPath,
			WindowStartMS: window.WindowStartMS, WindowEndMS: window.WindowEndMS,
			CoreStartMS: window.CoreStartMS, CoreEndMS: window.CoreEndMS,
			SegmentKey: fmt.Sprintf("%s:%d:%d:%d:%d", AudioSegmenterVersion, window.WindowStartMS, window.WindowEndMS, window.CoreStartMS, window.CoreEndMS),
			Version:    AudioSegmenterVersion,
		})
	}
	return segments, outputDir, nil
}

func planAudioSegmentWindows(durationMS, segmentMS, overlapMS int64) []audioSegmentWindow {
	if durationMS <= 0 || segmentMS <= 0 || overlapMS < 0 || overlapMS*2 >= segmentMS {
		return nil
	}
	windows := make([]audioSegmentWindow, 0, int((durationMS+segmentMS-1)/segmentMS))
	for coreStart := int64(0); coreStart < durationMS; coreStart += segmentMS {
		coreEnd := coreStart + segmentMS
		if coreEnd > durationMS {
			coreEnd = durationMS
		}
		windowStart := coreStart - overlapMS
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := coreEnd + overlapMS
		if windowEnd > durationMS {
			windowEnd = durationMS
		}
		windows = append(windows, audioSegmentWindow{
			WindowStartMS: windowStart, WindowEndMS: windowEnd,
			CoreStartMS: coreStart, CoreEndMS: coreEnd,
		})
	}
	return windows
}

func buildExtractAudioWindowArgs(inputPath, outputPath string, startMS, endMS int64) []string {
	durationMS := endMS - startMS
	return []string{
		"-y",
		"-ss", formatFFmpegSeconds(startMS),
		"-i", inputPath,
		"-t", formatFFmpegSeconds(durationMS),
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-acodec", "libmp3lame",
		"-b:a", "32k",
		outputPath,
	}
}

func formatFFmpegSeconds(milliseconds int64) string {
	return strconv.FormatFloat(float64(milliseconds)/1000, 'f', 3, 64)
}

func companionFFprobePath(ffmpegPath string) string {
	dir := filepath.Dir(ffmpegPath)
	ext := filepath.Ext(ffmpegPath)
	name := "ffprobe" + ext
	if dir == "." || dir == "" {
		return name
	}
	return filepath.Join(dir, name)
}
