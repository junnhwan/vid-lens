package runtimecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"vid-lens/internal/config"
)

func init() {
	if os.Getenv("VIDLENS_CHECK_FIXTURE") != "1" {
		return
	}
	if p := os.Getenv("VIDLENS_CHECK_COUNT"); p != "" {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		fmt.Fprintln(f, "check")
		f.Close()
	}
	switch os.Args[len(os.Args)-1] {
	case "--list-langs":
		fmt.Println("eng\nchi_sim")
	case "--check":
		fmt.Println(`{"available":true,"health":"selfcheck_ok","revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	default:
		fmt.Println("tool 1.2.3 /private/local-model /secret/path")
	}
	os.Exit(0)
}
func TestInspectorChecksOnceAndNeverRunsInference(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool directory with spaces")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	ff := filepath.Join(dir, "ffmpeg"+suffix)
	probe := filepath.Join(dir, "ffprobe"+suffix)
	for _, path := range []string{ff, probe} {
		if err := os.WriteFile(path, data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("VIDLENS_CHECK_FIXTURE", "1")
	count := filepath.Join(dir, "calls")
	t.Setenv("VIDLENS_CHECK_COUNT", count)
	inspector := New(config.ToolsConfig{FFmpegPath: ff, OCRPath: ff, OCRLang: "chi_sim+eng", YtDlpPath: ff, TranscriptAlignerCommand: []string{ff}})
	first := inspector.Snapshot(context.Background())
	before, _ := os.ReadFile(count)
	second := inspector.Snapshot(context.Background())
	after, _ := os.ReadFile(count)
	if string(before) != string(after) {
		t.Fatal("cached GET repeated commands")
	}
	for key, state := range first {
		if !state.Available || state.Health != "selfcheck_ok" || state.CheckedAt != second[key].CheckedAt {
			t.Fatalf("%s: %+v", key, state)
		}
	}
	public, _ := json.Marshal(first)
	if strings.Contains(string(public), "private") || strings.Contains(string(public), dir) {
		t.Fatal("private paths leaked")
	}
}
func TestMissingOptionalToolsAreExplained(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	i := New(config.ToolsConfig{FFmpegPath: missing, TranscriptAlignerCommand: []string{missing}})
	states := i.Snapshot(context.Background())
	if states["alignment"].ReasonCode != "dependency_missing" || states["ocr"].Available || states["url_import"].Available {
		t.Fatalf("%+v", states)
	}
}
