package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

// Drafts and rejected submissions deliberately use their own operation kinds.
// Only the separately validated operation named "candidate" can be published.
type Draft struct {
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data"`
}

func (s *Store) ArticleDraft(ctx context.Context, run string) (Draft, error) {
	d, err := one[Draft](ctx, s.Pool, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1`, run)
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, nil
	}
	return d, err
}

// Every accepted revision is immutable and fenced, including replays after a
// worker takeover. ExpectedVersion prevents two writers from losing a chapter.
func (s *Store) SaveArticleDraft(ctx context.Context, c domain.Claim, key string, args any, expected int, data any) (Draft, error) {
	var out Draft
	raw := domain.JSON(data)
	if len(raw) > 512<<10 {
		return out, domain.Err(422, "MODEL_RESULT_INVALID", "文章草稿超过保存上限，请减少单个章节的内容。")
	}
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		if err := fence(ctx, tx, c); err != nil {
			return err
		}
		op, err := one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2", c.Run.ID, "article-draft:"+key)
		if err == nil {
			if op.ArgsHash != Hash(args) || op.Kind != "article_draft" {
				return errors.New("draft replay conflict")
			}
			return json.Unmarshal(op.Result, &out)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var current int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max((result->>'version')::int),0) FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed'`, c.Run.ID).Scan(&current); err != nil {
			return err
		}
		if current != expected {
			return domain.Err(409, "DRAFT_VERSION_CONFLICT", "草稿版本已更新，请先读取当前版本再修改。")
		}
		if current >= 48 {
			return domain.Err(422, "MODEL_RESULT_INVALID", "文章草稿修订已达到上限；已保存章节仍然保留。")
		}
		out = Draft{Version: current + 1, Data: raw}
		_, err = tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,attempt_id,kind,args_hash,status,result) VALUES($1,$2,$3,'article_draft',$4,'completed',$5)`, c.Run.ID, "article-draft:"+key, c.AttemptID, Hash(args), domain.JSON(out))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE job_runs SET updated_at=now() WHERE id=$1", c.Run.ID)
		return err
	})
	return out, err
}

func (s *Store) RecordCandidate(ctx context.Context, c domain.Claim, key, name, raw, status string, issue any) error {
	if len(raw) > 512<<10 {
		return domain.Err(422, "MODEL_RESULT_INVALID", "模型提交的候选超过接收上限。")
	}
	args := map[string]any{"candidate_id": key, "arguments_hash": Hash(raw), "tool": name, "arguments": raw, "status": status, "issue": issue}
	operation := "candidate-attempt:" + key + ":" + status
	if _, err := s.BeginOperation(ctx, c, operation, "candidate_attempt", map[string]string{"tool": name, "arguments": raw, "status": status}, false); err != nil {
		return err
	}
	return s.FinishOperation(ctx, c, operation, args, "", nil)
}

// An explicit retry may reuse validated draft chapters and their evidence only
// under the same immutable source/config/protocol. The failed run stays intact.
func reuseArticleDraft(ctx context.Context, tx pgx.Tx, old domain.Request, run string, snapshot string) error {
	if old.SnapshotID == nil || *old.SnapshotID != snapshot {
		return nil
	}
	previousID := old.ActiveRunID
	if previousID == nil && len(old.Segments) > 0 {
		previousID = &old.Segments[len(old.Segments)-1].RunID
	}
	if previousID == nil {
		return nil
	}
	previous, err := loadRun(ctx, tx, *previousID)
	if err != nil {
		return err
	}
	if previous.Kind != "article.generate" || !domain.Terminal(previous.Status) || previous.ConfigID != old.ConfigID {
		return nil
	}
	var cp map[string]any
	if json.Unmarshal(previous.Checkpoint, &cp) != nil || cp["layout_version"] != "source-context-v3" || (cp["protocol"] != domain.ArticleDraftProtocol && cp["protocol"] != "eino-0.9.21/openai-0.1.13/vidlens-1") {
		return nil
	}
	draft, err := one[Draft](ctx, tx, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1`, previous.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// No automatic download/re-materialization during retry admission. If an
	// inspected frame is gone, start fresh rather than fabricate an observation.
	var missing bool
	// terminate releases run refs, so inspect every original observation frame too.
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jsonb_each($1::jsonb->'inspections') e CROSS JOIN LATERAL jsonb_array_elements_text(e.value) f(frame_id) LEFT JOIN media_assets a ON a.id=f.frame_id WHERE a.id IS NULL OR a.state<>'ready' OR a.snapshot_id<>$2)`, previous.Checkpoint, snapshot).Scan(&missing); err != nil {
		return err
	}
	if missing {
		return nil
	}
	cp["messages"] = []any{}
	cp["calls"] = 0
	cp["repeated"] = map[string]int{}
	cp["repairs"] = 0
	delete(cp, "progress")
	delete(cp, "last_candidate")
	delete(cp, "inspect_batch_rejected")
	cp["draft_correction"] = false
	cp["draft_repair_version"] = 0
	cp["finalization_failed"] = false
	delete(cp, "finalization_failure")
	cp["draft_resume"] = true
	if _, err = tx.Exec(ctx, "UPDATE job_runs SET checkpoint=$2 WHERE id=$1", run, domain.JSON(cp)); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,kind,args_hash,status,result) VALUES($1,'article-draft:retry','article_draft',$2,'completed',$3)`, run, Hash(map[string]any{"from_run": previous.ID}), domain.JSON(Draft{Version: 1, Data: draft.Data}))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO observations(id,run_id,snapshot_id,operation_key,inspection_id,data,created_at) SELECT md5($2||id),$2,snapshot_id,operation_key,inspection_id,data,created_at FROM observations WHERE run_id=$1`, previous.ID, run)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO evidence_questions(id,run_id,snapshot_id,data) SELECT $2||':'||(data->>'id'),$2,snapshot_id,data FROM evidence_questions WHERE run_id=$1`, previous.ID, run)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO asset_refs(asset_id,owner_type,owner_id,expires_at) SELECT DISTINCT a.id,'run',$2,(SELECT deadline_at+interval '5 minutes' FROM job_runs WHERE id=$2) FROM jsonb_each($1::jsonb->'inspections') e CROSS JOIN LATERAL jsonb_array_elements_text(e.value) f(frame_id) JOIN media_assets a ON a.id=f.frame_id WHERE a.kind='frame' ON CONFLICT DO NOTHING`, previous.Checkpoint, run)
	return err
}
