package investigator

import (
	"context"
	"encoding/json"
	"slices"
	"unicode/utf8"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

const loopReviewWorkRunes = 16000
const loopRepairRoundLimit = 3
const loopRepairActionLimit = 3
const loopReviewProtocolLimit = 2

func (s *session) loopConvergence() bool {
	return s.cp.ArticleLoop != nil && domain.ArticleConvergenceReview(s.cp.ArticleLoop.ReviewRevision)
}

func (s *session) loopCompactReview() bool {
	return s.cp.ArticleLoop != nil && (s.cp.ArticleLoop.ReviewRevision == domain.ArticleConvergenceReviewV2 || s.loopDeltaReview())
}

func (s *session) loopDeltaReview() bool {
	return s.cp.ArticleLoop != nil && (s.cp.ArticleLoop.ReviewRevision == domain.ArticleConvergenceReviewV3 || s.loopEvidenceReview())
}

func loopBlocking(issues []domain.ArticleReviewIssue) bool {
	return slices.ContainsFunc(issues, func(issue domain.ArticleReviewIssue) bool { return issue.Severity == "blocking" })
}

func (s *session) loopScopeContext(a domain.Article, scope domain.ArticleReviewScope) domain.ArticleReviewScope {
	if s.loopEvidenceReview() && len(scope.FrameIDs) == 0 {
		scope.FrameIDs = nil
	}
	if scope.Global && len(scope.SourceIDs) == 0 {
		return scope
	}
	scope.ContextSourceIDs = nil
	scope = loopScopeEvidence(scope, a)
	selected := append(slices.Clone(scope.SourceIDs), scope.ContextSourceIDs...)
	contextIDs := slices.Clone(scope.ContextSourceIDs)
	for i, original := range s.transcript {
		if !slices.Contains(selected, original.ID) {
			continue
		}
		for _, j := range []int{i - 1, i + 1} {
			if j >= 0 && j < len(s.transcript) && !slices.Contains(scope.SourceIDs, s.transcript[j].ID) {
				contextIDs = appendUnique(contextIDs, s.transcript[j].ID)
			}
		}
	}
	scope.ContextSourceIDs = nil
	if s.loopEvidenceReview() && !s.cp.ArticleLoop.LimitedReviewContext {
		// Full originals are context, not extra coverage work. Capacity still checked before sending.
		for _, original := range s.transcript {
			if !slices.Contains(scope.SourceIDs, original.ID) {
				contextIDs = appendUnique(contextIDs, original.ID)
			}
		}
	}
	for _, original := range s.transcript {
		if slices.Contains(contextIDs, original.ID) {
			scope.ContextSourceIDs = append(scope.ContextSourceIDs, original.ID)
		}
	}
	return scope
}

// 新修订优先综合核验，容量不足时拆分；修稿后按完整章节复用未变材料。
func (s *session) convergentLoopScopes(a domain.Article) []domain.ArticleReviewScope {
	if s.loopEvidenceReview() {
		if scopes := s.retainedLoopScopes(a); len(scopes) > 0 {
			return scopes
		}
	}
	return s.buildLoopScopes(a, s.loopCompactReview() && !s.loopDeltaReview() && s.cp.ArticleLoop.RepairRounds > 0)
}

func (s *session) buildLoopScopes(a domain.Article, units bool) []domain.ArticleReviewScope {
	limit := loopReviewWorkRunes
	if s.cp.ArticleLoop.ReviewWorkLimit > 0 {
		limit = s.cp.ArticleLoop.ReviewWorkLimit
	}
	whole := domain.ArticleReviewScope{Global: true, FrameIDs: store.ContentFrameIDs(a.Sections, a.Citations)}
	for _, section := range a.Sections {
		whole.SectionIDs = append(whole.SectionIDs, section.ID)
	}
	for _, original := range s.transcript {
		whole.SourceIDs = append(whole.SourceIDs, original.ID)
	}
	seal := func(scope domain.ArticleReviewScope) domain.ArticleReviewScope {
		scope = s.loopScopeContext(a, scope)
		scope.ID = ""
		scope.ID = store.Hash(scope)[:24]
		return scope
	}
	if len(a.Sections) == 0 || !units && (s.loopCompactReview() && s.cp.ArticleLoop.ReviewWorkLimit == 0 || s.loopScopeWork(a, whole) <= limit) {
		return []domain.ArticleReviewScope{seal(whole)}
	}
	chapters := make([]domain.ArticleReviewScope, len(a.Sections))
	owners := map[string][]int{}
	for i, section := range a.Sections {
		scope := loopScopeEvidence(domain.ArticleReviewScope{SectionIDs: []string{section.ID}}, a)
		scope.SourceIDs, scope.ContextSourceIDs = scope.ContextSourceIDs, nil
		for _, id := range scope.SourceIDs {
			owners[id] = append(owners[id], i)
		}
		scope.FrameIDs = store.ContentFrameIDs([]domain.Section{section}, a.Citations)
		chapters[i] = scope
	}
	// 未引用原文仍是覆盖义务，分配给时间上最近的章节；没有来源的章也必须审。
	for index, original := range s.transcript {
		if len(owners[original.ID]) > 0 {
			continue
		}
		owner := 0
		best := len(s.transcript) + 1
		for j, candidate := range s.transcript {
			if len(owners[candidate.ID]) == 0 {
				continue
			}
			distance := index - j
			if distance < 0 {
				distance = -distance
			}
			if distance < best {
				best, owner = distance, owners[candidate.ID][0]
			}
		}
		chapters[owner].SourceIDs = appendUnique(chapters[owner].SourceIDs, original.ID)
	}
	canonical := func(scope domain.ArticleReviewScope) domain.ArticleReviewScope {
		ids := scope.SourceIDs
		scope.SourceIDs = nil
		for _, original := range s.transcript {
			if slices.Contains(ids, original.ID) {
				scope.SourceIDs = append(scope.SourceIDs, original.ID)
			}
		}
		return scope
	}
	join := func(left, right domain.ArticleReviewScope) domain.ArticleReviewScope {
		left.SectionIDs = append(slices.Clone(left.SectionIDs), right.SectionIDs...)
		left.SourceIDs = slices.Clone(left.SourceIDs)
		left.FrameIDs = slices.Clone(left.FrameIDs)
		for _, id := range right.SourceIDs {
			left.SourceIDs = appendUnique(left.SourceIDs, id)
		}
		for _, id := range right.FrameIDs {
			left.FrameIDs = appendUnique(left.FrameIDs, id)
		}
		return canonical(left)
	}
	scopes := []domain.ArticleReviewScope{}
	current := domain.ArticleReviewScope{}
	for _, chapter := range chapters {
		next := join(current, chapter)
		if len(current.SectionIDs) > 0 && (units || s.loopScopeWork(a, next) > limit || len(next.FrameIDs) > 4) {
			scopes = append(scopes, seal(current))
			current = canonical(chapter)
		} else {
			current = next
		}
	}
	if len(current.SectionIDs) > 0 {
		scopes = append(scopes, seal(current))
	}
	global := domain.ArticleReviewScope{Global: true, SectionIDs: whole.SectionIDs}
	return append(scopes, seal(global))
}

func (s *session) loopChapterHalves(a domain.Article, parent domain.ArticleReviewScope) (domain.ArticleReviewScope, domain.ArticleReviewScope) {
	mid := len(parent.SectionIDs) / 2
	left := domain.ArticleReviewScope{SectionIDs: slices.Clone(parent.SectionIDs[:mid])}
	right := domain.ArticleReviewScope{SectionIDs: slices.Clone(parent.SectionIDs[mid:])}
	left = loopScopeEvidence(left, a)
	right = loopScopeEvidence(right, a)
	leftRefs, rightRefs := left.ContextSourceIDs, right.ContextSourceIDs
	left.ContextSourceIDs, right.ContextSourceIDs = nil, nil
	for i, original := range s.transcript {
		if !slices.Contains(parent.SourceIDs, original.ID) {
			continue
		}
		inLeft, inRight := slices.Contains(leftRefs, original.ID), slices.Contains(rightRefs, original.ID)
		if !inLeft && !inRight {
			best := len(s.transcript) + 1
			for j, neighbor := range s.transcript {
				distance := i - j
				if distance < 0 {
					distance = -distance
				}
				if distance < best && (slices.Contains(leftRefs, neighbor.ID) || slices.Contains(rightRefs, neighbor.ID)) {
					best = distance
					inLeft, inRight = slices.Contains(leftRefs, neighbor.ID), slices.Contains(rightRefs, neighbor.ID)
				}
			}
			if !inLeft && !inRight {
				inLeft = true
			}
		}
		if inLeft {
			left.SourceIDs = append(left.SourceIDs, original.ID)
		}
		if inRight {
			right.SourceIDs = append(right.SourceIDs, original.ID)
		}
	}
	for _, section := range a.Sections {
		if slices.Contains(left.SectionIDs, section.ID) {
			for _, id := range store.ContentFrameIDs([]domain.Section{section}, a.Citations) {
				left.FrameIDs = appendUnique(left.FrameIDs, id)
			}
		}
		if slices.Contains(right.SectionIDs, section.ID) {
			for _, id := range store.ContentFrameIDs([]domain.Section{section}, a.Citations) {
				right.FrameIDs = appendUnique(right.FrameIDs, id)
			}
		}
	}
	for _, id := range parent.FrameIDs {
		if !slices.Contains(left.FrameIDs, id) && !slices.Contains(right.FrameIDs, id) {
			left.FrameIDs = append(left.FrameIDs, id)
		}
	}
	return loopScopeEvidence(left, a), loopScopeEvidence(right, a)
}

func (s *session) loopScopeWork(a domain.Article, scope domain.ArticleReviewScope) int {
	if s.loopConvergence() {
		scope = s.loopScopeContext(a, scope)
	}
	projected := a
	projected.Sections = nil
	for _, section := range a.Sections {
		if slices.Contains(scope.SectionIDs, section.ID) {
			projected.Sections = append(projected.Sections, section)
		}
	}
	work := utf8.RuneCount(domain.JSON(domain.ReadingArticle(projected)))
	for _, original := range s.transcript {
		if slices.Contains(scope.SourceIDs, original.ID) || slices.Contains(scope.ContextSourceIDs, original.ID) {
			work += utf8.RuneCount(domain.JSON(original))
		}
	}
	return work
}

func (s *session) loopMaterialHash(ctx context.Context, a domain.Article, scope domain.ArticleReviewScope) (string, error) {
	assets := []domain.Asset{}
	for _, id := range scope.FrameIDs {
		asset, err := s.engine.Store.InternalFrame(ctx, s.claim, id)
		if err != nil {
			return "", err
		}
		assets = append(assets, asset)
	}
	return domain.ArticleReviewMaterialIdentity(a, scope, s.snapshot, s.transcript, assets), nil
}

func loopReviewKey(candidate, evidence, scope string) string {
	return "article-loop:review:" + store.Hash(map[string]any{"candidate": candidate, "evidence": evidence, "scope": scope})
}

func (s *session) loopReviewProtocolKey(scope string) string {
	if s.loopCompactReview() {
		return "review_protocol:" + s.cp.ArticleLoop.CandidateHash + ":" + scope
	}
	return "review_protocol:" + scope
}

func (s *session) loopPendingReviewCount(ctx context.Context, a domain.Article, scopes []domain.ArticleReviewScope) (int, error) {
	count := 0
	for _, scope := range scopes {
		if scope.Global {
			count++
			continue
		}
		hash, err := s.loopMaterialHash(ctx, a, scope)
		if err != nil {
			return 0, err
		}
		key, err := s.loopReusableReview(ctx, a, scope, hash)
		if err != nil {
			return 0, err
		}
		if key == "" {
			count++
		}
	}
	return count, nil
}

func (s *session) loopReusableReview(ctx context.Context, a domain.Article, scope domain.ArticleReviewScope, material string) (string, error) {
	l := s.cp.ArticleLoop
	if key := l.ReviewCache[material]; key != "" {
		return key, nil
	}
	if !s.loopCompactReview() || scope.Global {
		return "", nil
	}
	keys := []string{}
	for _, key := range l.ReviewCache {
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
		if err != nil {
			return "", err
		}
		var record domain.ArticleReviewRecord
		if json.Unmarshal(op.Result, &record) != nil || record.ReusedFrom != "" || record.Delta != nil || record.PolicyRevision != l.PolicyRevision || !record.Report.Complete || len(record.Report.Unassessed) > 0 || !domain.ArticleReviewScopeContains(record.Scope, scope) {
			continue
		}
		parent, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-loop:candidate:"+record.CandidateHash)
		if err != nil {
			return "", err
		}
		var prior domain.Article
		if json.Unmarshal(parent.Result, &prior) != nil || domain.ArticleHash(prior) != record.CandidateHash {
			continue
		}
		if _, ok := domain.ArticleReviewSubset(record.Report, prior, scope, s.transcript); !ok {
			continue
		}
		hash, err := s.loopMaterialHash(ctx, prior, scope)
		if err != nil {
			return "", err
		}
		if hash == material {
			return key, nil
		}
	}
	return "", nil
}

// 只从本运行的原始完整核验复用逐字相同的局部材料；重新绑定候选，不继承全文批准。
func (s *session) reuseLoopReviews(ctx context.Context, a domain.Article) error {
	l := s.cp.ArticleLoop
	pending := []domain.ArticleReviewScope{}
	for _, scope := range l.Scopes {
		if scope.Global {
			pending = append(pending, scope)
			continue
		}
		material, err := s.loopMaterialHash(ctx, a, scope)
		if err != nil {
			return err
		}
		previous, err := s.loopReusableReview(ctx, a, scope, material)
		if err != nil {
			return err
		}
		if previous == "" {
			pending = append(pending, scope)
			continue
		}
		op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, previous)
		if err != nil {
			return err
		}
		var record domain.ArticleReviewRecord
		if json.Unmarshal(op.Result, &record) != nil || op.Kind != "article_loop_review" || op.Status != "completed" || record.ReusedFrom != "" || record.PolicyRevision != l.PolicyRevision || !record.Report.Complete || len(record.Report.Unassessed) > 0 {
			return loopStop("INTERNAL_FAILURE", "局部核验复用材料或策略不一致")
		}
		if record.MaterialHash != material {
			if !s.loopCompactReview() || !domain.ArticleReviewScopeContains(record.Scope, scope) {
				return loopStop("INTERNAL_FAILURE", "核验超集未覆盖当前局部义务")
			}
			parent, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-loop:candidate:"+record.CandidateHash)
			if err != nil {
				return err
			}
			var prior domain.Article
			if json.Unmarshal(parent.Result, &prior) != nil || domain.ArticleHash(prior) != record.CandidateHash {
				return loopStop("INTERNAL_FAILURE", "原核验候选身份不一致")
			}
			priorHash, err := s.loopMaterialHash(ctx, prior, scope)
			if err != nil {
				return err
			}
			if priorHash != material {
				return loopStop("INTERNAL_FAILURE", "变化后的局部内容不能继承原核验")
			}
			projected, ok := domain.ArticleReviewSubset(record.Report, prior, scope, s.transcript)
			if !ok {
				return loopStop("INTERNAL_FAILURE", "受阻断问题影响的范围不能限制为局部批准")
			}
			record.Report = projected
			record.VisualObservations = domain.ArticleReviewSubsetObservations(record.VisualObservations, scope)
			record.Scope = scope
			record.MaterialHash = material
			record.ReuseKind = "completed_scope_subset"
		} else if loopBlocking(record.Report.Issues) {
			return loopStop("INTERNAL_FAILURE", "带阻断问题的材料不能复用")
		}
		record.ReusedFrom = previous
		record.CandidateHash, record.EvidenceFingerprint = l.CandidateHash, l.EvidenceFingerprint
		key := loopReviewKey(l.CandidateHash, l.EvidenceFingerprint, scope.ID)
		if err := s.loopSave(ctx, key, "article_loop_review", record); err != nil {
			return err
		}
		l.ReviewKeys = appendUnique(l.ReviewKeys, key)
		l.Issues = append(l.Issues, record.Report.Issues...)
	}
	l.Scopes = pending
	return nil
}

func (s *session) cacheLoopReviewSubsets(ctx context.Context, a domain.Article, record domain.ArticleReviewRecord, key string) error {
	if !s.loopCompactReview() || s.loopDeltaReview() || record.Scope.Global && len(record.Scope.SourceIDs) == 0 {
		return nil
	}
	l := s.cp.ArticleLoop
	if l.ReviewCache == nil {
		l.ReviewCache = map[string]string{}
	}
	for _, scope := range s.buildLoopScopes(a, true) {
		if !domain.ArticleReviewScopeContains(record.Scope, scope) {
			continue
		}
		if _, ok := domain.ArticleReviewSubset(record.Report, a, scope, s.transcript); !ok {
			continue
		}
		hash, err := s.loopMaterialHash(ctx, a, scope)
		if err != nil {
			return err
		}
		l.ReviewCache[hash] = key
	}
	return nil
}
