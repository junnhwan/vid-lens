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
	Noop    bool            `json:"noop,omitempty"`
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
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := fence(ctx, tx, c); err != nil {
			return err
		}
		op, err := one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2", c.Run.ID, "article-draft:"+key)
		if err == nil {
			if op.ArgsHash != Hash(args) || op.Kind != "article_draft" && op.Kind != "article_draft_noop" {
				return errors.New("draft replay conflict")
			}
			return json.Unmarshal(op.Result, &out)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		currentDraft, err := one[Draft](ctx, tx, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1`, c.Run.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		current := currentDraft.Version
		if current != expected {
			return domain.Err(409, "DRAFT_VERSION_CONFLICT", "草稿版本已更新，请先读取当前版本再修改。")
		}
		unchanged := false
		if current == 0 {
			var empty struct {
				Article *struct {
					Title    string           `json:"title"`
					Sections []domain.Section `json:"sections"`
				} `json:"article"`
			}
			if json.Unmarshal(raw, &empty) == nil && empty.Article != nil && empty.Article.Title == "" && len(empty.Article.Sections) == 0 {
				unchanged = true
			}
		}
		if current > 0 {
			// A batch acknowledgement is not reader-visible draft content. Compare
			// JSON values in the same fenced transaction as version CAS and replay.
			err = tx.QueryRow(ctx, `SELECT
				(CASE WHEN jsonb_typeof($1::jsonb)='object' THEN $1::jsonb-'batch_receipt' ELSE $1::jsonb END) =
				(CASE WHEN jsonb_typeof($2::jsonb)='object' THEN $2::jsonb-'batch_receipt' ELSE $2::jsonb END)`, currentDraft.Data, raw).Scan(&unchanged)
			if err != nil {
				return err
			}
		}
		if current >= 48 && !unchanged {
			return domain.Err(422, "MODEL_RESULT_INVALID", "文章草稿修订已达到上限；已保存章节仍然保留。")
		}
		out = Draft{Version: current + 1, Data: raw}
		kind := "article_draft"
		if unchanged {
			out.Version, out.Noop, kind = current, true, "article_draft_noop"
		}
		_, err = tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,attempt_id,kind,args_hash,status,result) VALUES($1,$2,$3,$4,$5,'completed',$6)`, c.Run.ID, "article-draft:"+key, c.AttemptID, kind, Hash(args), domain.JSON(out))
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

// An explicit retry may reuse real observations before a plan exists, or saved
// draft chapters, under the same immutable source/config/protocol. It never
// carries model submissions or changes the failed run.
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
	if previous.Kind != "article.generate" || !domain.Terminal(previous.Status) || previous.ConfigID != old.ConfigID || previous.SnapshotID == nil || *previous.SnapshotID != snapshot {
		return nil
	}
	if handled, err := reuseArticleLoopDraft(ctx, tx, previous, run, snapshot); handled || err != nil {
		return err
	}
	var cp map[string]any
	if json.Unmarshal(previous.Checkpoint, &cp) != nil || cp["ready"] != true || cp["layout_version"] != "source-context-v3" || (cp["protocol"] != domain.ArticleDraftProtocol && cp["protocol"] != "eino-0.9.21/openai-0.1.13/vidlens-1") {
		return nil
	}
	previousContract, _ := cp["execution_contract"].(map[string]any)
	previousVersion, _ := previousContract["version"].(string)
	workflow := domain.StagedWorkflowContract(previousVersion)
	draft, err := one[Draft](ctx, tx, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1`, previous.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var observations int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM observations WHERE run_id=$1 AND snapshot_id=$2", previous.ID, snapshot).Scan(&observations); err != nil {
		return err
	}
	notes, _ := cp["source_notes"].([]any)
	contract, _ := cp["execution_contract"].(map[string]any)
	reusableNotes := len(notes) > 0 && (contract["version"] == domain.ExecutionContractV4 || contract["version"] == domain.ExecutionContractV5 || contract["version"] == domain.ExecutionContractV6 || contract["version"] == domain.ExecutionContractV7 || contract["version"] == domain.ExecutionContractV8 || contract["version"] == domain.ExecutionContractV9) && cp["source_index_protocol"] == domain.SourceIndexProtocolV1
	if draft.Version == 0 && observations == 0 && !reusableNotes {
		return nil
	}
	// No automatic download/re-materialization during retry admission. If an
	// inspected frame is gone, start fresh rather than fabricate an observation.
	// Termination releases run refs. Lock every delivered and observed frame
	// before checking readiness and pinning, so cleanup cannot win in between.
	var frames []string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(array_agg(DISTINCT frame_id),'{}'::text[]) FROM (
		SELECT f.frame_id FROM jsonb_each(COALESCE($1::jsonb->'inspections','{}'::jsonb)) e CROSS JOIN LATERAL jsonb_array_elements_text(e.value) f(frame_id)
		UNION SELECT jsonb_array_elements_text(COALESCE(NULLIF($1::jsonb->'pending_overview','null'::jsonb),'[]'::jsonb))
		UNION SELECT jsonb_array_elements_text(COALESCE(data->'frame_ids','[]'::jsonb)) FROM observations WHERE run_id=$2
	) wanted`, previous.Checkpoint, previous.ID).Scan(&frames); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT id, state='ready' AND kind='frame' AND snapshot_id=$2 FROM media_assets WHERE id=ANY($1::text[]) ORDER BY id FOR UPDATE", frames, snapshot)
	if err != nil {
		return err
	}
	ready := true
	count := 0
	for rows.Next() {
		var id string
		var valid bool
		if err = rows.Scan(&id, &valid); err != nil {
			rows.Close()
			return err
		}
		count++
		ready = ready && valid
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !ready || count != len(frames) {
		return nil
	}
	cp["messages"] = []any{}
	cp["calls"] = 0
	cp["repeated"] = map[string]int{}
	cp["repairs"] = 0
	delete(cp, "progress")
	delete(cp, "last_candidate")
	delete(cp, "draft_issue")
	delete(cp, "evidence_review_issue")
	delete(cp, "inspect_batch_rejected")
	cp["draft_correction"] = false
	cp["draft_repair_version"] = 0
	cp["finalization_failed"] = false
	delete(cp, "finalization_failure")
	cp["draft_resume"] = true
	delete(cp, "plan_hash")
	// Only artifacts cross the explicit retry boundary. The new run freezes
	// its own adapter, then selects a compatible prompt/tool revision from
	// the unchanged generation spec before its first model request.
	adapter := domain.ExecutionContractV9
	if workflow {
		adapter = domain.ExecutionContractV12
		cp["workflow"] = map[string]any{"phase": "prepare", "calls": map[string]int{}}
	}
	cp["execution_contract"] = domain.ExecutionContract{Version: adapter, RunID: run}
	delete(cp, "harness_revision")
	delete(cp, "layout_version")
	delete(cp, "protocol")
	if _, err = tx.Exec(ctx, "UPDATE job_runs SET checkpoint=$2 WHERE id=$1", run, domain.JSON(cp)); err != nil {
		return err
	}
	if draft.Version > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,kind,args_hash,status,result) VALUES($1,'article-draft:retry','article_draft',$2,'completed',$3)`, run, Hash(map[string]any{"from_run": previous.ID}), domain.JSON(Draft{Version: 1, Data: draft.Data}))
		if err != nil {
			return err
		}
	}
	if workflow {
		// Only source observations and the compatible author plan cross this
		// explicit retry. No model input, call reservation or review verdict is
		// copied; the new execution must review the inherited draft itself.
		_, err = tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,kind,args_hash,status,result) SELECT $2,replace(operation_key,'workflow:','workflow-inherited:'),'workflow_artifact',args_hash,status,result FROM run_operations WHERE run_id=$1 AND kind='workflow_stage' AND status='completed' AND (operation_key='workflow:plan' OR operation_key LIKE 'workflow:investigate:%')`, previous.ID, run)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO observations(id,run_id,snapshot_id,operation_key,inspection_id,data,created_at) SELECT md5($2||id),$2,snapshot_id,operation_key,inspection_id,data,created_at FROM observations WHERE run_id=$1`, previous.ID, run)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO evidence_questions(id,run_id,snapshot_id,data) SELECT $2||':'||(data->>'id'),$2,snapshot_id,data FROM evidence_questions WHERE run_id=$1`, previous.ID, run)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO asset_refs(asset_id,owner_type,owner_id,expires_at) SELECT id,'run',$2,(SELECT deadline_at+interval '5 minutes' FROM job_runs WHERE id=$2) FROM media_assets WHERE id=ANY($1::text[]) ON CONFLICT DO NOTHING`, frames, run)
	return err
}
