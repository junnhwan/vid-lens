package ytdlp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// DownloadVideo 通过 yt-dlp 下载视频
// 面试亮点：yt-dlp 支持 B站/YouTube/抖音等主流平台，降低用户使用门槛
// 用户无需手动下载视频再上传，直接粘贴链接即可
func DownloadVideo(ctx context.Context, ytDlpPath, ffmpegPath, cookiesPath, proxyURL, videoURL string) (string, error) {
	outputPath := filepath.Join(os.TempDir(), uuid.New().String()+".mp4")

	args := buildArgs(ffmpegPath, cookiesPath, proxyURL, videoURL)
	args = append(args, "-o", outputPath)

	cmd := exec.CommandContext(ctx, ytDlpPath, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = nil // 丢弃 stdout

	if err := cmd.Run(); err != nil {
		// 清理残留文件
		os.Remove(outputPath)
		return "", formatDownloadError(err, stderr.String())
	}

	// 验证文件存在
	info, err := os.Stat(outputPath)
	if err != nil {
		return "", fmt.Errorf("下载显示成功但文件未生成")
	}

	fmt.Printf("[yt-dlp] 下载完成: %s (%d KB)\n", filepath.Base(outputPath), info.Size()/1024)
	return outputPath, nil
}

func buildArgs(ffmpegPath, cookiesPath, proxyURL, videoURL string) []string {
	args := []string{
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"--referer", "https://www.bilibili.com/",
		"--format", "bv*[height<=720][ext=mp4]+ba[ext=m4a]/bv*[height<=720]+ba/best[height<=720]/best",
		"--recode-video", "mp4",
		"--ffmpeg-location", ffmpegPath,
		"--no-playlist",
	}
	if strings.TrimSpace(cookiesPath) != "" {
		args = append(args, "--cookies", strings.TrimSpace(cookiesPath))
	}
	if strings.TrimSpace(proxyURL) != "" {
		args = append(args, "--proxy", strings.TrimSpace(proxyURL))
	}
	return append(args, videoURL)
}

func formatDownloadError(err error, stderr string) error {
	if strings.Contains(stderr, "HTTP Error 412") && strings.Contains(stderr, "[BiliBili]") {
		return fmt.Errorf("yt-dlp 下载失败: B 站返回 412，服务器请求被 B 站风控拦截。请改用本地视频上传，或在服务器配置 B 站 cookies 后重试: %w\n%s", err, stderr)
	}
	if strings.Contains(stderr, "[youtube]") && strings.Contains(stderr, "Network is unreachable") {
		return fmt.Errorf("yt-dlp 下载失败: 服务器直连 YouTube 失败，请在 tools.proxy_url 配置可用代理后重试: %w\n%s", err, stderr)
	}
	return fmt.Errorf("yt-dlp 下载失败: %w\n%s", err, stderr)
}
