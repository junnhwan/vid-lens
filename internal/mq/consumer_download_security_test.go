package mq

import (
	"context"
	"net"
	"strings"
	"testing"
)

type downloadURLTestResolver map[string][]net.IP

func (r downloadURLTestResolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return r["www.youtube.com"], nil
}

func TestConsumerValidatesDownloadURLBeforeExternalDownloader(t *testing.T) {
	consumer := &Consumer{}
	consumer.SetDownloadURLPolicy(nil, downloadURLTestResolver{"www.youtube.com": {net.ParseIP("203.0.113.10")}})
	sanitized, err := consumer.validateDownloadURL(context.Background(), "https://www.youtube.com/watch?v=video&token=secret")
	if err != nil {
		t.Fatalf("validateDownloadURL() error = %v", err)
	}
	if sanitized != "https://www.youtube.com/watch?v=video" {
		t.Fatalf("sanitized URL = %q", sanitized)
	}

	consumer.SetDownloadURLPolicy([]string{"www.youtube.com"}, downloadURLTestResolver{"www.youtube.com": {net.ParseIP("10.0.0.2")}})
	if _, err := consumer.validateDownloadURL(context.Background(), "https://www.youtube.com/watch?v=video"); err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("validateDownloadURL() error = %v, want private-address rejection", err)
	}
	if _, err := consumer.callDownloadVideo(context.Background(), "https://www.youtube.com/watch?v=video"); err == nil || !strings.Contains(err.Error(), "安全校验") {
		t.Fatalf("callDownloadVideo() error = %v, want validation before yt-dlp execution", err)
	}
}
