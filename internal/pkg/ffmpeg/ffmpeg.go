package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

const DefaultAudioSegmentSeconds = 300

// ExtractAudio 从视频中提取音频
// 面试亮点：FFmpeg 是 CPU 密集型任务，这正是需要异步处理的核心原因
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
