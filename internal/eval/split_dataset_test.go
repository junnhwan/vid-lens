package eval

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadSplitDatasetKeepsDevFilePhysicallySeparateFromSealedTest(t *testing.T) {
	dataset := validStrictDataset(t)
	devCase := dataset.Cases[0]
	devCase.CaseID = "rag-dev-001"
	devCase.VideoID = "video-dev"
	devCase.SourceGroup = "series-dev"
	devCase.Split = SplitDev
	dataset.Cases = append(dataset.Cases, devCase)
	bindSplitContentHash(t, &dataset, SplitDev)

	manifestRaw, err := MarshalDatasetManifestYAML(dataset)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(manifestRaw, []byte("Which holdout fact is present?")) {
		t.Fatal("manifest file leaked sealed test case content")
	}
	devRaw, err := MarshalSplitDatasetYAML(dataset, SplitDev)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSplitDataset(manifestRaw, devRaw, SplitLoadOptions{
		ExpectedVersion: dataset.DatasetVersion,
		Split:           SplitDev,
	})
	if err != nil {
		t.Fatalf("LoadSplitDataset() error = %v", err)
	}
	if len(loaded.Cases) != 1 || loaded.Cases[0].Split != SplitDev || loaded.Cases[0].CaseID != "rag-dev-001" {
		t.Fatalf("loaded cases = %+v, want only dev split", loaded.Cases)
	}
	if _, ok := loaded.LoadedSplit(); !ok {
		t.Fatal("loaded dataset must retain split-scoped validation state")
	}
}

func TestLoadSplitDatasetRequiresTokenAndAppendOnlyRegistryForTest(t *testing.T) {
	dataset := validStrictDataset(t)
	manifestRaw, err := MarshalDatasetManifestYAML(dataset)
	if err != nil {
		t.Fatal(err)
	}
	testRaw, err := MarshalSplitDatasetYAML(dataset, SplitTest)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(t.TempDir(), "sealed-access.jsonl")
	base := SplitLoadOptions{
		ExpectedVersion:    dataset.DatasetVersion,
		Split:              SplitTest,
		AccessRegistryPath: registryPath,
		AccessEvent: SealedAccessEvent{
			OccurredAt: time.Unix(1_700_000_000, 0).UTC(), ExperimentID: "exp-final", RunID: "run-final", Commit: "abc123",
		},
	}

	if _, err := LoadSplitDataset(manifestRaw, testRaw, base); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("missing-token LoadSplitDataset() error = %v", err)
	}
	invalid := base
	invalid.SealedToken = "wrong"
	if _, err := LoadSplitDataset(manifestRaw, testRaw, invalid); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("invalid-token LoadSplitDataset() error = %v", err)
	}
	missingRegistry := base
	missingRegistry.SealedToken = "test-only-token"
	missingRegistry.AccessRegistryPath = ""
	if _, err := LoadSplitDataset(manifestRaw, testRaw, missingRegistry); err == nil || !strings.Contains(err.Error(), "access registry") {
		t.Fatalf("missing-registry LoadSplitDataset() error = %v", err)
	}

	authorized := base
	authorized.SealedToken = "test-only-token"
	loaded, err := LoadSplitDataset(manifestRaw, testRaw, authorized)
	if err != nil {
		t.Fatalf("authorized LoadSplitDataset() error = %v", err)
	}
	if len(loaded.Cases) != 1 || loaded.Cases[0].Split != SplitTest {
		t.Fatalf("loaded test cases = %+v", loaded.Cases)
	}
	registryRaw, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{dataset.DatasetVersion, "run-final", dataset.Manifest.Splits[SplitTest].ContentSHA256} {
		if !bytes.Contains(registryRaw, []byte(want)) {
			t.Fatalf("access registry %s missing %q", registryRaw, want)
		}
	}
	if err := GuardTuningAllowed(registryPath, dataset.DatasetVersion); err == nil {
		t.Fatal("test access must block further tuning for the same dataset version")
	}
}

func TestLoadSplitDatasetRejectsContentHashOrSplitMismatch(t *testing.T) {
	dataset := validStrictDataset(t)
	manifestRaw, err := MarshalDatasetManifestYAML(dataset)
	if err != nil {
		t.Fatal(err)
	}
	testRaw, err := MarshalSplitDatasetYAML(dataset, SplitTest)
	if err != nil {
		t.Fatal(err)
	}

	tampered := bytes.Replace(testRaw, []byte("Which holdout fact is present?"), []byte("Tampered question?"), 1)
	_, err = LoadSplitDataset(manifestRaw, tampered, SplitLoadOptions{
		ExpectedVersion: dataset.DatasetVersion, Split: SplitTest, SealedToken: "test-only-token",
		AccessRegistryPath: filepath.Join(t.TempDir(), "access.jsonl"),
		AccessEvent:        SealedAccessEvent{OccurredAt: time.Now(), ExperimentID: "exp", RunID: "run", Commit: "abc"},
	})
	if err == nil || !strings.Contains(err.Error(), "content sha256") {
		t.Fatalf("tampered LoadSplitDataset() error = %v, want content hash mismatch", err)
	}

	devDocument := SplitDatasetDocument{SchemaVersion: "1", DatasetVersion: dataset.DatasetVersion, Split: SplitDev, Cases: nil}
	devRaw, err := MarshalSplitDatasetDocumentYAML(devDocument)
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadSplitDataset(manifestRaw, devRaw, SplitLoadOptions{ExpectedVersion: dataset.DatasetVersion, Split: SplitTrain})
	if err == nil || !strings.Contains(err.Error(), "split") {
		t.Fatalf("split mismatch LoadSplitDataset() error = %v", err)
	}
}

func bindSplitContentHash(t *testing.T, dataset *Dataset, split Split) {
	t.Helper()
	hash, err := ComputeSplitContentSHA256(*dataset, split)
	if err != nil {
		t.Fatal(err)
	}
	definition := dataset.Manifest.Splits[split]
	definition.ContentSHA256 = hash
	dataset.Manifest.Splits[split] = definition
}

func TestRunnerRequiresRegisteredSealedAccessBeforeExecutingTest(t *testing.T) {
	dataset := validStrictDataset(t)
	executions := 0
	runner := Runner{Executor: CaseExecutorFunc(func(_ context.Context, c Case) (EvaluationCaseResult, error) {
		executions++
		return EvaluationCaseResult{Case: c}, nil
	})}
	metadata := sealedTestRunMetadata()
	_, err := runner.Run(t.Context(), dataset, SplitTest, metadata, MetricConfig{K: 5, BoundaryToleranceMS: 500, MaxChunkDurationMS: 30_000, MinEvidenceCoverage: 0.5})
	if err == nil || !strings.Contains(err.Error(), "sealed test access") {
		t.Fatalf("Runner.Run() error = %v, want registered sealed access gate", err)
	}
	if executions != 0 {
		t.Fatalf("executor ran %d times before sealed access gate", executions)
	}

	manifestRaw, _ := MarshalDatasetManifestYAML(dataset)
	testRaw, _ := MarshalSplitDatasetYAML(dataset, SplitTest)
	loaded, err := LoadSplitDataset(manifestRaw, testRaw, SplitLoadOptions{
		ExpectedVersion: dataset.DatasetVersion, Split: SplitTest, SealedToken: "test-only-token",
		AccessRegistryPath: filepath.Join(t.TempDir(), "access.jsonl"),
		AccessEvent:        SealedAccessEvent{OccurredAt: time.Now(), ExperimentID: "exp-final", RunID: metadata.RunID, Commit: metadata.Commit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(t.Context(), loaded, SplitTest, metadata, MetricConfig{K: 5, BoundaryToleranceMS: 500, MaxChunkDurationMS: 30_000, MinEvidenceCoverage: 0.5}); err != nil {
		t.Fatalf("Runner.Run() after registered access error = %v", err)
	}
}

func sealedTestRunMetadata() RunMetadata {
	return RunMetadata{
		RunID: "run-final", Commit: "abc123", DatasetVersion: "rag-v1",
		DatasetSHA256: strings.Repeat("1", 64), SourceManifestSHA256: strings.Repeat("2", 64),
		CorpusSHA256: strings.Repeat("3", 64), ChunkManifestSHA256: strings.Repeat("4", 64),
		VectorArtifactSHA256: strings.Repeat("5", 64), ConfigSHA256: strings.Repeat("6", 64),
		Split: SplitTest, Environment: "test", ExperimentID: "exp-final", VariantID: "candidate",
		Models: ModelMetadata{Embedding: ModelRef{Provider: "fixture", Name: "embedding"}},
		Milvus: MilvusMetadata{Collection: "eval", IndexType: "HNSW", MetricType: "COSINE"},
		Prompt: PromptMetadata{Name: "answer", Version: "v1", SHA256: strings.Repeat("7", 64)},
	}
}

func TestAppendSealedAccessRequiresAuditableIdentityAndDigests(t *testing.T) {
	valid := SealedAccessEvent{
		OccurredAt: time.Now(), DatasetVersion: "rag-v1",
		DatasetSHA256: strings.Repeat("a", 64), TestContentSHA256: strings.Repeat("b", 64),
		ExperimentID: "exp-final", RunID: "run-final", Commit: "abc123",
	}
	for _, tt := range []struct {
		name string
		edit func(*SealedAccessEvent)
		want string
	}{
		{name: "dataset digest", edit: func(e *SealedAccessEvent) { e.DatasetSHA256 = "" }, want: "dataset_sha256"},
		{name: "test digest", edit: func(e *SealedAccessEvent) { e.TestContentSHA256 = strings.Repeat("G", 64) }, want: "test_content_sha256"},
		{name: "experiment", edit: func(e *SealedAccessEvent) { e.ExperimentID = "" }, want: "experiment_id"},
		{name: "commit", edit: func(e *SealedAccessEvent) { e.Commit = "" }, want: "commit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			event := valid
			tt.edit(&event)
			path := filepath.Join(t.TempDir(), "access.jsonl")
			err := AppendSealedAccess(path, event)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("AppendSealedAccess() error = %v, want %q", err, tt.want)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("invalid event created registry file: %v", statErr)
			}
		})
	}
}
