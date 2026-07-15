package storage

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestRequiresStreamingMergeForSmallNonFinalSources(t *testing.T) {
	const mib = int64(1024 * 1024)
	tests := []struct {
		name  string
		sizes []int64
		want  bool
	}{
		{name: "single small source is allowed as final part", sizes: []int64{mib}, want: false},
		{name: "one MiB chunks require streaming", sizes: []int64{mib, mib, 98 * 1024}, want: true},
		{name: "five MiB non-final source can use compose", sizes: []int64{5 * mib, 98 * 1024}, want: false},
		{name: "just below five MiB requires streaming", sizes: []int64{5*mib - 1, mib}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requiresStreamingMerge(tt.sizes); got != tt.want {
				t.Fatalf("requiresStreamingMerge(%v) = %v, want %v", tt.sizes, got, tt.want)
			}
		})
	}
}

func TestCopyObjectPartsPreservesSourceOrder(t *testing.T) {
	sources := []minio.CopySrcOptions{
		{Bucket: "videos", Object: "chunks/file/0"},
		{Bucket: "videos", Object: "chunks/file/1"},
		{Bucket: "videos", Object: "chunks/file/2"},
	}
	contents := map[string]string{
		"chunks/file/0": "first-",
		"chunks/file/1": "second-",
		"chunks/file/2": "third",
	}
	var opened []string
	var dst bytes.Buffer

	err := copyObjectParts(context.Background(), &dst, sources, func(_ context.Context, source minio.CopySrcOptions) (io.ReadCloser, error) {
		opened = append(opened, source.Object)
		return io.NopCloser(strings.NewReader(contents[source.Object])), nil
	})
	if err != nil {
		t.Fatalf("copyObjectParts returned error: %v", err)
	}
	if got, want := dst.String(), "first-second-third"; got != want {
		t.Fatalf("merged content = %q, want %q", got, want)
	}
	if got, want := strings.Join(opened, ","), "chunks/file/0,chunks/file/1,chunks/file/2"; got != want {
		t.Fatalf("opened order = %q, want %q", got, want)
	}
}
