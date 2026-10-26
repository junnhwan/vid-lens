package store

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

// Advice is separate from publication. Delivery still binds the exact saved
// candidate to its frozen sources and the actual pixels delivered to the author.
func verifyDeliveryPublication(ctx context.Context, tx pgx.Tx, c domain.Claim, a domain.Article, version int, evidence, policy string, seen []string, revision string) error {
	if revision == domain.ArticleDeliveryV7 && a.MindMap == nil {
		return articleLoopGateError()
	}
	draft, err := one[Draft](ctx, tx, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1`, c.Run.ID)
	var data struct {
		Article domain.Article `json:"article"`
		Notes   map[string]struct {
			VisibleFacts []string `json:"visible_facts"`
		} `json:"notes"`
	}
	if err != nil || draft.Version != version || json.Unmarshal(draft.Data, &data) != nil || domain.ArticleHash(data.Article) != domain.ArticleHash(a) {
		return articleLoopGateError()
	}
	snap, err := one[domain.Snapshot](ctx, tx, "SELECT to_jsonb(s) FROM source_snapshots s WHERE id=$1 FOR SHARE", a.SnapshotID)
	if err != nil {
		return err
	}
	text, err := many[domain.Segment](ctx, tx, "SELECT to_jsonb(t) FROM transcript_segments t WHERE snapshot_id=$1 ORDER BY ordinal FOR SHARE", a.SnapshotID)
	if err != nil {
		return err
	}
	ids := ContentFrameIDs(a.Sections, a.Citations)
	assets, err := many[domain.Asset](ctx, tx, "SELECT to_jsonb(a) FROM media_assets a WHERE id=ANY($1::text[]) ORDER BY id FOR UPDATE", ids)
	if err != nil {
		return err
	}
	if len(assets) != len(ids) || Hash(a.Source) != Hash(snap.Source) {
		return articleLoopGateError()
	}
	for _, asset := range assets {
		if asset.SnapshotID == nil || *asset.SnapshotID != a.SnapshotID || asset.State != "ready" || !slices.Contains(seen, asset.ID) || len(data.Notes[asset.ID].VisibleFacts) == 0 {
			return articleLoopGateError()
		}
	}
	spec, err := one[domain.GenerationSpec](ctx, tx, "SELECT generation_spec FROM generation_requests WHERE active_run_id=$1 ORDER BY created_at LIMIT 1 FOR SHARE", c.Run.ID)
	if err != nil {
		return err
	}
	if evidence != domain.ArticleEvidenceIdentity(snap, text, assets) || policy != domain.ArticleReviewPolicyIdentity(spec, domain.ArticleLoopV2, revision) {
		return articleLoopGateError()
	}
	return nil
}
