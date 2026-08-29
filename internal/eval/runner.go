package eval

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ModelRef struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

type ModelMetadata struct {
	Embedding ModelRef `json:"embedding"`
	LLM       ModelRef `json:"llm,omitempty"`
	OCR       ModelRef `json:"ocr,omitempty"`
}

type MilvusMetadata struct {
	Collection string            `json:"collection"`
	Partition  string            `json:"partition,omitempty"`
	IndexType  string            `json:"index_type"`
	MetricType string            `json:"metric_type"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

type PromptMetadata struct {
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	SHA256      string  `json:"sha256"`
	Temperature float64 `json:"temperature"`
	Seed        *int64  `json:"seed,omitempty"`
}

type RunMetadata struct {
	RunID                string         `json:"run_id"`
	StartedAt            time.Time      `json:"started_at"`
	CompletedAt          time.Time      `json:"completed_at"`
	Commit               string         `json:"commit"`
	DatasetVersion       string         `json:"dataset_version"`
	DatasetSHA256        string         `json:"dataset_sha256"`
	SourceManifestSHA256 string         `json:"source_manifest_sha256"`
	CorpusSHA256         string         `json:"corpus_sha256"`
	ChunkManifestSHA256  string         `json:"chunk_manifest_sha256"`
	VectorArtifactSHA256 string         `json:"vector_artifact_sha256"`
	ConfigSHA256         string         `json:"config_sha256"`
	Split                Split          `json:"split"`
	Environment          string         `json:"environment"`
	ExperimentID         string         `json:"experiment_id"`
	VariantID            string         `json:"variant_id"`
	Models               ModelMetadata  `json:"models"`
	Milvus               MilvusMetadata `json:"milvus"`
	Prompt               PromptMetadata `json:"prompt"`
}

func (m RunMetadata) Validate() error {
	required := map[string]string{
		"run_id": m.RunID, "commit": m.Commit, "dataset_version": m.DatasetVersion,
		"dataset_sha256": m.DatasetSHA256, "source_manifest_sha256": m.SourceManifestSHA256,
		"corpus_sha256": m.CorpusSHA256, "chunk_manifest_sha256": m.ChunkManifestSHA256,
		"vector_artifact_sha256": m.VectorArtifactSHA256, "config_sha256": m.ConfigSHA256,
		"environment": m.Environment, "experiment_id": m.ExperimentID, "variant_id": m.VariantID,
	}
	var problems []string
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, "missing "+field)
		}
	}
	for field, value := range map[string]string{
		"dataset_sha256": m.DatasetSHA256, "source_manifest_sha256": m.SourceManifestSHA256,
		"corpus_sha256": m.CorpusSHA256, "chunk_manifest_sha256": m.ChunkManifestSHA256,
		"vector_artifact_sha256": m.VectorArtifactSHA256, "config_sha256": m.ConfigSHA256,
		"prompt.sha256": m.Prompt.SHA256,
	} {
		if value != "" && !isSHA256(value) {
			problems = append(problems, field+" must be a 64-character SHA-256 hex digest")
		}
	}
	if !validSplit(m.Split) {
		problems = append(problems, "invalid split")
	}
	if strings.TrimSpace(m.Models.Embedding.Name) == "" || strings.TrimSpace(m.Models.Embedding.Provider) == "" {
		problems = append(problems, "missing embedding model identity")
	}
	if strings.TrimSpace(m.Milvus.Collection) == "" || strings.TrimSpace(m.Milvus.IndexType) == "" || strings.TrimSpace(m.Milvus.MetricType) == "" {
		problems = append(problems, "missing Milvus collection/index/metric metadata")
	}
	if strings.TrimSpace(m.Prompt.Name) == "" || strings.TrimSpace(m.Prompt.Version) == "" || strings.TrimSpace(m.Prompt.SHA256) == "" {
		problems = append(problems, "missing prompt identity")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid run metadata: %s", strings.Join(problems, "; "))
	}
	return nil
}

type ExecutionError struct {
	Stage string
	Code  string
	Err   error
}

func (e *ExecutionError) Error() string {
	if e == nil || e.Err == nil {
		return "evaluation execution failed"
	}
	return e.Err.Error()
}

func (e *ExecutionError) Unwrap() error { return e.Err }

type CaseExecutor interface {
	Execute(context.Context, Case) (EvaluationCaseResult, error)
}

type CaseExecutorFunc func(context.Context, Case) (EvaluationCaseResult, error)

func (f CaseExecutorFunc) Execute(ctx context.Context, c Case) (EvaluationCaseResult, error) {
	return f(ctx, c)
}

type Runner struct {
	Executor CaseExecutor
	Now      func() time.Time
}

type CaseArtifact struct {
	CaseID string               `json:"case_id"`
	Result EvaluationCaseResult `json:"result"`
	Metric CaseMetric           `json:"metric"`
}

type RunArtifact struct {
	Metadata RunMetadata         `json:"metadata"`
	Summary  MetricReport        `json:"summary"`
	Cases    []CaseArtifact      `json:"cases"`
	Analysis *ExperimentAnalysis `json:"analysis,omitempty"`
}

func (r Runner) Run(ctx context.Context, dataset Dataset, split Split, metadata RunMetadata, metricConfig MetricConfig) (RunArtifact, error) {
	if r.Executor == nil {
		return RunArtifact{}, fmt.Errorf("case executor is required")
	}
	if err := ValidateDataset(dataset, ValidationOptions{ExpectedVersion: dataset.DatasetVersion}); err != nil {
		return RunArtifact{}, err
	}
	if !validSplit(split) {
		return RunArtifact{}, fmt.Errorf("invalid split %q", split)
	}
	if split == SplitTest && !dataset.sealedAccessRegistered {
		return RunArtifact{}, fmt.Errorf("sealed test access must be token-authorized and recorded before execution")
	}
	datasetHash, err := ComputeDatasetSHA256(dataset)
	if err != nil {
		return RunArtifact{}, fmt.Errorf("compute dataset sha256: %w", err)
	}
	metadata.DatasetVersion = dataset.DatasetVersion
	metadata.DatasetSHA256 = datasetHash
	metadata.SourceManifestSHA256 = dataset.Manifest.SHA256
	metadata.Split = split
	now := r.Now
	if now == nil {
		now = time.Now
	}
	metadata.StartedAt = now().UTC()
	if metadata.RunID == "" {
		metadata.RunID = fmt.Sprintf("%s-%s-%d", dataset.DatasetVersion, split, metadata.StartedAt.UnixNano())
	}
	if err := metadata.Validate(); err != nil {
		return RunArtifact{}, err
	}

	results := make([]EvaluationCaseResult, 0)
	for _, c := range dataset.Cases {
		if c.Split != split {
			continue
		}
		result, executeErr := r.Executor.Execute(ctx, c)
		result.Case = c
		if executeErr != nil {
			failure := RunFailure{Stage: "execution", Message: executeErr.Error()}
			var typed *ExecutionError
			if errors.As(executeErr, &typed) {
				failure.Stage = typed.Stage
				failure.Code = typed.Code
			}
			result.Failure = &failure
		}
		results = append(results, result)
	}
	summary, err := EvaluateMetrics(results, metricConfig)
	if err != nil {
		return RunArtifact{}, err
	}
	metadata.CompletedAt = now().UTC()
	cases := make([]CaseArtifact, len(results))
	for i := range results {
		cases[i] = CaseArtifact{CaseID: results[i].Case.CaseID, Result: results[i], Metric: summary.Cases[i]}
	}
	return RunArtifact{Metadata: metadata, Summary: summary, Cases: cases}, nil
}

func ComputeDatasetSHA256(dataset Dataset) (string, error) {
	clone := dataset
	clone.Legacy = false
	raw, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type SealedAccessEvent struct {
	OccurredAt        time.Time `json:"occurred_at"`
	DatasetVersion    string    `json:"dataset_version"`
	DatasetSHA256     string    `json:"dataset_sha256"`
	TestContentSHA256 string    `json:"test_content_sha256"`
	ExperimentID      string    `json:"experiment_id"`
	RunID             string    `json:"run_id"`
	Commit            string    `json:"commit"`
}

func AppendSealedAccess(path string, event SealedAccessEvent) error {
	var problems []string
	for field, value := range map[string]string{
		"dataset_version": event.DatasetVersion,
		"experiment_id":   event.ExperimentID,
		"run_id":          event.RunID,
		"commit":          event.Commit,
	} {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, "missing "+field)
		}
	}
	if event.OccurredAt.IsZero() {
		problems = append(problems, "missing occurred_at")
	}
	if err := ValidateSHA256Digest("dataset_sha256", event.DatasetSHA256); err != nil {
		problems = append(problems, err.Error())
	}
	if err := ValidateSHA256Digest("test_content_sha256", event.TestContentSHA256); err != nil {
		problems = append(problems, err.Error())
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid sealed access event: %s", strings.Join(problems, "; "))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func GuardTuningAllowed(path, datasetVersion string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReader(file))
	for {
		var event SealedAccessEvent
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read sealed access registry: %w", err)
		}
		if event.DatasetVersion == datasetVersion {
			return fmt.Errorf("dataset version %q has been unsealed; create a new dataset version before further tuning", datasetVersion)
		}
	}
}

func isSHA256(value string) bool {
	return ValidateSHA256Digest("sha256", value) == nil
}
