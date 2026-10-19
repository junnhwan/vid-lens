package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

// An approval covers one exact claim against one immutable evidence scope.
// Changing another prose field does not revoke it. Changing structure, origin,
// citations, transcript, pixels or question does, including on worker replay.
func (s *session) reviewApprovalKeys(candidate any, catalog map[string]domain.Citation, batch evidenceReviewBatch) map[string]string {
	frames := []any{}
	for _, frame := range batch.Frames {
		frames = append(frames, map[string]any{"id": frame.ID, "sha256": frame.SHA256, "time_ms": frame.TimeMS})
	}
	scope := map[string]any{"protocol": evidenceReviewRecoveryProtocol, "mode": batch.Mode, "snapshot_id": s.snapshot.ID, "text_hash": s.snapshot.TextHash, "video_hash": s.snapshot.VideoHash, "source": s.snapshot.Source, "transcript": s.transcript, "frames": frames, "catalog": catalog, "context": reviewCandidateContext(candidate)}
	if s.turn != nil {
		scope["question"] = s.turn.Question
	}
	scopeHash := store.Hash(scope)
	keys := map[string]string{}
	for path, value := range reviewFields(candidate, batch) {
		keys[path] = "evidence-approval:" + store.Hash(map[string]any{"scope": scopeHash, "path": path, "value": value, "meaning": reviewSemanticScope(candidate, path)})
	}
	return keys
}

// A clause can rely on a heading or condition in its own chapter. Recheck that
// chapter after an edit, preserving independent chapters. Global prose depends
// on the complete candidate.
func reviewSemanticScope(candidate any, path string) any {
	var root map[string]any
	_ = json.Unmarshal(domain.JSON(candidate), &root)
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) >= 3 && (parts[0] == "sections" || parts[0] == "blocks") {
		items, _ := root[parts[0]].([]any)
		index, err := strconv.Atoi(parts[1])
		if err == nil && index >= 0 && index < len(items) {
			return map[string]any{"title": root["title"], "summary": root["summary"], "limitations": root["limitations"], "unit": items[index]}
		}
	}
	return root
}

func (s *session) unreviewedFields(ctx context.Context, candidate any, batch evidenceReviewBatch, keys map[string]string) (map[string]any, error) {
	fields := reviewFields(candidate, batch)
	for path, key := range keys {
		op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if op.Status != "completed" {
			continue
		}
		var proof struct {
			Supported bool `json:"supported"`
		}
		if json.Unmarshal(op.Result, &proof) != nil || !proof.Supported {
			return nil, fmt.Errorf("invalid durable evidence approval at %s", path)
		}
		delete(fields, path)
	}
	return fields, nil
}

func (s *session) rememberReviewApprovals(ctx context.Context, reviewKey string, keys map[string]string, result evidenceReview) error {
	// A full issue list may have omitted additional problems. Such a batch has
	// no implicit approvals; every field must be checked again after correction.
	if len(result.Issues) >= 8 {
		return nil
	}
	rejected := map[string]bool{}
	for _, issue := range result.Issues {
		rejected[issue.Path] = true
	}
	for path, key := range keys {
		if rejected[path] {
			continue
		}
		op, err := s.engine.Store.BeginOperation(ctx, s.claim, key, "review_approval", map[string]string{"field_path": path}, false)
		if err != nil {
			return err
		}
		if op.Status != "completed" {
			if err = s.engine.Store.FinishOperation(ctx, s.claim, key, map[string]any{"supported": true, "field_path": path, "review_operation_key": reviewKey}, "", nil); err != nil {
				return err
			}
		}
	}
	return nil
}
