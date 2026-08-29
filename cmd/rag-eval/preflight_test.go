package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"

	"gorm.io/gorm"
)

type fakeEvalPreflightTasks struct {
	tasks map[int64]*model.VideoTask
	err   error
}

func (f fakeEvalPreflightTasks) FindByID(id int64) (*model.VideoTask, error) {
	if f.err != nil {
		return nil, f.err
	}
	task, ok := f.tasks[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return task, nil
}

type fakeEvalPreflightChunks struct {
	models map[[2]int64][]string
	chunks map[[3]interface{}][]repository.ChunkEvidenceManifestEntry
	err    error
}

func (f fakeEvalPreflightChunks) ListEmbeddingModelsByTask(userID, taskID int64) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.models[[2]int64{userID, taskID}], nil
}

func (f fakeEvalPreflightChunks) ListEvidenceManifest(userID, taskID int64, embeddingModel string) ([]repository.ChunkEvidenceManifestEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.chunks[[3]interface{}{userID, taskID, embeddingModel}], nil
}

type fakeEvalPreflightProfiles struct {
	profiles map[int64]*ai.Profile
	err      error
}

func (f fakeEvalPreflightProfiles) GetDefaultAIProfile(userID int64) (*ai.Profile, error) {
	if f.err != nil {
		return nil, f.err
	}
	profile, ok := f.profiles[userID]
	if !ok {
		return nil, errors.New("profile not found")
	}
	return profile, nil
}

type fakeEvalPreflightVectors struct {
	entries map[[3]interface{}][]service.RAGVectorManifestEntry
	err     error
}

func (f fakeEvalPreflightVectors) ListTaskVectorManifest(_ context.Context, userID, taskID int64, embeddingModel string) ([]service.RAGVectorManifestEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries[[3]interface{}{userID, taskID, embeddingModel}], nil
}

func TestPreflightCasesAcceptsMatchingLiveEvidence(t *testing.T) {
	cases := []evalCase{
		{TaskID: 14, Question: "q1", ExpectedChunkKeywords: []string{"one"}},
		{TaskID: 14, Question: "q2", ExpectedChunkKeywords: []string{"two"}},
	}
	chunks := []repository.ChunkEvidenceManifestEntry{{UserID: 5, TaskID: 14, ChunkID: 21, ChunkIndex: 0, EvidenceID: "e1", ContentHash: "h1", EmbeddingModel: "embed-v1"}}
	vectors := []service.RAGVectorManifestEntry{{UserID: 5, TaskID: 14, ChunkID: 21, ChunkIndex: 0, EvidenceID: "e1", ContentHash: "h1", EmbeddingModel: "embed-v1"}}
	report, err := preflightCases(context.Background(), cases, evalPreflightSources{
		tasks: fakeEvalPreflightTasks{tasks: map[int64]*model.VideoTask{14: {ID: 14, UserID: 5}}},
		chunks: fakeEvalPreflightChunks{
			models: map[[2]int64][]string{{int64(5), int64(14)}: {"embed-v1"}},
			chunks: map[[3]interface{}][]repository.ChunkEvidenceManifestEntry{{int64(5), int64(14), "embed-v1"}: chunks},
		},
		profiles: fakeEvalPreflightProfiles{profiles: map[int64]*ai.Profile{5: {EmbeddingModel: "embed-v1"}}},
		vectors:  fakeEvalPreflightVectors{entries: map[[3]interface{}][]service.RAGVectorManifestEntry{{int64(5), int64(14), "embed-v1"}: vectors}},
	}, "pgvector")
	if err != nil {
		t.Fatalf("preflightCases() error = %v", err)
	}
	if !report.Valid() || report.ReadyCases != 2 || report.InvalidCases != 0 {
		t.Fatalf("report = %+v, want valid two-case report", report)
	}
}

func TestCompareEvalManifestsRejectsUnexpectedVectorScope(t *testing.T) {
	source := []repository.ChunkEvidenceManifestEntry{{UserID: 5, TaskID: 14, ChunkID: 21, ChunkIndex: 0, EvidenceID: "e1", ContentHash: "h1", EmbeddingModel: "embed-v1"}}
	target := []service.RAGVectorManifestEntry{{UserID: 99, TaskID: 7, ChunkID: 21, ChunkIndex: 0, EvidenceID: "e1", ContentHash: "h1", EmbeddingModel: "wrong-model"}}
	issues := compareEvalManifests(source, target, "pgvector", 5, 14, "embed-v1")
	message := strings.Join(issues, "; ")
	for _, want := range []string{"unexpected user_id", "unexpected task_id", "unexpected embedding model"} {
		if !strings.Contains(message, want) {
			t.Fatalf("issues = %q, missing %q", message, want)
		}
	}
}

func TestPreflightCasesAggregatesMissingAndDriftIssues(t *testing.T) {
	cases := []evalCase{
		{TaskID: 5, Question: "deleted", ExpectedChunkKeywords: []string{"x"}},
		{TaskID: 14, Question: "model mismatch", ExpectedChunkKeywords: []string{"y"}},
	}
	report, err := preflightCases(context.Background(), cases, evalPreflightSources{
		tasks: fakeEvalPreflightTasks{tasks: map[int64]*model.VideoTask{14: {ID: 14, UserID: 5}}},
		chunks: fakeEvalPreflightChunks{
			models: map[[2]int64][]string{{int64(5), int64(14)}: {"old-model"}},
			chunks: map[[3]interface{}][]repository.ChunkEvidenceManifestEntry{{int64(5), int64(14), "old-model"}: {{TaskID: 14, ChunkIndex: 0, EvidenceID: "e1", ContentHash: "source-hash"}}},
		},
		profiles: fakeEvalPreflightProfiles{profiles: map[int64]*ai.Profile{5: {EmbeddingModel: "new-model"}}},
		vectors:  fakeEvalPreflightVectors{entries: map[[3]interface{}][]service.RAGVectorManifestEntry{{5, 14, "new-model"}: nil}},
	}, "pgvector")
	if err != nil {
		t.Fatalf("preflightCases() error = %v", err)
	}
	if report.Valid() || report.ReadyCases != 0 || report.InvalidCases != 2 {
		t.Fatalf("report = %+v, want two invalid cases", report)
	}
	message := report.Error()
	for _, want := range []string{"task 5", "not found", "task 14", "embedding model", "old-model", "new-model"} {
		if !strings.Contains(message, want) {
			t.Fatalf("report error missing %q: %s", want, message)
		}
	}
}

func TestParseEvalFlagsSupportsPreflightOnlyAndRejectsStrictCombination(t *testing.T) {
	opts, err := parseEvalFlags([]string{"--preflight-only", "--cases", "cases.yaml"})
	if err != nil {
		t.Fatalf("parseEvalFlags() error = %v", err)
	}
	if !opts.preflightOnly || opts.casesPath != "cases.yaml" {
		t.Fatalf("opts = %+v", opts)
	}
	if _, err := parseEvalFlags([]string{"--strict", "--preflight-only", "--dataset-version", "v1"}); err == nil || !strings.Contains(err.Error(), "preflight-only") {
		t.Fatalf("strict preflight should be rejected, error = %v", err)
	}
}
