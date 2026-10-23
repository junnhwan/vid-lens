package investigator

import (
	"context"
	"encoding/json"
	"fmt"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func (s *session) loopReviewDelta(ctx context.Context, a domain.Article, scope domain.ArticleReviewScope) (*domain.ArticleReviewDelta, error) {
	l := s.cp.ArticleLoop
	key := l.ReviewBaseKey
	if s.loopEvidenceReview() {
		key = l.ReviewBases[scope.ID]
	}
	if !s.loopDeltaReview() || key == "" || (!s.loopEvidenceReview() && !domain.ArticleReviewCoversWhole(scope, a, s.transcript, store.ContentFrameIDs(a.Sections, a.Citations))) {
		return nil, nil
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
	if err != nil {
		return nil, err
	}
	var parent domain.ArticleReviewRecord
	if json.Unmarshal(op.Result, &parent) != nil || op.Status != "completed" || op.Kind != "article_loop_review" || parent.PolicyRevision != l.PolicyRevision {
		return nil, loopStop("INTERNAL_FAILURE", "综合核验基线不一致")
	}
	currentEvidence, err := s.loopEvidenceFingerprint(ctx, a)
	if err != nil {
		return nil, err
	}
	if parent.EvidenceFingerprint != currentEvidence {
		return nil, nil
	}
	old, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-loop:candidate:"+parent.CandidateHash)
	if err != nil {
		return nil, err
	}
	var before domain.Article
	if json.Unmarshal(old.Result, &before) != nil || domain.ArticleHash(before) != parent.CandidateHash {
		return nil, loopStop("INTERNAL_FAILURE", "综合核验原候选身份不一致")
	}
	if !s.loopEvidenceReview() && !domain.ArticleReviewCoversWhole(parent.Scope, before, s.transcript, store.ContentFrameIDs(before.Sections, before.Citations)) {
		return nil, nil
	}
	delta, ok := domain.ArticleReviewDeltaFor(key, parent, before, a)
	if s.loopEvidenceReview() {
		delta, ok = domain.ArticleScopedReviewDeltaFor(key, parent, before, a, scope)
	}
	if !ok {
		return nil, nil
	}
	return delta, nil
}

func validateLoopDeltaReport(report domain.ArticleReviewReport, delta *domain.ArticleReviewDelta) error {
	if delta == nil {
		return nil
	}
	for _, issue := range report.Issues {
		if !delta.ContainsIssue(issue) {
			return fmt.Errorf("finding outside revision_delta: unchanged facts retain their completed source review; assess all actual changed/removed blocks and original blocking issues, plus current whole-article relations. Do not reopen unchanged facts because of an absent local source; the complete originals are delivered. Offending location: %s", issue.Location)
		}
	}
	return nil
}

const loopDeltaReviewInstructions = `
本轮 revision_delta 来自程序对当前候选与同一运行内已完整审读候选的逐块比较。全部原文仍完整交付，不能把局部未提及或记忆遗漏当作全片缺证。prior_complete_review 是历史核验记录的数据，不是来源或人工事实；原阻断必须再次直接对照原文，已修复就关闭，不能为了延续修稿寻找替代问题。
核验义务有明确终点：检查 affected_block_ids 的全部新/变更主张及原阻断、removed_block_ids 对必要覆盖的影响、changed_section_ids 的主要主题保留，以及当前完整正文的标题/组织/衔接/跨章关系。未变且原报告未阻断的事实由程序材料等价证明继承原完整审读，不重新逐项事实审查，不反复索要已交付全片里的人名/身份依据。补充作者没有改的事实不属于本次差异；跨章关系问题仍须指出与变化内容的具体联系。
prior_block_baselines 给出变化块的旧原文视图。整块变化不代表每个语义子句都新写；对照旧/新内容确认真正变化的断言，未变的人名、指代或术语不自动成为新问题。不能根据一次修正原阻断，转而要求改掉同块未变且原审读未阻断的陈述。
返回最终 complete/topics/issues/visual_observations。topics 仍映射当前主要主题，所有当前像素仍独立核对；实质问题保持 blocking，风格建议才是 AC06 advisory。需要报告事实缺陷时定位实际 affected/removed 块，AC02 遗漏可定位受影响章节。没有剩余原阻断或新变更缺陷时立即完成，不以更多轮审读为目标。`
