package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	rageval "vid-lens/internal/eval"
)

type legacyBaselineManifest struct {
	SchemaVersion  string `json:"schema_version"`
	BaselineID     string `json:"baseline_id"`
	Classification struct {
		Mode                   string   `json:"mode"`
		Strict                 bool     `json:"strict"`
		ResumeEvidenceEligible bool     `json:"resume_evidence_eligible"`
		Limitations            []string `json:"limitations"`
	} `json:"classification"`
	Provenance struct {
		EvaluatedCommit string  `json:"evaluated_commit"`
		ReportCommit    string  `json:"report_commit"`
		DatasetPath     string  `json:"dataset_path"`
		DatasetSHA256   string  `json:"dataset_sha256"`
		ReportPath      string  `json:"report_path"`
		ReportSHA256    string  `json:"report_sha256"`
		CaseCount       int     `json:"case_count"`
		TaskIDs         []int64 `json:"task_ids"`
	} `json:"provenance"`
	RetrievalConfig       map[string]any `json:"retrieval_config"`
	RetrievalConfigSHA256 string         `json:"retrieval_config_sha256"`
}

func TestLegacyBaselineManifestFreezesFilesAndRejectsResumeUse(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestPath := filepath.Join(root, "docs", "eval", "legacy-baseline-manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest legacyBaselineManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "1" || manifest.BaselineID == "" {
		t.Fatalf("invalid manifest identity: %+v", manifest)
	}
	if manifest.Classification.Mode != "legacy/non-strict" || manifest.Classification.Strict || manifest.Classification.ResumeEvidenceEligible {
		t.Fatalf("legacy baseline must be marked non-strict and ineligible for resume evidence: %+v", manifest.Classification)
	}
	if len(manifest.Classification.Limitations) == 0 {
		t.Fatal("legacy baseline must record limitations")
	}
	if manifest.Provenance.EvaluatedCommit != "e962ebd0f491051056bf7ebfac7dd782c32a8124" {
		t.Fatalf("evaluated commit = %q", manifest.Provenance.EvaluatedCommit)
	}
	if manifest.Provenance.ReportCommit != "e569a10402f725bf9e21cdaf271bfa4a43925f67" {
		t.Fatalf("report commit = %q", manifest.Provenance.ReportCommit)
	}
	if manifest.Provenance.CaseCount != 50 || len(manifest.Provenance.TaskIDs) != 3 {
		t.Fatalf("legacy dataset inventory = %+v", manifest.Provenance)
	}

	assertFileDigest := func(path, want string) {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Fatalf("%s digest = %s, want %s", path, got, want)
		}
	}
	assertFileDigest(manifest.Provenance.DatasetPath, manifest.Provenance.DatasetSHA256)
	assertFileDigest(manifest.Provenance.ReportPath, manifest.Provenance.ReportSHA256)

	datasetRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifest.Provenance.DatasetPath)))
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := rageval.LoadDataset(datasetRaw, rageval.LoadOptions{Mode: rageval.LoadModeLegacy})
	if err != nil {
		t.Fatal(err)
	}
	if !dataset.Legacy || len(dataset.Cases) != manifest.Provenance.CaseCount {
		t.Fatalf("loaded legacy dataset: legacy=%v cases=%d", dataset.Legacy, len(dataset.Cases))
	}
	if _, err := rageval.LoadDataset(datasetRaw, rageval.LoadOptions{Mode: rageval.LoadModeStrict, DatasetVersion: "legacy-2026-06-28"}); err == nil {
		t.Fatal("legacy list unexpectedly passed strict dataset validation")
	}

	canonicalConfig, err := json.Marshal(manifest.RetrievalConfig)
	if err != nil {
		t.Fatal(err)
	}
	configSum := sha256.Sum256(canonicalConfig)
	if got := hex.EncodeToString(configSum[:]); got != manifest.RetrievalConfigSHA256 {
		t.Fatalf("retrieval config digest = %s, want %s", got, manifest.RetrievalConfigSHA256)
	}
}
