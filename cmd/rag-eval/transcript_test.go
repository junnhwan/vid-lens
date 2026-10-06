package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vid-lens/internal/eval"
)

func TestTranscriptCLIIsOfflineAndNeverOverwritesReport(t *testing.T) {
	if err := runTranscriptCommand([]string{}); err == nil {
		t.Fatal("missing identity accepted")
	}
	root := t.TempDir()
	identity := map[string]string{}
	for _, k := range []string{"code_commit", "patch_sha256", "asr_model", "alignment_model", "assembler_version", "source_mapping_version", "index_version", "config_sha256", "hardware", "preprocessing_version", "annotation_sha256"} {
		identity[k] = "fixture"
	}
	b, _ := json.Marshal(identity)
	p := filepath.Join(root, "identity.json")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--dataset", "../../docs/eval/transcript-cases.dev.json", "--identity", p, "--output", filepath.Join(root, "report.json"), "--review-output", filepath.Join(root, "review.html")}
	if err := runTranscriptCommand(args); err != nil {
		t.Fatal(err)
	}
	if err := runTranscriptCommand(args); err == nil {
		t.Fatal("report overwrite accepted")
	}
	data, err := os.ReadFile("../../docs/eval/transcript-cases.dev.json")
	if err != nil {
		t.Fatal(err)
	}
	var dataset eval.TranscriptDataset
	if err = json.Unmarshal(data, &dataset); err != nil {
		t.Fatal(err)
	}
	dataset.Cases = dataset.Cases[:1]
	c := &dataset.Cases[0]
	c.Kind = "media"
	c.MediaSHA256 = strings.Repeat("a", 64)
	c.ExpectedText = nil
	c.TaskID = 2
	start, end := int64(1234), int64(2345)
	c.Sentences = []eval.TranscriptSentence{{ID: "s1", Text: "audit", SourceIDs: []string{"source"}, StartMS: &start, EndMS: &end, TimeStatus: "exact"}}
	data, _ = json.Marshal(dataset)
	input := filepath.Join(root, "media.json")
	if err = os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = runTranscriptCommand([]string{"--dataset", input, "--identity", p, "--output", filepath.Join(root, "media-report.json"), "--review-output", filepath.Join(root, "media-review.html")}); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(root, "media-review.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "/video/2?t=1234") {
		t.Fatal("review link must preserve the player's millisecond time contract")
	}
}
