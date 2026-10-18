package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func TestExtractorFailureNeverLeavesPublishablePartialFile(t *testing.T) {
	for _, tc := range []struct{ name, script string }{
		{"failed", "printf 'partial media'\nexit 1\n"},
		{"empty", "exit 0\n"},
		{"missing_fragment", "case \" $* \" in *\" --abort-on-unavailable-fragments \"*) exit 1;; *) printf 'silently skipped fragment';; esac\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeExtractor(t, tc.script)
			dst := filepath.Join(t.TempDir(), "video.input")
			err := (&Service{}).downloadStream(context.Background(), "https://www.youtube.com/watch?v=abcdefghijk", "best", dst, 1024)
			var problem *domain.Problem
			if err == nil || !errors.As(err, &problem) || problem.Code != "SOURCE_UNAVAILABLE" {
				t.Fatalf("incomplete source accepted or misclassified: %v", err)
			}
			if _, e := os.Lstat(dst); !os.IsNotExist(e) {
				t.Fatalf("failed download left a publishable destination: %v", e)
			}
		})
	}
}

func TestExtractorFailurePreservesExistingDestination(t *testing.T) {
	fakeExtractor(t, "printf 'new media'\n")
	dst := filepath.Join(t.TempDir(), "video.input")
	if err := os.WriteFile(dst, []byte("existing media"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (&Service{}).downloadStream(ctx, "https://www.youtube.com/watch?v=abcdefghijk", "best", dst, 1024); err == nil {
		t.Fatal("existing destination overwritten")
	}
	raw, err := os.ReadFile(dst)
	if err != nil || string(raw) != "existing media" {
		t.Fatal("unrelated destination changed", err)
	}
}

func TestExtractorByteLimitSurvivesChildCancellation(t *testing.T) {
	fakeExtractor(t, "printf '%2048s' x\n")
	dst := filepath.Join(t.TempDir(), "video.input")
	err := (&Service{}).downloadStream(context.Background(), "https://www.youtube.com/watch?v=abcdefghijk", "best", dst, 16)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "INPUT_LIMIT_EXCEEDED" {
		t.Fatalf("byte-limit failure was masked by internal cancellation: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatal("oversized transfer was published", err)
	}
}
