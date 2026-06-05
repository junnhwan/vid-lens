package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ExtractAudio 从视频中提取音频
// 面试亮点：FFmpeg 是 CPU 密集型任务，这正是需要异步处理的核心原因
func ExtractAudio(ctx context.Context, ffmpegPath, inputPath string) (string, error) {
	outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("vidlens_%d.mp3", time.Now().UnixNano()))

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-i", inputPath,
		"-vn",
		"-acodec", "libmp3lame",
		"-q:a", "2",
		outputPath,
	)

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

// GetVideoDuration 获取视频时长（秒）
func GetVideoDuration(ctx context.Context, ffprobePath, inputPath string) (float64, error) {
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("获取视频时长失败: %w", err)
	}

	var duration float64
	fmt.Sscanf(stdout.String(), "%f", &duration)
	return duration, nil
}
