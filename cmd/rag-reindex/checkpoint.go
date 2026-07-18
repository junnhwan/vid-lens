package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vid-lens/internal/service"
)

const (
	legacyCheckpointVersion = 1
	checkpointVersion       = 2
)

type checkpointStatus string

const (
	checkpointStatusRunning   checkpointStatus = "running"
	checkpointStatusCompleted checkpointStatus = "completed"
	checkpointStatusFailed    checkpointStatus = "failed"
)

type checkpointFailureStage string

const (
	checkpointFailureInitializeCodec checkpointFailureStage = "initialize_api_key_codec"
	checkpointFailureConnectPGVector checkpointFailureStage = "connect_pgvector"
	checkpointFailureRebuildVectors  checkpointFailureStage = "rebuild_vectors"
	checkpointFailureComplete        checkpointFailureStage = "complete_checkpoint"
)

// checkpoint persists resumable progress. Status is canonical in v2;
// Completed remains only for v1 compatibility and must agree with Status.
type checkpoint struct {
	Version      int                    `json:"version"`
	Signature    string                 `json:"signature"`
	LastChunkID  int64                  `json:"last_chunk_id"`
	Processed    int64                  `json:"processed"`
	Status       checkpointStatus       `json:"status"`
	FailureStage checkpointFailureStage `json:"failure_stage,omitempty"`
	Completed    bool                   `json:"completed"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

func (c *checkpoint) markRunning(now time.Time) {
	c.Version = checkpointVersion
	c.Status = checkpointStatusRunning
	c.FailureStage = ""
	c.Completed = false
	c.UpdatedAt = now.UTC()
}

func (c *checkpoint) markFailed(stage checkpointFailureStage, now time.Time) {
	c.Version = checkpointVersion
	c.Status = checkpointStatusFailed
	c.FailureStage = stage
	c.Completed = false
	c.UpdatedAt = now.UTC()
}

func (c *checkpoint) markCompleted(now time.Time) {
	c.Version = checkpointVersion
	c.Status = checkpointStatusCompleted
	c.FailureStage = ""
	c.Completed = true
	c.UpdatedAt = now.UTC()
}

func validCheckpointFailureStage(stage checkpointFailureStage) bool {
	switch stage {
	case checkpointFailureInitializeCodec, checkpointFailureConnectPGVector, checkpointFailureRebuildVectors, checkpointFailureComplete:
		return true
	default:
		return false
	}
}

func validateCheckpointLifecycle(state checkpoint) error {
	switch state.Status {
	case checkpointStatusRunning:
		if state.Completed || state.FailureStage != "" {
			return errors.New("running checkpoint must be incomplete and have no failure stage")
		}
	case checkpointStatusCompleted:
		if !state.Completed || state.FailureStage != "" {
			return errors.New("completed checkpoint must be complete and have no failure stage")
		}
	case checkpointStatusFailed:
		if state.Completed || !validCheckpointFailureStage(state.FailureStage) {
			return errors.New("failed checkpoint must be incomplete and have a valid failure stage")
		}
	default:
		return fmt.Errorf("unknown checkpoint status %q", state.Status)
	}
	return nil
}

type checkpointSaver func(path string, state checkpoint) error

// persistCheckpointFailure records only a stable stage enum. The operation
// error is returned to stderr but is deliberately excluded from the durable
// checkpoint because provider and driver errors may contain secrets.
func persistCheckpointFailure(path string, state *checkpoint, stage checkpointFailureStage, now time.Time, operationErr error) error {
	return persistCheckpointFailureWithSaver(path, state, stage, now, operationErr, saveCheckpoint)
}

func persistCheckpointFailureWithSaver(path string, state *checkpoint, stage checkpointFailureStage, now time.Time, operationErr error, save checkpointSaver) error {
	if operationErr == nil {
		return errors.New("persist checkpoint failure: operation error is required")
	}
	if state == nil {
		return errors.Join(operationErr, errors.New("persist checkpoint failure: state is required"))
	}
	if !validCheckpointFailureStage(stage) {
		return errors.Join(operationErr, errors.New("persist checkpoint failure: invalid failure stage"))
	}
	if save == nil {
		return errors.Join(operationErr, errors.New("persist checkpoint failure: saver is required"))
	}
	state.markFailed(stage, now)
	if err := save(path, *state); err != nil {
		return errors.Join(operationErr, fmt.Errorf("persist failed checkpoint: %w", err))
	}
	return operationErr
}

// checkpointLifecycle is the single owner of execute-mode state transitions.
// Callers select a stable failure stage and report per-chunk progress; execute
// guarantees running -> completed or running -> failed finalization.
type checkpointLifecycle struct {
	path          string
	state         *checkpoint
	now           func() time.Time
	save          checkpointSaver
	baseProcessed int64
	failureStage  checkpointFailureStage
}

func newCheckpointLifecycle(path string, state *checkpoint, now func() time.Time) *checkpointLifecycle {
	if now == nil {
		now = time.Now
	}
	return &checkpointLifecycle{path: path, state: state, now: now, save: saveCheckpoint}
}

func (l *checkpointLifecycle) execute(operation func(*checkpointLifecycle) (service.RAGReindexResult, error)) (result service.RAGReindexResult, returnErr error) {
	if l == nil || l.state == nil {
		return result, errors.New("checkpoint lifecycle state is required")
	}
	if operation == nil {
		return result, errors.New("checkpoint lifecycle operation is required")
	}

	l.baseProcessed = l.state.Processed
	l.failureStage = checkpointFailureInitializeCodec
	l.state.markRunning(l.now())
	if l.save == nil {
		return result, errors.New("checkpoint lifecycle saver is required")
	}
	if err := l.save(l.path, *l.state); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			returnErr = persistCheckpointFailureWithSaver(l.path, l.state, l.failureStage, l.now(), returnErr, l.save)
		}
	}()

	result, returnErr = operation(l)
	if returnErr != nil {
		return result, returnErr
	}

	l.failureStage = checkpointFailureComplete
	l.state.LastChunkID = result.LastChunkID
	l.state.Processed = l.baseProcessed + result.Processed
	l.state.markCompleted(l.now())
	if returnErr = l.save(l.path, *l.state); returnErr != nil {
		return result, returnErr
	}
	return result, nil
}

func (l *checkpointLifecycle) enterStage(stage checkpointFailureStage) error {
	if l == nil {
		return errors.New("checkpoint lifecycle is required")
	}
	if !validCheckpointFailureStage(stage) {
		return fmt.Errorf("invalid checkpoint failure stage %q", stage)
	}
	l.failureStage = stage
	return nil
}

func (l *checkpointLifecycle) saveProgress(lastChunkID, processed int64) error {
	if l == nil || l.state == nil {
		return errors.New("checkpoint lifecycle state is required")
	}
	if lastChunkID <= l.state.LastChunkID {
		return fmt.Errorf("checkpoint chunk id %d must advance beyond %d", lastChunkID, l.state.LastChunkID)
	}
	if processed <= 0 {
		return fmt.Errorf("checkpoint processed count %d must be positive", processed)
	}
	l.state.LastChunkID = lastChunkID
	l.state.Processed = l.baseProcessed + processed
	l.state.markRunning(l.now())
	return l.save(l.path, *l.state)
}

type checkpointScope struct {
	UserID         int64  `json:"user_id"`
	TaskID         int64  `json:"task_id"`
	EmbeddingModel string `json:"embedding_model"`
	PostgresHost   string `json:"postgres_host"`
	PostgresPort   int    `json:"postgres_port"`
	PostgresDB     string `json:"postgres_db"`
	PostgresTable  string `json:"postgres_table"`
	EmbeddingDim   int    `json:"embedding_dim"`
}

func scopeSignature(scope checkpointScope) (string, error) {
	scope.EmbeddingModel = strings.TrimSpace(scope.EmbeddingModel)
	scope.PostgresHost = strings.TrimSpace(scope.PostgresHost)
	scope.PostgresDB = strings.TrimSpace(scope.PostgresDB)
	scope.PostgresTable = strings.TrimSpace(scope.PostgresTable)
	raw, err := json.Marshal(scope)
	if err != nil {
		return "", fmt.Errorf("marshal checkpoint scope: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func loadCheckpoint(path string) (checkpoint, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return checkpoint{}, false, nil
	}
	if err != nil {
		return checkpoint{}, false, fmt.Errorf("read checkpoint: %w", err)
	}
	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		return checkpoint{}, false, fmt.Errorf("parse checkpoint: %w", err)
	}
	switch state.Version {
	case legacyCheckpointVersion:
		state.Version = checkpointVersion
		if state.Completed {
			state.Status = checkpointStatusCompleted
		} else {
			state.Status = checkpointStatusRunning
		}
		state.FailureStage = ""
	case checkpointVersion:
	default:
		return checkpoint{}, false, fmt.Errorf("unsupported checkpoint version %d", state.Version)
	}
	if !state.UpdatedAt.IsZero() {
		state.UpdatedAt = state.UpdatedAt.UTC()
	}
	if err := validateCheckpointLifecycle(state); err != nil {
		return checkpoint{}, false, fmt.Errorf("invalid checkpoint lifecycle: %w", err)
	}
	return state, true, nil
}

func saveCheckpoint(path string, state checkpoint) error {
	if state.Version != checkpointVersion {
		return fmt.Errorf("refuse to save invalid checkpoint: unsupported version %d", state.Version)
	}
	if err := validateCheckpointLifecycle(state); err != nil {
		return fmt.Errorf("refuse to save invalid checkpoint: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create checkpoint directory: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".rag-reindex-*.tmp")
	if err != nil {
		return fmt.Errorf("create checkpoint temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure checkpoint temp file: %w", err)
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write checkpoint temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync checkpoint temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close checkpoint temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace checkpoint: %w", err)
	}
	return nil
}
