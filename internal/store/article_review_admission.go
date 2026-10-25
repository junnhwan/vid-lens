package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

type referenceFrozenInput struct {
	Messages []struct {
		Content string `json:"content"`
		Parts   []struct {
			Type  string `json:"type"`
			Extra struct {
				FrameID string `json:"frame_id"`
			} `json:"extra"`
		} `json:"user_input_multi_content"`
	} `json:"messages"`
	ToolNames []string `json:"tool_names"`
}

func referenceModelArguments(ctx context.Context, tx pgx.Tx, run, key, tool string) (string, error) {
	op, err := one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2 AND kind='model' AND status='completed'", run, key)
	if err != nil {
		return "", err
	}
	var response struct {
		ToolCalls []struct {
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if json.Unmarshal(op.Result, &response) != nil || len(response.ToolCalls) != 1 || response.ToolCalls[0].Function.Name != tool {
		return "", articleLoopGateError()
	}
	if domain.ValidateArticleReferenceJSON([]byte(response.ToolCalls[0].Function.Arguments)) != nil {
		return "", articleLoopGateError()
	}
	var observed bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM model_submissions WHERE run_id=$1 AND operation_key=$2 AND submission=$3 AND reporting->>'stream_status'='complete')", run, key, op.Submissions).Scan(&observed); err != nil || !observed {
		return "", articleLoopGateError()
	}
	return response.ToolCalls[0].Function.Arguments, nil
}

func referenceInputPayload(ctx context.Context, tx pgx.Tx, run, key string) (Operation, map[string]json.RawMessage, error) {
	op, err := one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2 AND kind='model_input' AND status='completed'", run, key)
	payload := map[string]json.RawMessage{}
	var input referenceFrozenInput
	if err != nil {
		return op, payload, err
	}
	if json.Unmarshal(op.Result, &input) != nil {
		return op, payload, articleLoopGateError()
	}
	for _, m := range input.Messages {
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(m.Content), &fields) == nil {
			for k, v := range fields {
				if _, duplicate := payload[k]; duplicate {
					return op, payload, articleLoopGateError()
				}
				payload[k] = v
			}
		}
	}
	return op, payload, nil
}

// Check the actual durable model messages and result, not a model claim or the
// checkpoint's assertion that it saw the complete article. No credentials are
// involved in these journal inputs/results.
func verifyReferenceReview(ctx context.Context, tx pgx.Tx, run string, record domain.ArticleReviewRecord, a domain.Article, snap domain.Snapshot, text []domain.Segment) error {
	if err := domain.ValidateArticleReviewAdmission(record, a, snap.ID, text); err != nil {
		return fmt.Errorf("%w: reference admission: %v", articleLoopGateError(), err)
	}
	r := record.Admission
	if r.RunID != run {
		return articleLoopGateError()
	}
	op, payload, err := referenceInputPayload(ctx, tx, run, r.InputKey)
	if err != nil || domain.ContentHash(op.Result) != r.InputHash {
		return fmt.Errorf("%w: frozen input identity", articleLoopGateError())
	}
	equal := func(key string, value any) bool {
		return payload[key] != nil && domain.ArticleJSONHash(payload[key]) == domain.ArticleJSONHash(value)
	}
	if !equal("snapshot_id", snap.ID) || !equal("draft_version", r.DraftVersion) || !equal("evidence_directory", r.Directory) {
		return fmt.Errorf("%w: frozen snapshot/version/directory", articleLoopGateError())
	}
	var frozen referenceFrozenInput
	if json.Unmarshal(op.Result, &frozen) != nil || !slices.Equal(frozen.ToolNames, []string{"report_article_review"}) {
		return articleLoopGateError()
	}
	frames := []string{}
	for _, m := range frozen.Messages {
		for _, part := range m.Parts {
			if part.Type == "image_url" {
				frames = append(frames, part.Extra.FrameID)
			}
		}
	}
	if !slices.Equal(frames, record.Scope.FrameIDs) {
		return articleLoopGateError()
	}
	type pixel struct {
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	}
	var submitted struct {
		Pixels []pixel `json:"pixel_inputs"`
	}
	if len(frames) > 0 {
		if err = tx.QueryRow(ctx, `SELECT reporting FROM model_submissions WHERE run_id=$1 AND operation_key=$2 AND submission=(SELECT submissions FROM run_operations WHERE run_id=$1 AND operation_key=$2)`, run, r.ModelKey).Scan(&submitted); err != nil {
			return articleLoopGateError()
		}
		expected := []pixel{}
		for _, id := range frames {
			asset, e := one[domain.Asset](ctx, tx, "SELECT to_jsonb(a) FROM media_assets a WHERE id=$1", id)
			if e != nil {
				return e
			}
			expected = append(expected, pixel{asset.Bytes, asset.SHA256})
		}
		if !slices.Equal(submitted.Pixels, expected) {
			return fmt.Errorf("%w: actual submitted pixels", articleLoopGateError())
		}
	}
	primary, contextRefs := []string{}, []string{}
	for i, t := range text {
		ref := fmt.Sprint("t", i+1)
		if slices.Contains(record.Scope.SourceIDs, t.ID) {
			primary = append(primary, ref)
		}
		if slices.Contains(record.Scope.ContextSourceIDs, t.ID) {
			contextRefs = append(contextRefs, ref)
		}
	}
	wireScope := record.Scope
	wireScope.SourceIDs = []string{}
	for _, e := range r.Directory {
		wireScope.SourceIDs = append(wireScope.SourceIDs, e.Ref)
	}
	wireScope.ContextSourceIDs = contextRefs
	role := "local"
	if record.Scope.Global {
		role = "full_current_article"
	}
	if !equal("primary_source_ids", primary) || !equal("context_source_ids", contextRefs) || !equal("primary_sections", record.Scope.SectionIDs) || !equal("material_role", role) || !equal("scope", wireScope) {
		return fmt.Errorf("%w: frozen primary/context responsibility", articleLoopGateError())
	}
	aliases := []string{}
	for _, entry := range r.Directory {
		aliases = append(aliases, entry.Ref)
	}
	originals := []map[string]any{}
	for i, t := range text {
		ref := fmt.Sprint("t", i+1)
		if slices.Contains(aliases, ref) {
			originals = append(originals, map[string]any{"alias": ref, "original": map[string]any{"start_ms": t.StartMS, "end_ms": t.EndMS, "text": t.Text, "origin": t.Origin}})
		}
	}
	if !equal("originals", originals) {
		return fmt.Errorf("%w: delivered originals", articleLoopGateError())
	}
	view := domain.ArticleReviewProjection(a, record.Scope)
	if record.Scope.Global {
		view = a
		if !equal("full_view_manifest", domain.ArticleFullViewManifest(a)) || !equal("pending_coverage_questions", r.Questions) {
			return fmt.Errorf("%w: full body manifest or pending questions", articleLoopGateError())
		}
	}
	if !equal("candidate", domain.ReadingArticle(view)) || !equal("citation_bindings", domain.ArticleReadingBindings(view, text)) || !equal("context_blocks", domain.ArticleReviewContextBlocks(a, record.Scope.ContextBlockIDs)) {
		return fmt.Errorf("%w: delivered reading/citation/context view", articleLoopGateError())
	}
	args, err := referenceModelArguments(ctx, tx, run, r.ModelKey, "report_article_review")
	if err != nil {
		return articleLoopGateError()
	}
	if args != r.OriginalToolArguments {
		return articleLoopGateError()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(args), &fields) != nil {
		return articleLoopGateError()
	}
	delete(fields, "activity_title")
	parsed, parseErr := domain.DecodeArticleReferenceReport(domain.JSON(fields))
	if parseErr != nil || domain.ArticleJSONHash(fields) != domain.ArticleJSONHash(json.RawMessage(r.Original)) || domain.ContentHash(parsed) != domain.ContentHash(r.OriginalReport) {
		return articleLoopGateError()
	}
	if r.Ticket != nil {
		var stage struct {
			Admission domain.ArticleReviewAdmission `json:"admission"`
			Scope     domain.ArticleReviewScope     `json:"scope"`
		}
		op, err := one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2 AND kind='article_reference_stage' AND status='completed'", run, "article-loop:reference-stage:"+r.Ticket.ID)
		if err != nil || json.Unmarshal(op.Result, &stage) != nil || domain.ContentHash(stage.Admission.Ticket) != domain.ContentHash(r.Ticket) || domain.ContentHash(stage.Admission.OriginalReport) != domain.ContentHash(r.OriginalReport) || domain.ContentHash(stage.Scope) != domain.ContentHash(record.Scope) {
			return articleLoopGateError()
		}
		_, repairInput, err := referenceInputPayload(ctx, tx, run, r.RepairInputKey)
		if err != nil || domain.ArticleJSONHash(repairInput["ticket"]) != domain.ArticleJSONHash(r.Ticket) || domain.ArticleJSONHash(repairInput["evidence_directory"]) != domain.ArticleJSONHash(r.Directory) {
			return articleLoopGateError()
		}
		args, err = referenceModelArguments(ctx, tx, run, r.RepairModelKey, "repair_review_references")
		var patchFields map[string]json.RawMessage
		_ = json.Unmarshal([]byte(args), &patchFields)
		delete(patchFields, "activity_title")
		if err != nil || domain.ArticleJSONHash(patchFields) != domain.ArticleJSONHash(r.Repair) {
			return articleLoopGateError()
		}
	}
	return nil
}
