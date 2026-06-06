package service

import "testing"

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
