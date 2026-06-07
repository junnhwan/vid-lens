package ytdlp

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildArgsIncludesCookiesWhenConfigured(t *testing.T) {
	args := buildArgs("ffmpeg", "/tmp/bilibili-cookies.txt", "", "https://www.bilibili.com/video/BV1xx")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--cookies /tmp/bilibili-cookies.txt") {
		t.Fatalf("args missing cookies path: %v", args)
	}
}

func TestBuildArgsIncludesProxyWhenConfigured(t *testing.T) {
	args := buildArgs("ffmpeg", "", "http://127.0.0.1:7890", "https://www.youtube.com/watch?v=test")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--proxy http://127.0.0.1:7890") {
		t.Fatalf("args missing proxy url: %v", args)
	}
}

func TestBuildArgsLimitsDownloadedVideoFormat(t *testing.T) {
	args := buildArgs("ffmpeg", "", "", "https://www.youtube.com/watch?v=test")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--format") {
		t.Fatalf("args missing format selector: %v", args)
	}
	if !strings.Contains(joined, "height<=720") {
		t.Fatalf("format selector should cap video height: %v", args)
	}
}

func TestBuildArgsDoesNotDisableCertificateChecks(t *testing.T) {
	args := buildArgs("ffmpeg", "", "", "https://www.youtube.com/watch?v=test")
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "--no-check-certificate") {
		t.Fatalf("args should not disable certificate checks: %v", args)
	}
}

func TestDownloadErrorExplainsBilibili412(t *testing.T) {
	err := formatDownloadError(errors.New("exit status 1"), "ERROR: [BiliBili] Unable to download webpage: HTTP Error 412: Precondition Failed")

	if !strings.Contains(err.Error(), "B 站返回 412") {
		t.Fatalf("error missing Bilibili 412 explanation: %v", err)
	}
	if !strings.Contains(err.Error(), "cookies") {
		t.Fatalf("error missing cookies hint: %v", err)
	}
}

func TestDownloadErrorExplainsYouTubeNetworkUnreachable(t *testing.T) {
	err := formatDownloadError(errors.New("exit status 1"), "WARNING: [youtube] [Errno 101] Network is unreachable")

	if !strings.Contains(err.Error(), "服务器直连 YouTube 失败") {
		t.Fatalf("error missing YouTube network explanation: %v", err)
	}
	if !strings.Contains(err.Error(), "tools.proxy_url") {
		t.Fatalf("error missing proxy config hint: %v", err)
	}
}
