package investigator

import (
	"context"
	"errors"

	"vidlens/internal/domain"
)

type workflowAnswerDraft struct {
	Kind        string              `json:"kind" jsonschema:"enum=grounded,enum=insufficient_evidence,enum=out_of_scope"`
	Summary     string              `json:"summary"`
	Blocks      []workflowBlock     `json:"blocks"`
	FrameNotes  []workflowFrameNote `json:"frame_notes"`
	Limitations []domain.Limitation `json:"limitations"`
}

func (s *session) runWorkflowAnswer(ctx context.Context) error {
	ids := []string{}
	if s.turn.Context.FrameID != nil {
		asset, _, err := s.engine.Media.Reload(ctx, s.claim, *s.turn.Context.FrameID, "workflow-question")
		if err != nil {
			return err
		}
		if asset.ID != *s.turn.Context.FrameID {
			return domain.Err(422, "WORKFLOW_EVIDENCE_CHANGED", "用户选定图片的字节已不可用，重建图片具有新身份；未把新像素当作原图回答。")
		}
		ids = append(ids, asset.ID)
	}
	s.cp.CitationFrames = ids
	s.cp.Inspections["question-context"] = append([]string{}, ids...)
	if err := s.persist(ctx, s.cp.Messages); err != nil {
		return err
	}
	payload, err := s.workflowEvidence(ctx, 0, s.snapshot.DurationMS, ids)
	if err != nil {
		return err
	}
	payload["question"], payload["question_context"] = s.turn.Question, s.turn.Context
	payload["published_article_navigation_only"] = workflowArticleCandidate(*s.article)
	prior, err := s.engine.Store.PriorTurns(ctx, *s.turn)
	if err != nil {
		return err
	}
	payload["prior_conversation_not_video_evidence"] = prior
	frames, err := s.workflowFrames(ctx, ids)
	if err != nil {
		return err
	}
	written, err := workflowStage[workflowAnswerDraft](ctx, s, "answer", "answer", "answer_video_question", "依据本次问题、原始全片文字及实际交付图片作答。summary简短，blocks给具体解释与就近引用，格式与图文章节相同。图像问题必须从实际像素观察，frame_notes覆盖本轮全部delivered_pixels；无图则[]。published_article与prior_conversation只作导航，不能替代来源。视频未提供依据则明确insufficient_evidence；与当前视频无关则out_of_scope，不编造答案。", payload, ids, func(a *workflowAnswerDraft) error {
		if a.Summary == "" || len(a.Blocks) == 0 || a.Kind != "grounded" && a.Kind != "insufficient_evidence" && a.Kind != "out_of_scope" {
			return errors.New("回答kind、summary或blocks无效")
		}
		return validateWorkflowFrameNotes(a.FrameNotes, frames)
	})
	if err != nil {
		return err
	}
	if err = s.saveWorkflowFrameNotes(ctx, "answer", written.FrameNotes); err != nil {
		return err
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		return err
	}
	section, err := workflowCanonicalSection(workflowSection{ID: "answer", Blocks: written.Blocks}, sectionPlan{ID: "answer", Title: "回答"}, catalog)
	if err != nil {
		return err
	}
	draft := articleDraft{Sections: map[string]domain.Section{"answer": section}, Limitations: written.Limitations}
	workflowDraftCitations(&draft, catalog)
	a := domain.Answer{Kind: written.Kind, Summary: written.Summary, Blocks: section.Blocks, Citations: draft.Citations, Limitations: written.Limitations}
	if err = s.validateAnswer(ctx, &a); err != nil {
		return err
	}
	reviewArticle := workflowAnswerArticle(a)
	units := workflowArticleUnits(reviewArticle, catalog)
	units = append(units, workflowGlobalUnit(reviewArticle))
	issues, err := s.workflowReviewBatch(ctx, reviewArticle, workflowGlobalOutline(reviewArticle), units, catalog, "review")
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		if err = s.workflowSave(ctx, "workflow-answer-findings", issues); err != nil {
			return err
		}
		return domain.Err(422, "WORKFLOW_REVIEW_UNRESOLVED", "回答仍存在未解决的证据问题；原回包和审核意见已保存，本轮未将候选作为正式回答。")
	}
	if err = s.workflowSave(ctx, "candidate", a); err != nil {
		return err
	}
	return s.publish(ctx, domain.JSON(a))
}

func workflowAnswerArticle(a domain.Answer) domain.Article {
	return domain.Article{Title: a.Kind + "：" + a.Summary, Sections: []domain.Section{{ID: "answer", Title: "回答", Blocks: a.Blocks}}, Limitations: a.Limitations, Citations: a.Citations}
}
