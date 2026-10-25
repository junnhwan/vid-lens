package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

// Source obligations acquire authority only from their exact source-only input
// and original completed model reply. Recheck inside the publish transaction.
func verifyArticleSourcePlans(ctx context.Context, tx pgx.Tx, run string, snap domain.Snapshot, text []domain.Segment, policy, revision string, keys []string) ([]domain.ArticleCoverageQuestion, error) {
	ops, err := many[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND kind='article_source_plan' AND status='completed'", run)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 || len(ops) != len(keys) {
		return nil, articleLoopGateError()
	}
	seen := map[string]bool{}
	questions := []domain.ArticleCoverageQuestion{}
	for _, op := range ops {
		var p domain.ArticleSourcePlan
		if !slices.Contains(keys, op.Key) || json.Unmarshal(op.Result, &p) != nil || p.RunID != run || p.SnapshotID != snap.ID || p.PolicyRevision != policy {
			return nil, articleLoopGateError()
		}
		if op.Key != "article-loop:source-plan:"+domain.ContentHash([]any{run, policy, p.Directory}) || domain.ValidateArticleSourceRequirementsForRevision(p.Report, p.PrimaryRefs, p.Directory, revision) != nil {
			return nil, articleLoopGateError()
		}
		if domain.ContentHash(p.Directory) != domain.ContentHash(domain.ArticleEvidenceDirectory(snap.ID, text, p.PrimaryRefs)) {
			return nil, articleLoopGateError()
		}
		originals := []map[string]any{}
		for i, t := range text {
			ref := fmt.Sprint("t", i+1)
			if slices.Contains(p.PrimaryRefs, ref) {
				if seen[ref] {
					return nil, articleLoopGateError()
				}
				seen[ref] = true
				originals = append(originals, map[string]any{"alias": ref, "original": map[string]any{"start_ms": t.StartMS, "end_ms": t.EndMS, "text": t.Text, "origin": t.Origin}})
			}
		}
		input, payload, err := referenceInputPayload(ctx, tx, run, p.InputKey)
		var frozen referenceFrozenInput
		if err != nil || domain.ContentHash(input.Result) != p.InputHash || json.Unmarshal(input.Result, &frozen) != nil || !slices.Equal(frozen.ToolNames, []string{"plan_source_requirements"}) {
			return nil, articleLoopGateError()
		}
		for _, key := range []string{"candidate", "current_article", "navigation", "prior_complete_review", "review_findings", "draft_version"} {
			if payload[key] != nil {
				return nil, articleLoopGateError()
			}
		}
		for _, m := range frozen.Messages {
			if len(m.Parts) > 0 {
				return nil, articleLoopGateError()
			}
		}
		equal := func(key string, value any) bool {
			return payload[key] != nil && domain.ArticleJSONHash(payload[key]) == domain.ArticleJSONHash(value)
		}
		if !equal("snapshot_id", snap.ID) || !equal("material_role", "source_requirements") || !equal("primary_source_ids", p.PrimaryRefs) || !equal("evidence_directory", p.Directory) || !equal("originals", originals) {
			return nil, articleLoopGateError()
		}
		if revision == domain.ArticleConvergenceReviewV9 || revision == domain.ArticleConvergenceReviewV10 || domain.ArticleWorkPlannedReview(revision) {
			if !slices.Equal(domain.ArticleSourceWorkBatch(text, p.PrimaryRefs), p.PrimaryRefs) || !equal("source_work_limit_runes", domain.ArticleSourceWorkRunes) {
				return nil, articleLoopGateError()
			}
			refs := domain.ArticleSourceBoundaryRefs(text, p.PrimaryRefs)
			contextOriginals := []map[string]any{}
			for i, t := range text {
				ref := fmt.Sprint("t", i+1)
				if slices.Contains(refs, ref) {
					contextOriginals = append(contextOriginals, map[string]any{"alias": ref, "original": map[string]any{"start_ms": t.StartMS, "end_ms": t.EndMS, "text": t.Text, "origin": t.Origin}})
				}
			}
			if !equal("context_source_ids", refs) || !equal("context_originals", contextOriginals) {
				return nil, articleLoopGateError()
			}
		}
		if domain.ArticleWorkPlannedReview(revision) && !equal("major_concept_limit", domain.ArticleSourceRequirementUnits) {
			return nil, articleLoopGateError()
		}
		args, err := referenceModelArguments(ctx, tx, run, p.ModelKey, "plan_source_requirements")
		if err != nil || args != p.OriginalArguments {
			return nil, articleLoopGateError()
		}
		fields := map[string]json.RawMessage{}
		if json.Unmarshal([]byte(args), &fields) != nil {
			return nil, articleLoopGateError()
		}
		delete(fields, "activity_title")
		if domain.ArticleJSONHash(fields) != domain.ArticleJSONHash(p.Report) {
			return nil, articleLoopGateError()
		}
		questions = append(questions, p.Questions(op.Key)...)
	}
	if len(seen) != len(text) {
		return nil, articleLoopGateError()
	}
	return questions, nil
}
