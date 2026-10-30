package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

// These receipts are created by the server, never parsed from model output.
// Bind the delivery decision to both the final content and current source bytes.
type questionDeliveryReceipt struct {
	AnswerHash     string `json:"answer_hash"`
	SourceIdentity string `json:"source_identity"`
	Review         string `json:"review"`
}

func questionDeliveryHash(a domain.Answer) string {
	a.Frames = nil // URLs and retention may be refreshed at the read boundary.
	return store.Hash(a)
}

func (s *session) requireQuestionDelivery(ctx context.Context, a domain.Answer) error {
	if a.Evidence == nil || !slices.Contains([]string{"passed", "partial", "unavailable"}, a.Evidence.Review) {
		return publicationReviewIncomplete()
	}
	hash := questionDeliveryHash(a)
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "question-delivery:"+hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return publicationReviewIncomplete()
	}
	if err != nil {
		return err
	}
	identity, _, _, _, err := s.contentReviewIdentity(ctx, s.answerReviewCandidate(a), a.Citations)
	if err != nil {
		return err
	}
	var receipt questionDeliveryReceipt
	if op.Status != "completed" || op.Kind != "question_delivery" || json.Unmarshal(op.Result, &receipt) != nil || receipt.AnswerHash != hash || receipt.SourceIdentity != identity || receipt.Review != a.Evidence.Review || op.ArgsHash != store.Hash(receipt) {
		return publicationReviewIncomplete()
	}
	return nil
}

func recoverableQuestionReview(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var p *domain.Problem
	codes := []string{"PROVIDER_UNAVAILABLE", "PROVIDER_RATE_LIMITED", "MODEL_UNAVAILABLE", "MODEL_RESULT_INVALID", "MODEL_CONTEXT_EXCEEDED", "MODEL_CALL_BUDGET_EXHAUSTED", "MODEL_RESPONSE_BUDGET_EXHAUSTED", "REVIEW_COVERAGE_INCOMPLETE", "REVIEW_INPUT_TOO_LARGE", "REVIEW_BUDGET_EXHAUSTED", "MODEL_RESPONSE_TIMEOUT"}
	var work *domain.WorkError
	return errors.As(err, &p) && slices.Contains(codes, p.Code) || errors.As(err, &work) && slices.Contains(codes, work.Code)
}

type questionUnitVerdict struct {
	Supported bool
	Rejected  bool
	Reasons   []string
}

// Replay the durable review reports, including complete batches before a failed
// call. A missing batch remains unassessed; it can never acquire approval.
func (s *session) questionVerdicts(ctx context.Context, hash string, candidate any, catalog map[string]domain.Citation) ([]reviewUnit, map[string]questionUnitVerdict, error) {
	units, err := candidateReviewUnits(candidate, catalog)
	if err != nil {
		return nil, nil, err
	}
	tasks, err := s.planReviewTasks(units)
	if err != nil {
		return units, map[string]questionUnitVerdict{}, nil
	}
	verdicts := map[string]questionUnitVerdict{}
	for _, u := range units {
		verdicts[u.ID] = questionUnitVerdict{Supported: true}
	}
	for _, task := range tasks {
		results, readErr := s.questionTaskReports(ctx, hash, task, catalog)
		if readErr != nil {
			return nil, nil, readErr
		}
		for _, unit := range task.Units {
			id := unit.ID
			v := verdicts[id]
			r, ok := results[id]
			v.Supported = v.Supported && ok && r.Verdict == "supported" && len(r.Issues) == 0
			v.Rejected = v.Rejected || r.Verdict == "contradicted" || r.Verdict == "unverifiable"
			for _, issue := range r.Issues {
				v.Reasons = append(v.Reasons, issue.Reason)
			}
			verdicts[id] = v
		}
	}
	return units, verdicts, nil
}

func (s *session) questionTaskReports(ctx context.Context, hash string, task reviewTask, catalog map[string]domain.Citation) (map[string]reviewUnitResult, error) {
	results := map[string]reviewUnitResult{}
	ids := []string{}
	for _, u := range task.Units {
		ids = append(ids, u.ID)
	}
	key := "review-units:" + store.Hash(map[string]any{"candidate_hash": hash, "mode": task.Mode, "unit_ids": ids})
	id := "r_" + strings.TrimPrefix(key, "review-units:")[:24]
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return results, nil
	}
	if err != nil {
		return nil, err
	}
	var saved reviewBatchOutcome
	args := map[string]any{"request_id": id, "unit_ids": ids, "mode": task.Mode, "protocol": s.boundedReviewProtocol()}
	if op.Status != "completed" || op.Kind != "review_batch" || op.ArgsHash != store.Hash(args) || json.Unmarshal(op.Result, &saved) != nil || saved.RequestID != id || saved.Mode != task.Mode || !slices.Equal(saved.UnitIDs, ids) {
		return results, nil
	}
	if saved.State == "split" && len(task.Units) > 1 {
		middle := len(task.Units) / 2
		for _, units := range [][]reviewUnit{task.Units[:middle], task.Units[middle:]} {
			child, err := s.questionTaskReports(ctx, hash, reviewTask{Mode: task.Mode, Units: units}, catalog)
			if err != nil {
				return nil, err
			}
			for id, result := range child {
				results[id] = result
			}
		}
		return results, nil
	}
	if saved.State != "complete" || saved.Report == nil || validateReviewUnits(*saved.Report, id, task, catalog) != nil {
		return results, nil
	}
	for _, r := range saved.Report.Results {
		results[r.UnitID] = r
	}
	return results, nil
}

func questionBlockText(b domain.Block) string {
	parts := []string{b.Caption, b.Text}
	for _, in := range b.Content {
		parts = append(parts, in.Text)
	}
	for _, item := range b.Items {
		for _, in := range item.Content {
			parts = append(parts, in.Text)
		}
	}
	for _, row := range b.Rows {
		parts = append(parts, row.Cells...)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func questionLimitedAnswer(a domain.Answer, status, reason string, units []reviewUnit, verdicts map[string]questionUnitVerdict) domain.Answer {
	// Copy slices and provenance so a displayed preview cannot mutate a later
	// candidate or its independent review identity.
	out := a
	evidence := *a.Evidence
	out.Evidence = &evidence
	out.Evidence.Review, out.Evidence.ReviewReason = status, reason
	out.Evidence.VerifiedBlocks = []string{}
	out.Evidence.Uncertainties = []domain.AnswerUncertainty{}
	byPath := map[string]questionUnitVerdict{}
	for _, u := range units {
		v := verdicts[u.ID]
		byPath[u.Path] = v
		if v.Supported {
			continue
		}
		// A missing reviewer result qualifies the whole reference answer. Do
		// not repeat every unreviewed paragraph again as a separate defect.
		if status != "partial" && !v.Rejected && len(v.Reasons) == 0 {
			continue
		}
		claim := ""
		if u.Path == "/summary" {
			claim = a.Summary
		}
		for i, b := range a.Blocks {
			if u.Path == fmt.Sprintf("/blocks/%d", i) {
				claim = questionBlockText(b)
			}
		}
		if claim == "" {
			continue
		}
		why := strings.Join(v.Reasons, "；")
		if why == "" {
			why = "此项尚未完成独立核验，不能作为已核验结论。"
		}
		out.Evidence.Uncertainties = append(out.Evidence.Uncertainties, domain.AnswerUncertainty{Claim: claim, Reason: why, CitationIDs: u.CitationAliases})
	}
	if status == "unavailable" && len(out.Evidence.Uncertainties) == 0 {
		out.Evidence.Uncertainties = append(out.Evidence.Uncertainties, domain.AnswerUncertainty{Claim: "这份参考解读尚未完成独立核验。", Reason: "当前只能提供有来源的参考内容，不能把没有核验结论当作通过。"})
	}
	out.Kind = "limited"
	out.Parts = nil
	out.Blocks = []domain.Block{}
	for i, b := range a.Blocks {
		v := byPath[fmt.Sprintf("/blocks/%d", i)]
		if v.Rejected || status == "partial" && !v.Supported {
			continue
		}
		if status != "partial" && len(blockCitationIDs(b)) == 0 {
			continue
		}
		if status != "partial" && len(out.Blocks) >= 3 {
			continue
		}
		out.Blocks = append(out.Blocks, b)
		if v.Supported {
			out.Evidence.VerifiedBlocks = append(out.Evidence.VerifiedBlocks, b.ID)
		}
	}
	if status == "partial" {
		out.Summary = "已核验的内容如下；其余说法列在未确认项中。"
	} else if byPath["/summary"].Rejected {
		out.Summary = "参考解读（独立核验未完成）：以下仅依据已读取的局部来源，仍待确认。"
	} else {
		out.Summary = "参考解读（独立核验未完成）：" + a.Summary
	}
	if len(out.Blocks) == 0 {
		out.Summary = "目前没有可以单独确认的结论，已有依据和未确认项供你继续核对。"
		out.Blocks = []domain.Block{{ID: "verification-status", Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "text", Text: "请查看下方未确认项与回答依据；本轮没有把未完成的核验标记为通过。"}}}}
	}
	return out
}

func (s *session) prepareQuestionDelivery(ctx context.Context, a domain.Answer) (domain.Answer, error) {
	candidate := s.answerReviewCandidate(a)
	hash, catalog, _, hints, err := s.contentReviewIdentity(ctx, candidate, a.Citations)
	if err != nil {
		return a, err
	}
	if err = s.answerEvidence(ctx, &a); err != nil {
		return a, err
	}
	preview := questionLimitedAnswer(a, "pending", "已有原文或画面依据，独立核验正在进行；此处先供参考。", nil, nil)
	if err = s.engine.Store.SaveQuestionPreview(ctx, s.claim, preview); err != nil {
		return a, err
	}
	_, reviewErr := s.reviewUnits(ctx, hash, candidate, hints, catalog)
	if ctx.Err() != nil {
		return a, ctx.Err()
	}
	if !recoverableQuestionReview(reviewErr) {
		return a, reviewErr
	}
	units, verdicts, err := s.questionVerdicts(ctx, hash, candidate, catalog)
	if err != nil {
		return a, err
	}
	allSupported, anySupported := len(units) > 0, false
	for _, u := range units {
		allSupported = allSupported && verdicts[u.ID].Supported
		if strings.HasPrefix(u.Path, "/blocks/") && verdicts[u.ID].Supported {
			anySupported = true
		}
	}
	if allSupported && reviewErr == nil {
		a.Evidence.Review = "passed"
		a.Evidence.ReviewReason = "独立核验已完成。"
		for _, b := range a.Blocks {
			a.Evidence.VerifiedBlocks = append(a.Evidence.VerifiedBlocks, b.ID)
		}
	} else if anySupported {
		a = questionLimitedAnswer(a, "partial", "仅保留独立核验支持的正文；未确认或有冲突的说法已单列。", units, verdicts)
	} else {
		a = questionLimitedAnswer(a, "unavailable", "独立核验未返回完整可用结论；以下为有来源的有限参考解读，尚未通过独立核验。", units, verdicts)
	}
	if err = s.validateAnswer(ctx, &a); err != nil {
		return a, err
	}
	identity, _, _, _, err := s.contentReviewIdentity(ctx, s.answerReviewCandidate(a), a.Citations)
	if err != nil {
		return a, err
	}
	receipt := questionDeliveryReceipt{AnswerHash: questionDeliveryHash(a), SourceIdentity: identity, Review: a.Evidence.Review}
	key := "question-delivery:" + receipt.AnswerHash
	if _, err = s.engine.Store.BeginOperation(ctx, s.claim, key, "question_delivery", receipt, false); err != nil {
		return a, err
	}
	if err = s.engine.Store.FinishOperation(ctx, s.claim, key, receipt, "", nil); err != nil {
		return a, err
	}
	return a, nil
}
