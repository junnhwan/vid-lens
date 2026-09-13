package storage

import "testing"

func TestContentTypeForObjectCoversStoredMedia(t *testing.T) {
	tests := []struct {
		object string
		want   string
	}{
		{object: "videos/abc.mp4", want: "video/mp4"},
		{object: "videos/ABC.MP4", want: "video/mp4"},
		{object: "audio/part.mp3", want: "audio/mpeg"},
		{object: "audio/part.wav", want: "audio/wav"},
		{object: "videos/no-extension", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.object, func(t *testing.T) {
			if got := contentTypeForObject(tt.object); got != tt.want {
				t.Fatalf("contentTypeForObject(%q) = %q, want %q", tt.object, got, tt.want)
			}
		})
	}
}
