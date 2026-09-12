package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"vid-lens/internal/eval"
)

func TestProductCandidatesExportFailuresBindsDatasetAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	dataset := filepath.Join(dir, "cases.json")
	report := filepath.Join(dir, "report.json")
	out := filepath.Join(dir, "candidates.json")
	cases := []eval.ProductCase{{Version: eval.ProductSchemaVersion, ID: "failed", SourceGroup: "video-1", AssetVersion: "hash", Split: "dev", Turns: []eval.ProductTurn{{UserID: 7, SessionID: 1, Kind: "agent", Question: "q", Stream: true}}}}
	b, _ := json.Marshal(cases)
	os.WriteFile(dataset, b, 0600)
	digest := sha256.Sum256(b)
	payload := map[string]any{"dataset_sha256": hex.EncodeToString(digest[:]), "results": []eval.ProductResult{{CaseID: "failed", SourceGroup: "video-1", Classification: "failed"}}}
	b, _ = json.Marshal(payload)
	os.WriteFile(report, b, 0600)
	args := []string{"export", "--dataset", dataset, "--results", report, "--output", out}
	if e := runProductCandidatesCommand(context.Background(), args); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(out)
	if e != nil {
		t.Fatal(e)
	}
	var exported []exportedProductCandidate
	if e = json.Unmarshal(b, &exported); e != nil || len(exported) != 1 || exported[0].Candidate.Feedback != nil {
		t.Fatalf("bad export %s %v", b, e)
	}
	if e = runProductCandidatesCommand(context.Background(), args); e == nil {
		t.Fatal("overwrote frozen export")
	}
	os.WriteFile(dataset, []byte("[]"), 0600)
	args[len(args)-1] = filepath.Join(dir, "changed.json")
	if e = runProductCandidatesCommand(context.Background(), args); e == nil {
		t.Fatal("accepted mismatched source dataset")
	}
}
