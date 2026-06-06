package service

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"vid-lens/internal/config"
)

func TestValidateRemoteVideoURLRejectsLocalTargets(t *testing.T) {
	cases := []string{
		"http://localhost/video.mp4",
		"http://127.0.0.1/video.mp4",
		"http://[::1]/video.mp4",
		"file:///tmp/video.mp4",
	}

	for _, rawURL := range cases {
		if err := validateRemoteVideoURL(rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}

func TestValidateRemoteVideoURLAllowsHTTPVideoSites(t *testing.T) {
	cases := []string{
		"https://www.bilibili.com/video/BV1xx411c7mD",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}

	for _, rawURL := range cases {
		if err := validateRemoteVideoURL(rawURL); err != nil {
			t.Fatalf("expected %q to be allowed, got %v", rawURL, err)
		}
	}
}

func TestUploadByURLLogsDownloadFailureWithSanitizedURL(t *testing.T) {
	var logs bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(originalOutput)

	svc := &MediaService{
		tools: config.ToolsConfig{
			YtDlpPath:  filepathThatDoesNotExist(),
			FFmpegPath: "ffmpeg",
		},
	}

	_, err := svc.UploadByURL(context.Background(), 7, "https://www.bilibili.com/video/BV1xx411c7mD?p=1&token=secret#frag")
	if err == nil {
		t.Fatal("UploadByURL() succeeded, want download failure")
	}

	got := logs.String()
	for _, want := range []string{
		"[Media] URL upload download failed",
		"userID=7",
		"url=https://www.bilibili.com/video/BV1xx411c7mD",
		"yt-dlp 下载失败",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "token=secret") || strings.Contains(got, "#frag") {
		t.Fatalf("log leaked query or fragment: %s", got)
	}
}

func filepathThatDoesNotExist() string {
	return os.DevNull + "-vidlens-missing-ytdlp"
}
