package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

func articleLoopGateError() error {
	return domain.Err(422, "ARTICLE_APPROVAL_REQUIRED", "当前候选、原文、配图、稿件版本与完整核验批准不一致；已保存成果保留。")
}

// Called inside Publish's transaction, after locking the run. Neither the
// candidate replay entrance nor a direct Store caller can bypass this check.
func verifyArticleLoopPublication(ctx context.Context, tx pgx.Tx, c domain.Claim, a domain.Article) error {
	var cp struct {
		Contract domain.ExecutionContract `json:"execution_contract"`
		Loop     *struct {
			CandidateHash string `json:"candidate_hash"`
		} `json:"article_loop"`
	}
	if err := tx.QueryRow(ctx, "SELECT checkpoint FROM job_runs WHERE id=$1 FOR UPDATE", c.Run.ID).Scan(&cp); err != nil {
		return err
	}
	if err := fence(ctx, tx, c); err != nil {
		return err
	}
	if cp.Contract.Version != domain.ArticleLoopV1 {
		return nil
	}
	if cp.Contract.RunID != c.Run.ID || cp.Loop == nil || cp.Loop.CandidateHash != domain.ArticleHash(a) {
		return articleLoopGateError()
	}
	h := cp.Loop.CandidateHash
	candidate, err := one[domain.Article](ctx, tx, "SELECT result FROM run_operations WHERE run_id=$1 AND operation_key=$2 AND status='completed'", c.Run.ID, "article-loop:candidate:"+h)
	if err != nil || domain.ArticleHash(candidate) != h {
		return articleLoopGateError()
	}
	approval, err := one[domain.ArticleApproval](ctx, tx, "SELECT result FROM run_operations WHERE run_id=$1 AND operation_key=$2 AND kind='article_loop_approval' AND status='completed'", c.Run.ID, "article-loop:approval:"+h)
	if err != nil || approval.CandidateHash != h || len(approval.ReviewKeys) == 0 {
		return articleLoopGateError()
	}
	draft, err := one[Draft](ctx, tx, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1`, c.Run.ID)
	if err != nil || draft.Version != approval.DraftVersion {
		return articleLoopGateError()
	}
	var data struct {
		Article domain.Article `json:"article"`
	}
	if json.Unmarshal(draft.Data, &data) != nil || domain.ArticleHash(data.Article) != h {
		return articleLoopGateError()
	}
	snap, err := one[domain.Snapshot](ctx, tx, "SELECT to_jsonb(s) FROM source_snapshots s WHERE id=$1 FOR SHARE", a.SnapshotID)
	if err != nil {
		return err
	}
	transcript, err := many[domain.Segment](ctx, tx, "SELECT to_jsonb(t) FROM transcript_segments t WHERE snapshot_id=$1 ORDER BY ordinal FOR SHARE", a.SnapshotID)
	if err != nil {
		return err
	}
	ids := ContentFrameIDs(a.Sections, a.Citations)
	assets, err := many[domain.Asset](ctx, tx, "SELECT to_jsonb(a) FROM media_assets a WHERE id=ANY($1::text[]) ORDER BY id FOR UPDATE", ids)
	if err != nil {
		return err
	}
	if len(assets) != len(ids) {
		return articleLoopGateError()
	}
	for _, asset := range assets {
		if asset.SnapshotID == nil || *asset.SnapshotID != a.SnapshotID || asset.State != "ready" {
			return articleLoopGateError()
		}
	}
	evidence := domain.ArticleEvidenceIdentity(snap, transcript, assets)
	spec, err := one[domain.GenerationSpec](ctx, tx, "SELECT generation_spec FROM generation_requests WHERE active_run_id=$1 ORDER BY created_at LIMIT 1 FOR SHARE", c.Run.ID)
	if err != nil {
		return err
	}
	policy := domain.ArticlePolicyIdentity(spec)
	if approval.EvidenceFingerprint != evidence || approval.PolicyRevision != policy {
		return articleLoopGateError()
	}
	sections, sources, frames := map[string]bool{}, map[string]bool{}, map[string]bool{}
	global := false
	seen := map[string]bool{}
	for _, key := range approval.ReviewKeys {
		if seen[key] {
			return articleLoopGateError()
		}
		seen[key] = true
		record, e := one[domain.ArticleReviewRecord](ctx, tx, "SELECT result FROM run_operations WHERE run_id=$1 AND operation_key=$2 AND kind='article_loop_review' AND status='completed'", c.Run.ID, key)
		if e != nil || record.CandidateHash != h || record.EvidenceFingerprint != evidence || record.PolicyRevision != policy || !record.Report.Complete || len(record.Report.Unassessed) != 0 || len(record.Report.Topics) == 0 {
			return articleLoopGateError()
		}
		for _, issue := range record.Report.Issues {
			if issue.Severity != "advisory" {
				return articleLoopGateError()
			}
		}
		for _, id := range record.Scope.SectionIDs {
			sections[id] = true
		}
		for _, id := range record.Scope.SourceIDs {
			sources[id] = true
		}
		for _, id := range record.Scope.FrameIDs {
			frames[id] = true
		}
		global = global || record.Scope.Global
	}
	if !global {
		return articleLoopGateError()
	}
	for _, section := range a.Sections {
		if !sections[section.ID] {
			return articleLoopGateError()
		}
	}
	for _, segment := range transcript {
		if !sources[segment.ID] {
			return articleLoopGateError()
		}
	}
	for _, id := range ids {
		if !frames[id] {
			return articleLoopGateError()
		}
	}
	return nil
}

// Explicit retries of the new adapter import only a validated immutable draft
// and source-owned assets. A fresh source sweep and every review are required.
func reuseArticleLoopDraft(ctx context.Context, tx pgx.Tx, previous domain.Run, run, snapshot string) (bool, error) {
	var cp struct {
		Contract domain.ExecutionContract `json:"execution_contract"`
	}
	if json.Unmarshal(previous.Checkpoint, &cp) != nil || cp.Contract.Version != domain.ArticleLoopV1 {
		return false, nil
	}
	draft, err := one[Draft](ctx, tx, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1`, previous.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	var data struct {
		Article domain.Article `json:"article"`
	}
	if json.Unmarshal(draft.Data, &data) != nil || data.Article.SnapshotID != snapshot {
		return true, articleLoopGateError()
	}
	frames := ContentFrameIDs(data.Article.Sections, data.Article.Citations)
	assets, err := many[domain.Asset](ctx, tx, "SELECT to_jsonb(a) FROM media_assets a WHERE id=ANY($1::text[]) ORDER BY id FOR UPDATE", frames)
	if err != nil {
		return true, err
	}
	if len(assets) != len(frames) {
		return true, nil
	}
	for _, a := range assets {
		if a.State != "ready" || a.SnapshotID == nil || *a.SnapshotID != snapshot {
			return true, nil
		}
	}
	// The raw canonical draft is safe to import only into the same contract.
	_, err = tx.Exec(ctx, `UPDATE job_runs SET checkpoint=jsonb_build_object('schema_version',1,'execution_contract',$2::jsonb,'article_loop_parent',jsonb_build_object('run_id',$3::text,'draft_version',$4::int)) WHERE id=$1`, run, domain.JSON(domain.ExecutionContract{Version: domain.ArticleLoopV1, RunID: run}), previous.ID, draft.Version)
	if err != nil {
		return true, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,kind,args_hash,status,result) VALUES($1,'article-draft:retry','article_draft',$2,'completed',$3)`, run, Hash(map[string]any{"from_run": previous.ID, "version": draft.Version}), domain.JSON(Draft{Version: 1, Data: draft.Data}))
	if err != nil {
		return true, err
	}
	for _, id := range slices.Compact(frames) {
		if _, err = tx.Exec(ctx, `INSERT INTO asset_refs(asset_id,owner_type,owner_id,expires_at) VALUES($1,'run',$2,(SELECT deadline_at+interval '5 minutes' FROM job_runs WHERE id=$2)) ON CONFLICT DO NOTHING`, id, run); err != nil {
			return true, err
		}
	}
	return true, nil
}
