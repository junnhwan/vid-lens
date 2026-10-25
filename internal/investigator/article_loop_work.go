package investigator

import (
	"context"
	"slices"
	"unicode/utf8"

	"vidlens/internal/domain"
)

func (s *session) loopWorkPlannedReview() bool {
	return s.cp.ArticleLoop != nil && domain.ArticleWorkPlannedReview(s.cp.ArticleLoop.ReviewRevision)
}

// Complete originals required by these decisions, including adjacent speech.
// The whole candidate is always delivered separately, never clipped to a batch.
func (s *session) loopCoverageSources(scope domain.ArticleReviewScope) []string {
	refs := []string{}
	for _, q := range s.loopReferenceQuestions(scope) {
		for _, ref := range q.EvidenceRefs {
			refs = appendUnique(refs, ref)
		}
	}
	refs = append(refs, domain.ArticleSourceBoundaryRefs(s.transcript, refs)...)
	ids := []string{}
	for _, entry := range domain.ArticleEvidenceDirectory(s.snapshot.ID, s.transcript, refs) {
		ids = append(ids, entry.SegmentID)
	}
	return ids
}

func (s *session) loopCoverageWork(scope domain.ArticleReviewScope) int {
	work := 0
	for _, q := range s.loopReferenceQuestions(scope) {
		if s.loopSemanticReviewWork() {
			work += utf8.RuneCountInString(q.Topic)
		} else {
			work += utf8.RuneCount(domain.JSON(q))
		}
	}
	ids := append(s.loopCoverageSources(scope), scope.ContextSourceIDs...)
	for _, original := range s.transcript {
		if slices.Contains(ids, original.ID) {
			if s.loopSemanticReviewWork() {
				work += utf8.RuneCountInString(original.Text)
			} else {
				work += utf8.RuneCount(domain.JSON(original))
			}
		}
	}
	return work
}

// Preserve established question batches, including their unresolved findings.
// Newly discovered obligations acquire their own tasks before final relations.
// No candidate approval or missing/covered decision is inherited here.
func (s *session) planLoopCoverageWork(a domain.Article, scopes []domain.ArticleReviewScope) ([]domain.ArticleReviewScope, error) {
	out := []domain.ArticleReviewScope{}
	assigned := map[string]bool{}
	sections := []string{}
	for _, section := range a.Sections {
		sections = append(sections, section.ID)
	}
	var final domain.ArticleReviewScope
	for _, scope := range scopes {
		if !scope.Global {
			out = append(out, scope)
			continue
		}
		if scope.CoverageBatch && len(scope.CoverageQuestionIDs) > 0 {
			for _, id := range scope.CoverageQuestionIDs {
				assigned[id] = true
			}
			if scope.FullRelations {
				final = scope
			} else {
				out = append(out, scope)
			}
		} else if scope.CoverageBatch {
			final = scope
		}
	}
	seal := func(scope domain.ArticleReviewScope) domain.ArticleReviewScope {
		scope.ID = domain.ContentHash(scope)[:24]
		scope.ContextSourceIDs = s.loopCoverageSources(scope)
		return scope
	}
	batch := domain.ArticleReviewScope{Global: true, SectionIDs: sections, CoverageBatch: true}
	flush := func() {
		out = append(out, seal(batch))
		batch = domain.ArticleReviewScope{Global: true, SectionIDs: sections, CoverageBatch: true}
	}
	for _, q := range s.cp.ArticleLoop.CoverageQuestions {
		if assigned[q.ID] {
			continue
		}
		next := batch
		next.CoverageQuestionIDs = append(slices.Clone(batch.CoverageQuestionIDs), q.ID)
		if len(batch.CoverageQuestionIDs) > 0 && (len(next.CoverageQuestionIDs) > domain.ArticleCoverageDecisionUnits || s.loopCoverageWork(next) > domain.ArticleCoverageWorkRunes) {
			flush()
			next = batch
			next.CoverageQuestionIDs = []string{q.ID}
		}
		if s.loopCoverageWork(next) > domain.ArticleCoverageWorkRunes {
			return nil, loopStop("ARTICLE_CAPACITY_EXCEEDED", "单项覆盖义务及其完整必要原文超过工作上限，不能省去条件或截断全文")
		}
		batch = next
	}
	if len(batch.CoverageQuestionIDs) > 0 {
		flush()
	}
	if final.ID == "" && len(out) > 0 && out[len(out)-1].Global {
		final = out[len(out)-1]
		out = out[:len(out)-1]
		final.ID = ""
		final.ContextSourceIDs = nil
		final.FullRelations = true
		final = seal(final)
	} else if final.ID == "" {
		final = seal(domain.ArticleReviewScope{Global: true, SectionIDs: sections, CoverageBatch: true, FullRelations: true})
	}
	return append(out, final), nil
}

func (s *session) loopRepairReviewReserve(scopes []domain.ArticleReviewScope) int {
	// One repair plus finish (atomic finish is cheaper), one changed local,
	// and a fresh decision on every complete-current-body task.
	reserve := 3
	if s.loopExecutableAuthorBudget() {
		// Both supported write actions can atomically finish the candidate.
		reserve = 2
	}
	for _, scope := range scopes {
		if scope.Global {
			reserve++
		}
	}
	return reserve
}

func (s *session) loopExecutableAuthorBudget() bool {
	return s.cp.ArticleLoop != nil && (s.cp.ArticleLoop.ReviewRevision == domain.ArticleConvergenceReviewV12 || s.loopSemanticReviewWork())
}

func (s *session) loopSemanticReviewWork() bool {
	return s.cp.ArticleLoop != nil && s.cp.ArticleLoop.ReviewRevision == domain.ArticleConvergenceReviewV13
}

func (s *session) refreshLoopCoverageWork(ctx context.Context, a domain.Article) error {
	l := s.cp.ArticleLoop
	if !s.loopWorkPlannedReview() || slices.ContainsFunc(l.Scopes, func(sc domain.ArticleReviewScope) bool { return !sc.Global }) {
		return nil
	}
	// The first complete-body task follows all locals. Later coverage batches
	// cannot invent local questions; keep every already admitted batch intact.
	known := map[string]bool{}
	for _, sc := range l.ReviewPlan {
		for _, id := range sc.CoverageQuestionIDs {
			known[id] = true
		}
	}
	newQuestions := []domain.ArticleCoverageQuestion{}
	for _, q := range l.CoverageQuestions {
		if !known[q.ID] {
			newQuestions = append(newQuestions, q)
		}
	}
	if len(newQuestions) == 0 {
		return nil
	}
	plan, err := s.planLoopCoverageWork(a, l.ReviewPlan)
	if err != nil {
		return err
	}
	for _, scope := range plan {
		if !scope.Global || scope.FullRelations || len(scope.CoverageQuestionIDs) == 0 || slices.ContainsFunc(l.ReviewPlan, func(old domain.ArticleReviewScope) bool { return old.ID == scope.ID }) {
			continue
		}
		// Insert new obligations before the existing mandatory relations task.
		at := slices.IndexFunc(l.Scopes, func(sc domain.ArticleReviewScope) bool { return sc.Global && sc.FullRelations })
		if at < 0 {
			return loopStop("INTERNAL_FAILURE", "新增覆盖义务时全文关系责任丢失")
		}
		l.Scopes = slices.Insert(l.Scopes, at, scope)
	}
	l.ReviewPlan = plan
	if len(l.Scopes) > l.Budget.Calls-s.cp.Calls {
		return loopStop("ARTICLE_BUDGET_EXHAUSTED", "新增覆盖义务无法在剩余预算内完整核验，不能丢弃后继续")
	}
	return nil
}
