package ytdlp

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildArgsIncludesCookiesWhenConfigured(t *testing.T) {
	args := buildArgs("ffmpeg", "/tmp/bilibili-cookies.txt", "https://www.bilibili.com/video/BV1xx")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--cookies /tmp/bilibili-cookies.txt") {
		t.Fatalf("args missing cookies path: %v", args)
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
