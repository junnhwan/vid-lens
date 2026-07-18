package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/service"
)

func TestParseFlagsDefaultsToSafeDryRun(t *testing.T) {
	opts, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.execute || opts.pageSize != 100 || opts.maxRetries != 2 {
		t.Fatalf("defaults=%+v", opts)
	}
}

func TestParseFlagsRequiresExplicitScopeForExecute(t *testing.T) {
	if _, err := parseFlags([]string{"--execute"}); err == nil {
		t.Fatal("expected execute mode without scope to fail")
	}
	if _, err := parseFlags([]string{"--execute", "--all"}); err != nil {
		t.Fatalf("explicit --all should be accepted: %v", err)
	}
}

func TestCheckpointRoundTripAndScopeSignature(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	state := checkpoint{Signature: "scope", LastChunkID: 12, Processed: 2}
	state.markRunning(time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC))
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatal(err)
	}
	state.LastChunkID = 13
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("replace checkpoint: %v", err)
	}
	loaded, found, err := loadCheckpoint(path)
	if err != nil || !found || loaded.LastChunkID != 13 || loaded.Signature != "scope" {
		t.Fatalf("loaded=%+v found=%v err=%v", loaded, found, err)
	}
	a, err := scopeSignature(checkpointScope{PostgresHost: "127.0.0.1", PostgresPort: 5433, PostgresDB: "vidlens", EmbeddingDim: 3})
	if err != nil {
		t.Fatal(err)
	}
	b, err := scopeSignature(checkpointScope{PostgresHost: "127.0.0.1", PostgresPort: 5433, PostgresDB: "vidlens", EmbeddingDim: 4})
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) != 64 {
		t.Fatalf("scope signatures a=%q b=%q", a, b)
	}
}

func TestPersistCheckpointFailureStoresOnlyStableStage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.FixedZone("local", 8*60*60))
	state := checkpoint{Version: checkpointVersion, Signature: "scope", LastChunkID: 12, Processed: 2}
	state.markRunning(now)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatal(err)
	}

	sensitiveErr := errors.New("provider failed with api_key=do-not-persist")
	err := persistCheckpointFailure(path, &state, "rebuild_vectors", now.Add(time.Minute), sensitiveErr)
	if !errors.Is(err, sensitiveErr) {
		t.Fatalf("persistCheckpointFailure() error = %v, want original operation error", err)
	}

	loaded, found, err := loadCheckpoint(path)
	if err != nil || !found {
		t.Fatalf("loadCheckpoint() loaded=%+v found=%v error=%v", loaded, found, err)
	}
	if loaded.Status != checkpointStatusFailed || loaded.Completed || loaded.FailureStage != "rebuild_vectors" {
		t.Fatalf("failed checkpoint = %+v", loaded)
	}
	if loaded.UpdatedAt.Location() != time.UTC {
		t.Fatalf("updated_at location = %v, want UTC", loaded.UpdatedAt.Location())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "do-not-persist") || strings.Contains(string(raw), "api_key") {
		t.Fatalf("checkpoint leaked provider error: %s", raw)
	}
}

func TestLoadCheckpointUpgradesLegacyV1Lifecycle(t *testing.T) {
	tests := []struct {
		name      string
		completed bool
		want      checkpointStatus
	}{
		{name: "completed", completed: true, want: checkpointStatusCompleted},
		{name: "incomplete", completed: false, want: checkpointStatusRunning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "checkpoint.json")
			raw := []byte(`{"version":1,"signature":"scope","last_chunk_id":12,"processed":2,"completed":` + fmt.Sprint(tt.completed) + `,"updated_at":"2026-07-18T12:00:00+08:00"}`)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}

			loaded, found, err := loadCheckpoint(path)
			if err != nil || !found {
				t.Fatalf("loadCheckpoint() loaded=%+v found=%v error=%v", loaded, found, err)
			}
			if loaded.Version != checkpointVersion || loaded.Status != tt.want || loaded.Completed != tt.completed {
				t.Fatalf("upgraded checkpoint = %+v, want status=%q completed=%v", loaded, tt.want, tt.completed)
			}
			if loaded.UpdatedAt.Location() != time.UTC {
				t.Fatalf("updated_at location = %v, want UTC", loaded.UpdatedAt.Location())
			}
		})
	}
}

func TestLoadCheckpointRejectsInvalidV2Lifecycle(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown status", body: `{"version":2,"signature":"scope","status":"mystery","completed":false,"updated_at":"2026-07-18T04:00:00Z"}`},
		{name: "completed flag drift", body: `{"version":2,"signature":"scope","status":"completed","completed":false,"updated_at":"2026-07-18T04:00:00Z"}`},
		{name: "failed without stage", body: `{"version":2,"signature":"scope","status":"failed","completed":false,"updated_at":"2026-07-18T04:00:00Z"}`},
		{name: "running with failure stage", body: `{"version":2,"signature":"scope","status":"running","failure_stage":"rebuild_vectors","completed":false,"updated_at":"2026-07-18T04:00:00Z"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "checkpoint.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := loadCheckpoint(path); err == nil || !strings.Contains(err.Error(), "invalid checkpoint lifecycle") {
				t.Fatalf("loadCheckpoint() error = %v, want lifecycle validation", err)
			}
		})
	}
}

func TestLoadCheckpointRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	raw := []byte(`{"version":3,"signature":"scope","status":"running","completed":false,"updated_at":"2026-07-18T04:00:00Z"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := loadCheckpoint(path); err == nil || !strings.Contains(err.Error(), "unsupported checkpoint version") {
		t.Fatalf("loadCheckpoint() error = %v, want unsupported version", err)
	}
}

func TestCheckpointLifecycleExecuteMarksCompletedAndAccumulatesProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	state := checkpoint{Signature: "scope", LastChunkID: 12, Processed: 2}
	lifecycle := newCheckpointLifecycle(path, &state, func() time.Time { return now })

	result, err := lifecycle.execute(func(lifecycle *checkpointLifecycle) (service.RAGReindexResult, error) {
		if err := lifecycle.enterStage(checkpointFailureRebuildVectors); err != nil {
			return service.RAGReindexResult{}, err
		}
		if err := lifecycle.saveProgress(13, 1); err != nil {
			return service.RAGReindexResult{}, err
		}
		return service.RAGReindexResult{Candidates: 1, Processed: 1, LastChunkID: 13}, nil
	})
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if result.Processed != 1 || result.LastChunkID != 13 {
		t.Fatalf("execute() result = %+v", result)
	}

	loaded, found, err := loadCheckpoint(path)
	if err != nil || !found {
		t.Fatalf("loadCheckpoint() loaded=%+v found=%v error=%v", loaded, found, err)
	}
	if loaded.Status != checkpointStatusCompleted || !loaded.Completed || loaded.FailureStage != "" {
		t.Fatalf("completed checkpoint = %+v", loaded)
	}
	if loaded.Processed != 3 || loaded.LastChunkID != 13 || loaded.UpdatedAt != now {
		t.Fatalf("completed checkpoint progress = %+v", loaded)
	}
}

func TestCheckpointLifecycleExecutePersistsFailedStageWithoutErrorDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	state := checkpoint{Signature: "scope", LastChunkID: 12, Processed: 2}
	lifecycle := newCheckpointLifecycle(path, &state, func() time.Time { return now })
	sensitiveErr := errors.New("embedding failed with api_key=do-not-persist")

	_, err := lifecycle.execute(func(lifecycle *checkpointLifecycle) (service.RAGReindexResult, error) {
		if err := lifecycle.enterStage(checkpointFailureRebuildVectors); err != nil {
			return service.RAGReindexResult{}, err
		}
		return service.RAGReindexResult{}, sensitiveErr
	})
	if !errors.Is(err, sensitiveErr) {
		t.Fatalf("execute() error = %v, want operation error", err)
	}

	loaded, found, err := loadCheckpoint(path)
	if err != nil || !found {
		t.Fatalf("loadCheckpoint() loaded=%+v found=%v error=%v", loaded, found, err)
	}
	if loaded.Status != checkpointStatusFailed || loaded.Completed || loaded.FailureStage != checkpointFailureRebuildVectors {
		t.Fatalf("failed checkpoint = %+v", loaded)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "do-not-persist") || strings.Contains(string(raw), "api_key") {
		t.Fatalf("checkpoint leaked operation error: %s", raw)
	}
}

func TestCheckpointLifecycleRecordsCompletionPersistenceFailure(t *testing.T) {
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	state := checkpoint{Signature: "scope", LastChunkID: 12, Processed: 2}
	lifecycle := newCheckpointLifecycle("ignored.json", &state, func() time.Time { return now })
	completionErr := errors.New("completion checkpoint unavailable")
	var saved []checkpoint
	lifecycle.save = func(_ string, current checkpoint) error {
		saved = append(saved, current)
		if current.Status == checkpointStatusCompleted {
			return completionErr
		}
		return nil
	}

	_, err := lifecycle.execute(func(_ *checkpointLifecycle) (service.RAGReindexResult, error) {
		return service.RAGReindexResult{Processed: 1, LastChunkID: 13}, nil
	})
	if !errors.Is(err, completionErr) {
		t.Fatalf("execute() error = %v, want completion persistence error", err)
	}
	if len(saved) != 3 {
		t.Fatalf("saved checkpoints = %+v, want running, completed attempt, failed", saved)
	}
	failed := saved[2]
	if failed.Status != checkpointStatusFailed || failed.FailureStage != checkpointFailureComplete || failed.Completed {
		t.Fatalf("failure checkpoint = %+v", failed)
	}
}

func TestSaveCheckpointRejectsInvalidLifecycle(t *testing.T) {
	tests := []struct {
		name  string
		state checkpoint
	}{
		{name: "legacy version", state: checkpoint{Version: legacyCheckpointVersion, Status: checkpointStatusRunning}},
		{name: "completed flag drift", state: checkpoint{Version: checkpointVersion, Status: checkpointStatusCompleted, Completed: false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "checkpoint.json")
			if err := saveCheckpoint(path, tt.state); err == nil || !strings.Contains(err.Error(), "refuse to save invalid checkpoint") {
				t.Fatalf("saveCheckpoint() error = %v, want write-side lifecycle validation", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("checkpoint file exists after rejected save: %v", err)
			}
		})
	}
}
