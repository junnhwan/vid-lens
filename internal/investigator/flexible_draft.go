package investigator

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

var errDraftReady = errors.New("validated draft ready for publication")

type batchReceipt struct {
	Accepted []string         `json:"accepted_sections"`
	Rejected []map[string]any `json:"rejected_sections"`
}

func (s *session) saveFlexibleBatch(ctx context.Context, in saveSectionsInput, version int, draft articleDraft) (*schema.ToolResult, error) {
	if len(in.Sections) < 1 || len(in.Sections) > 12 || len(domain.JSON(in.Sections)) > 128<<10 {
		return s.draftFeedback(ctx, "每批保存1–12章、最多128KiB；按当前输出预算选择完整批次，已有章节保持。")
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		return nil, err
	}
	receipt := &batchReceipt{Accepted: []string{}, Rejected: []map[string]any{}}
	counts := map[string]int{}
	for _, section := range in.Sections {
		counts[section.ID]++
	}
	for _, section := range in.Sections {
		next, issue := assembleDraftBatch(draft, []domain.Section{section}, catalog)
		if counts[section.ID] > 1 {
			issue = fmt.Errorf("重复章节%s；一次只提交一个修订", section.ID)
		}
		if issue == nil {
			issue = s.validateBlocks(ctx, next.Sections[section.ID].Blocks, next.Citations, true)
		}
		if issue != nil {
			receipt.Rejected = append(receipt.Rejected, map[string]any{"section_id": section.ID, "issue": candidateIssue(issue)})
			continue
		}
		draft = next
		receipt.Accepted = append(receipt.Accepted, section.ID)
	}
	if len(receipt.Accepted) == 0 {
		return s.draftFeedback(ctx, string(domain.JSON(map[string]any{"code": "chapter_validation", "affected_sections": receipt.Rejected, "repairable": true})))
	}
	// Receipt and accepted subset commit in the SAME fenced, idempotent operation.
	// A worker crash never acknowledges a chapter that was not saved.
	draft.BatchReceipt = receipt
	result, err := s.writeDraft(ctx, in, version, draft, fmt.Sprintf("保存%d章：%s", len(receipt.Accepted), draft.Sections[receipt.Accepted[0]].Title))
	if err == nil && len(receipt.Rejected) > 0 {
		s.cp.DraftIssue = map[string]any{"code": "chapter_validation", "affected_sections": receipt.Rejected, "repairable": true}
	}
	return result, err
}

func (s *session) autoFinalize(ctx context.Context) error {
	if s.turn != nil || s.cp.HarnessRevision < 7 {
		return nil
	}
	version, draft, err := s.loadDraft(ctx)
	if err != nil {
		return err
	}
	if version == 0 || len(draft.Plan) == 0 || len(draft.Sections) != len(draft.Plan) || s.cp.DraftCorrection && version <= s.cp.DraftRepairVersion {
		return nil
	}
	// This path uses the very same final validator and durable candidate operation
	// as the explicit submit tool; it does not bypass evidence or publication CAS.
	_, err = s.submitSavedDraft(ctx, submitDraftInput{Version: version})
	if err != nil {
		return err
	}
	return errDraftReady
}
