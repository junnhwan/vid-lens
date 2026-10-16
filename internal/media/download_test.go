package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func fakeExtractor(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestExtractorArgumentsNeverUseOwnerPlatformCookies(t *testing.T) {
	s := &Service{Config: config.Config{CookieFile: "/private/owner-cookies.txt", ProxyURL: "http://127.0.0.1:7890"}}
	for _, source := range []string{"https://youtube.com/watch?v=example", "https://www.youtube.com/watch?v=example", "https://m.youtube.com/shorts/example", "https://youtu.be/example", "https://WWW.YOUTUBE.COM/watch?v=example"} {
		t.Run(source, func(t *testing.T) {
			args := s.ytdlpArgs(source)
			if slices.Contains(args, "--cookies") || slices.Contains(args, s.Config.CookieFile) || !slices.Contains(args, "--no-cookies") || !slices.Contains(args, "--no-cookies-from-browser") {
				t.Fatal("YouTube arguments did not isolate account cookies")
			}
			if !slices.Contains(args, "--ignore-config") || !slices.Contains(args, "--js-runtimes") || !slices.Contains(args, s.Config.ProxyURL) {
				t.Fatal("anonymous arguments lost runtime, config isolation or proxy settings")
			}
		})
	}
	args := s.ytdlpArgs("https://www.bilibili.com/video/BVexample")
	if slices.Contains(args, "--cookies") || slices.Contains(args, s.Config.CookieFile) || !slices.Contains(args, "--no-cookies") || !slices.Contains(args, "--no-cookies-from-browser") {
		t.Fatal("Bilibili arguments still permit the owner's account cookies")
	}
	if youtubeSource("https://youtube.com.example.test/watch?v=example") {
		t.Fatal("lookalike host was classified as YouTube")
	}
}

func TestYouTubeDownloadDoesNotUsePlatformCookies(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("EXTRACTOR_ARGUMENTS", argsFile)
	fakeExtractor(t, "printf '%s\\n' \"$@\" > \"$EXTRACTOR_ARGUMENTS\"\nprintf 'video bytes'\n")
	s := &Service{Config: config.Config{CookieFile: "/private/owner-platform-cookies.txt"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.downloadStream(ctx, "https://www.youtube.com/watch?v=example", "best", filepath.Join(t.TempDir(), "video"), 1024); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := "\n" + string(raw)
	for _, flag := range []string{"--no-cookies", "--no-cookies-from-browser"} {
		if !strings.Contains(args, "\n"+flag+"\n") {
			t.Fatalf("YouTube download is missing %s", flag)
		}
	}
	if strings.Contains(args, "\n--cookies\n") || strings.Contains(args, s.Config.CookieFile) {
		t.Fatal("YouTube download received the owner's shared Cookie file")
	}
}

func TestYouTubeVerificationFailureHasSafeActionableMessage(t *testing.T) {
	for _, mode := range []string{"metadata", "download"} {
		t.Run(mode, func(t *testing.T) {
			fakeExtractor(t, "printf '%s\\n' \"ERROR: [youtube] example: Sign in to confirm you’re not a bot. Cookie=credential-do-not-echo https://media.example/video?sig=do-not-echo\" >&2\nexit 1\n")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var err error
			if mode == "metadata" {
				_, err = command(ctx, "yt-dlp", []string{"--dump-single-json", "https://www.youtube.com/watch?v=example"}, "", 0, 0)
			} else {
				err = (&Service{}).downloadStream(ctx, "https://www.youtube.com/watch?v=example", "best", filepath.Join(t.TempDir(), "video"), 1024)
			}
			var problem *domain.Problem
			if !errors.As(err, &problem) || problem.Code != "SOURCE_ACCESS_RESTRICTED" || !strings.Contains(problem.Detail, "匿名") {
				t.Fatalf("verification failure lost its safe classification: %v", err)
			}
			if strings.Contains(err.Error(), "do-not-echo") || strings.Contains(err.Error(), "https://") {
				t.Fatal("extractor diagnostics leaked into the public failure")
			}
		})
	}
}
