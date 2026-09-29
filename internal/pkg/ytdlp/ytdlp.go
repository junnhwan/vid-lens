package ytdlp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"vid-lens/internal/pkg/remoteurl"
)

// DownloadVideo 通过 yt-dlp 下载视频
// yt-dlp supports common video platforms so users can submit a URL directly.
// 用户无需手动下载视频再上传，直接粘贴链接即可
func DownloadVideo(ctx context.Context, ytDlpPath, ffmpegPath, cookiesPath, proxyURL, videoURL string) (string, error) {
	ytDlpPath = strings.TrimSpace(ytDlpPath)
	if ytDlpPath == "" {
		ytDlpPath = "yt-dlp"
	}
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
		"--ignore-config",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"--format", "bv*[height<=720][ext=mp4]+ba[ext=m4a]/bv*[height<=720]+ba/best[height<=720]/best",
		"--recode-video", "mp4",
		"--no-playlist",
	}
	// Bare default commands are discovered on PATH by yt-dlp. Its location
	// option expects an existing file/directory, not a command name.
	ffmpegPath = strings.TrimSpace(ffmpegPath)
	if ffmpegPath != "" && ffmpegPath != "ffmpeg" && ffmpegPath != "ffmpeg.exe" {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}
	parsed, err := url.Parse(videoURL)
	if err == nil && remoteurl.HostAllowed(parsed.Hostname(), []string{"bilibili.com", "b23.tv"}) {
		args = append(args, "--referer", "https://www.bilibili.com/")
		// tools.cookies_path is the Bilibili cookie jar. yt-dlp still applies
		// domain matching to individual cookies when following redirects.
		if strings.TrimSpace(cookiesPath) != "" {
			args = append(args, "--cookies", strings.TrimSpace(cookiesPath))
		}
	} else if err == nil && remoteurl.HostAllowed(parsed.Hostname(), []string{"youtube.com", "youtu.be"}) {
		args = append(args, "--referer", "https://www.youtube.com/")
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
