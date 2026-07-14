package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerRejectsMissingReproducibilityHashes(t *testing.T) {
	fields := []struct {
		name   string
		mutate func(*RunMetadata)
		want   string
	}{
		{name: "corpus", mutate: func(m *RunMetadata) { m.CorpusSHA256 = "" }, want: "corpus_sha256"},
		{name: "chunk manifest", mutate: func(m *RunMetadata) { m.ChunkManifestSHA256 = "" }, want: "chunk_manifest_sha256"},
		{name: "config", mutate: func(m *RunMetadata) { m.ConfigSHA256 = "" }, want: "config_sha256"},
		{name: "vector artifact", mutate: func(m *RunMetadata) { m.VectorArtifactSHA256 = "" }, want: "vector_artifact_sha256"},
	}

	for _, tt := range fields {
		t.Run(tt.name, func(t *testing.T) {
			metadata := validRunMetadata()
			tt.mutate(&metadata)
			runner := Runner{Executor: CaseExecutorFunc(func(context.Context, Case) (EvaluationCaseResult, error) {
				return EvaluationCaseResult{}, nil
			})}
			_, err := runner.Run(t.Context(), validStrictDataset(t), SplitDev, metadata, validMetricConfig())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Runner.Run() error = %v, want missing %s", err, tt.want)
			}
		})
	}
}

func TestRunnerRecordsPerCaseFailureWithoutDroppingOtherCases(t *testing.T) {
	dataset := validStrictDataset(t)
	dataset.Cases = append(dataset.Cases,
		devMetricCase("dev-ok", "video-dev", "series-dev"),
		devMetricCase("dev-fail", "video-dev", "series-dev"),
	)
	runner := Runner{
		Now: func() time.Time { return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) },
		Executor: CaseExecutorFunc(func(_ context.Context, c Case) (EvaluationCaseResult, error) {
			if c.CaseID == "dev-fail" {
				return EvaluationCaseResult{}, &ExecutionError{Stage: "retrieval", Code: "provider_timeout", Err: errors.New("timeout")}
			}
			return EvaluationCaseResult{
				Retrieved:           []RetrievedContext{{ContextID: "ctx-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR}},
				PredictedAnswerable: true,
			}, nil
		}),
	}

	artifact, err := runner.Run(t.Context(), dataset, SplitDev, validRunMetadata(), validMetricConfig())
	if err != nil {
		t.Fatalf("Runner.Run() error = %v", err)
	}
	if len(artifact.Cases) != 2 || artifact.Summary.Overall.Cases != 2 || artifact.Summary.Overall.FailedCases != 1 {
		t.Fatalf("artifact counts = cases:%d summary:%+v", len(artifact.Cases), artifact.Summary.Overall)
	}
	if artifact.Cases[1].Result.Failure == nil || artifact.Cases[1].Result.Failure.Stage != "retrieval" || artifact.Cases[1].Result.Failure.Code != "provider_timeout" {
		t.Fatalf("failure = %+v, want typed retrieval timeout", artifact.Cases[1].Result.Failure)
	}
	if artifact.Metadata.StartedAt.IsZero() || artifact.Metadata.CompletedAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", artifact.Metadata)
	}
}

func TestWriteArtifactsEmitsTraceableJSONLJSONCSVAndMarkdown(t *testing.T) {
	dataset := validStrictDataset(t)
	dataset.Cases = append(dataset.Cases, devMetricCase("dev-ok", "video-dev", "series-dev"))
	runner := Runner{Executor: CaseExecutorFunc(func(_ context.Context, _ Case) (EvaluationCaseResult, error) {
		return EvaluationCaseResult{
			Retrieved:           []RetrievedContext{{ContextID: "ctx-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR}},
			PredictedAnswerable: true,
		}, nil
	})}
	artifact, err := runner.Run(t.Context(), dataset, SplitDev, validRunMetadata(), validMetricConfig())
	if err != nil {
		t.Fatalf("Runner.Run() error = %v", err)
	}

	paths, err := WriteArtifacts(t.TempDir(), artifact)
	if err != nil {
		t.Fatalf("WriteArtifacts() error = %v", err)
	}
	for label, path := range map[string]string{
		"metadata": paths.MetadataJSON,
		"cases":    paths.CasesJSONL,
		"summary":  paths.SummaryJSON,
		"csv":      paths.SummaryCSV,
		"markdown": paths.ReportMarkdown,
	} {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() == 0 {
			t.Fatalf("%s artifact %q stat = %v size=%v", label, path, statErr, func() int64 {
				if info == nil {
					return 0
				}
				return info.Size()
			}())
		}
	}

	metadataRaw, err := os.ReadFile(paths.MetadataJSON)
	if err != nil {
		t.Fatal(err)
	}
	var metadata RunMetadata
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		t.Fatalf("metadata JSON error = %v", err)
	}
	if metadata.Commit != "abc123" || metadata.DatasetSHA256 == "" || metadata.Milvus.Collection != "video_chunks_eval" || metadata.Prompt.SHA256 == "" {
		t.Fatalf("metadata = %+v, missing trace fields", metadata)
	}
	casesRaw, _ := os.ReadFile(paths.CasesJSONL)
	if lines := strings.Split(strings.TrimSpace(string(casesRaw)), "\n"); len(lines) != 1 || !strings.Contains(lines[0], `"case_id":"dev-ok"`) {
		t.Fatalf("cases JSONL = %q", casesRaw)
	}
	markdown, _ := os.ReadFile(paths.ReportMarkdown)
	for _, want := range []string{"Per-video metrics", "Failed cases", "dev-ok", "abc123"} {
		if !strings.Contains(string(markdown), want) {
			t.Fatalf("report missing %q:\n%s", want, markdown)
		}
	}
}

func TestWriteArtifactsRefusesToOverwriteRunDirectory(t *testing.T) {
	artifact := RunArtifact{Metadata: validRunMetadata()}
	artifact.Metadata.RunID = "fixed-run"
	root := t.TempDir()
	if _, err := WriteArtifacts(root, artifact); err != nil {
		t.Fatalf("first WriteArtifacts() error = %v", err)
	}
	if _, err := WriteArtifacts(root, artifact); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second WriteArtifacts() error = %v, want no-overwrite error", err)
	}
}

func TestSealedAccessRegistryIsAppendOnlyAndBlocksFurtherTuning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sealed-access.jsonl")
	event := SealedAccessEvent{
		OccurredAt:        time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		DatasetVersion:    "rag-v1",
		DatasetSHA256:     strings.Repeat("a", 64),
		TestContentSHA256: strings.Repeat("b", 64),
		ExperimentID:      "exp-1",
		RunID:             "run-1",
		Commit:            "abc123",
	}
	if err := AppendSealedAccess(path, event); err != nil {
		t.Fatalf("AppendSealedAccess(first) error = %v", err)
	}
	second := event
	second.RunID = "run-2"
	second.OccurredAt = second.OccurredAt.Add(time.Hour)
	if err := AppendSealedAccess(path, second); err != nil {
		t.Fatalf("AppendSealedAccess(second) error = %v", err)
	}
	raw, _ := os.ReadFile(path)
	if lines := strings.Split(strings.TrimSpace(string(raw)), "\n"); len(lines) != 2 {
		t.Fatalf("registry lines = %d, want 2: %s", len(lines), raw)
	}
	if err := GuardTuningAllowed(path, "rag-v1"); err == nil || !strings.Contains(err.Error(), "new dataset version") {
		t.Fatalf("GuardTuningAllowed() error = %v, want consumed-version rejection", err)
	}
	if err := GuardTuningAllowed(path, "rag-v2"); err != nil {
		t.Fatalf("GuardTuningAllowed(new version) error = %v", err)
	}
}

func validRunMetadata() RunMetadata {
	return RunMetadata{
		RunID:                "run-dev-baseline",
		Commit:               "abc123",
		DatasetVersion:       "rag-v1",
		DatasetSHA256:        strings.Repeat("1", 64),
		SourceManifestSHA256: strings.Repeat("2", 64),
		CorpusSHA256:         strings.Repeat("3", 64),
		ChunkManifestSHA256:  strings.Repeat("4", 64),
		VectorArtifactSHA256: strings.Repeat("5", 64),
		ConfigSHA256:         strings.Repeat("6", 64),
		Split:                SplitDev,
		Environment:          "test",
		ExperimentID:         "exp-1",
		VariantID:            "vector-only",
		Models:               ModelMetadata{Embedding: ModelRef{Provider: "openai-compatible", Name: "embedding-model", Version: "fixture"}},
		Milvus:               MilvusMetadata{Collection: "video_chunks_eval", Partition: "dev", IndexType: "HNSW", MetricType: "COSINE", Parameters: map[string]string{"M": "16"}},
		Prompt:               PromptMetadata{Name: "rag-answer", Version: "v1", SHA256: strings.Repeat("7", 64), Temperature: 0},
	}
}

func validMetricConfig() MetricConfig {
	return MetricConfig{K: 5, BoundaryToleranceMS: 500, MaxChunkDurationMS: 30_000, MinEvidenceCoverage: 0.5}
}

func devMetricCase(id, videoID, sourceGroup string) Case {
	c := metricCase()
	c.CaseID = id
	c.VideoID = videoID
	c.SourceGroup = sourceGroup
	c.Split = SplitDev
	c.Difficulty = "medium"
	return c
}
