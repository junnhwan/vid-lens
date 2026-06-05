package ytdlp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DownloadVideo 通过 yt-dlp 下载视频
// 面试亮点：yt-dlp 支持 B站/YouTube/抖音等主流平台，降低用户使用门槛
// 用户无需手动下载视频再上传，直接粘贴链接即可
func DownloadVideo(ctx context.Context, ytDlpPath, ffmpegPath, videoURL string) (string, error) {
	outputPath := filepath.Join(os.TempDir(), uuid.New().String()+".mp4")

	args := []string{
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"--referer", "https://www.bilibili.com/",
		"--recode-video", "mp4",
		"--ffmpeg-location", ffmpegPath,
		"-o", outputPath,
		"--no-check-certificate",
		"--no-playlist",
		videoURL,
	}

	cmd := exec.CommandContext(ctx, ytDlpPath, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = nil // 丢弃 stdout

	if err := cmd.Run(); err != nil {
		// 清理残留文件
		os.Remove(outputPath)
		return "", fmt.Errorf("yt-dlp 下载失败: %w\n%s", err, stderr.String())
	}

	// 验证文件存在
	info, err := os.Stat(outputPath)
	if err != nil {
		return "", fmt.Errorf("下载显示成功但文件未生成")
	}

	fmt.Printf("[yt-dlp] 下载完成: %s (%d KB)\n", filepath.Base(outputPath), info.Size()/1024)
	return outputPath, nil
}

// DownloadVideoWithTimeout 带超时的下载（默认 10 分钟）
func DownloadVideoWithTimeout(ytDlpPath, ffmpegPath, videoURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return DownloadVideo(ctx, ytDlpPath, ffmpegPath, videoURL)
}
