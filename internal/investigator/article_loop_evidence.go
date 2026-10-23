package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

const loopChallengeTool = "challenge_review"

type loopReviewChallenge struct {
	IssueID        string                        `json:"issue_id"`
	Reason         string                        `json:"reason"`
	EvidenceQuotes []domain.ArticleEvidenceQuote `json:"evidence_quotes"`
}
type loopChallengeInput struct {
	ExpectedVersion int                   `json:"expected_version"`
	Responses       []loopReviewChallenge `json:"responses" jsonschema:"description=Address every current remaining blocking issue by exact review_findings issue_id, reason and verbatim original passages. This does not approve the article."`
}

func (s *session) loopEvidenceReview() bool {
	return s.cp.ArticleLoop != nil && domain.ArticleEvidenceReview(s.cp.ArticleLoop.ReviewRevision)
}

func (s *session) loopEditorialReview() bool {
	return s.cp.ArticleLoop != nil && s.cp.ArticleLoop.ReviewRevision == domain.ArticleConvergenceReviewV5
}

const loopEditorialReviewInstructions = `
区分出处支持与编排偏好：逐字引语匹配原文、绑定正确，且没有被错误归因或用来支持不成立的主张时，仅因它位于另一主题章节、缺少过渡句或你偏好另一位置，不构成AC04 blocking；可提出AC06 advisory。若编排确实造成错误含义、矛盾或无法独立理解，指出具体被误导的主张与上下文，按相应AC报告实质缺陷；不能仅以“读者可能误会”代替证明。
修正建议须检查全文已有覆盖：挪动或删除引语前，对照当前候选是否已完整保留该事实、条件和必要细节。删除有重复表达的装饰引语可以成立，不强求逐字重复；删掉唯一必要信息则属于AC02。此边界不撤销既有阻断，unresolved_findings仍须逐条独立裁决。`

const loopExclusivePatchInstructions = `
一次行动允许替换一些块并删除其他块；同一个block_id只能在replacements中替换一次或在remove_block_ids中删除一次，不能同时出现。删除不是移动，不能用同ID替换加删除来移动块。冗余内容仅在全文已有实质覆盖时删除；确需搬移唯一信息时，用save_sections在一次原子行动中完整替换受影响章节，保留全部必要正文和sources。冲突会整次拒绝，不会部分保存。`

func loopChallengeFindings(issues []domain.ArticleReviewIssue) []any {
	out := []any{}
	for _, issue := range issues {
		if issue.Severity == "blocking" {
			out = append(out, map[string]any{"issue_id": domain.ContentHash(issue), "finding": issue})
		}
	}
	return out
}

// Stable section identity is the boundary of a completed work unit. Changed
// content/evidence is checked separately; regrouping must not erase completion.
func (s *session) retainedLoopScopes(a domain.Article) []domain.ArticleReviewScope {
	plan := s.cp.ArticleLoop.ReviewPlan
	if len(plan) == 0 {
		return nil
	}
	sections := map[string]bool{}
	sources := map[string]bool{}
	for _, sc := range plan {
		for _, id := range sc.SectionIDs {
			sections[id] = true
		}
		for _, id := range sc.SourceIDs {
			sources[id] = true
		}
	}
	if len(sections) != len(a.Sections) || len(sources) != len(s.transcript) {
		return nil
	}
	for _, sec := range a.Sections {
		if !sections[sec.ID] {
			return nil
		}
	}
	for _, seg := range s.transcript {
		if !sources[seg.ID] {
			return nil
		}
	}
	out := []domain.ArticleReviewScope{}
	for _, sc := range plan {
		sc.FrameIDs = nil
		if !(sc.Global && len(sc.SourceIDs) == 0) {
			for _, sec := range a.Sections {
				if slices.Contains(sc.SectionIDs, sec.ID) {
					for _, id := range store.ContentFrameIDs([]domain.Section{sec}, a.Citations) {
						sc.FrameIDs = appendUnique(sc.FrameIDs, id)
					}
				}
			}
		}
		sc = s.loopScopeContext(a, sc)
		out = append(out, sc)
	}
	return out
}
func (s *session) challengeLoopReview(ctx context.Context, in loopChallengeInput) (bool, error) {
	l := s.cp.ArticleLoop
	saved, d, err := s.loadLoopDraft(ctx)
	if err != nil {
		return false, err
	}
	reject := func(message string) (bool, error) {
		l.Feedback = domain.JSON(map[string]any{"failure": "review_challenge", "detail": message})
		return false, nil
	}
	if saved.Version != in.ExpectedVersion {
		return false, domain.Err(409, "DRAFT_VERSION_CONFLICT", "异议稿件版本已变化")
	}
	hash := domain.ArticleHash(d.Article)
	issues := l.BlockedCandidates[hash]
	if len(issues) == 0 || l.Failures["challenge:"+hash] > 0 {
		return reject("Only one evidence-backed challenge per unchanged blocked candidate is allowed; repair genuine defects or stop.")
	}
	scope := domain.ArticleReviewScope{SourceIDs: []string{}}
	for _, seg := range s.transcript {
		scope.SourceIDs = append(scope.SourceIDs, seg.ID)
	}
	seen := map[string]bool{}
	for _, response := range in.Responses {
		if seen[response.IssueID] || len(response.Reason) < 6 || len(response.EvidenceQuotes) == 0 || !slices.ContainsFunc(issues, func(i domain.ArticleReviewIssue) bool { return domain.ContentHash(i) == response.IssueID }) {
			return reject("Each remaining finding needs its exact issue_id, a concrete reason and verbatim source passages.")
		}
		if err := domain.ValidateArticleEvidenceQuotes(response.EvidenceQuotes, scope, s.transcript); err != nil {
			return reject(err.Error())
		}
		seen[response.IssueID] = true
	}
	if len(seen) != len(issues) {
		return reject("Address every remaining blocking finding; do not silently drop any issue.")
	}
	l.Failures["challenge:"+hash]++
	l.Challenges = slices.Clone(in.Responses)
	l.ChallengeCandidate = hash
	// Challenges are data. They reopen independent review, never close findings.
	return s.freezeLoopCandidate(ctx, loopFinishInput{ExpectedVersion: saved.Version})
}

const loopEvidenceAuthorInstructions = `
复核意见是待核实主张，不是人工确认。先直接对照原文检查意见与建议是否成立：主体归属需读前后对话，专名可查全片，AC04引用缺口应补真正支持的引用，不为姓名找装饰图片，也不改掉有依据的事实。确有错误用最小修订一次处理；如果当前全部剩余阻断不成立，用challenge_review给出review_findings的issue_id、具体理由和原文逐字反证。程序只验证引文真实存在，独立核验者再次判断意见，作者不能自批。相同稿件只允许一次异议，争议仍无法解决则停止，不能循环改措辞。
candidate_validation是最近一次候选准入失败的独立记录，不因读状态/原文/像素而消失；先处理它再finish，不把已保存的新稿当作旧稿异议。正文引用fN也必须提供像素观察，用revise_blocks.section_observations补充当前章实际可见事实，或删除无依据的画面断言/引用；不需要为此重写整章。只读取证与状态回读计入总调用预算，不计修订尝试；save/revise/finish/challenge计修订尝试，保存本身不重置额度。`
const loopEvidenceReviewInstructions = `
unresolved_findings列出上轮仍未关闭的问题，resolutions必须逐条填写issue_id、outcome与explanation：fixed代表当前稿件实际修复，withdrawn代表原意见经原文反证不成立（给evidence_quotes），remaining必须继续列入blocking。不能靠漏写旧问题宣布完成。
每条blocking提交前，直接核对其依据和建议：用evidence_quotes给出短原文逐字片段（source_id与evidence_ids一致），problem说明候选哪项主张与之冲突或哪条绑定缺失。引文必须原样存在，引用存在只证明出处，不证明你的推断正确；检查前后承接与相反解释。区分来源明确反证、就近引用缺失、来源自身含糊；没有证据不要猜说话人，不靠外部知识改写来源。review_challenges是作者提出的未采信异议；直接读原文核实，异议成立则撤回该条，不成立保留具体反证。仅在全部义务已完成且无剩余实质缺陷时返回空issues，不能为了完成撤回真实问题。`
const loopEvidenceDeltaInstructions = `
revision_delta由程序计算，prior_complete_review是本次范围此前完成的审读。集中核实原阻断（含作者异议）、实际变更块/引用、删除造成的覆盖缺口及变更的关系影响；未变材料已审读，不逐句重做。prior_block_baselines供对照。完整原文仍是事实依据；当前scope确定主要主题覆盖范围。如果顺带发现未变部分的真实新缺陷，不得隐瞒或降级，给出具体位置和evidence_quotes直接依据，程序保留该问题。不要为寻找新问题反复重审。完整候选批准不继承：综合任务还须核对当前全文关系；分段任务后程序另做当前全文关系核对。`

func requireNewFindingEvidence(report domain.ArticleReviewReport, delta *domain.ArticleReviewDelta) error {
	if delta == nil {
		return nil
	}
	for _, issue := range report.Issues {
		if !delta.ContainsIssue(issue) && len(issue.EvidenceQuotes) == 0 {
			return fmt.Errorf("new finding outside previously assigned delta requires concrete source quotes; do not omit a genuine defect: %s", issue.Location)
		}
	}
	return nil
}

func (s *session) latestLoopReview(ctx context.Context, scope domain.ArticleReviewScope) (string, domain.ArticleReviewRecord, error) {
	key := s.cp.ArticleLoop.ReviewLatest[scope.ID]
	var record domain.ArticleReviewRecord
	if key == "" {
		return "", record, nil
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
	if err != nil {
		return "", record, err
	}
	if json.Unmarshal(op.Result, &record) != nil || op.Kind != "article_loop_review" || op.Status != "completed" || record.PolicyRevision != s.cp.ArticleLoop.PolicyRevision || !domain.ArticleReviewSameWorkUnit(record.Scope, scope) {
		return "", record, loopStop("INTERNAL_FAILURE", "待解决复核记录身份不一致")
	}
	return key, record, nil
}

func (s *session) loopCandidateValidation(d articleLoopDraft, version int, cause error) json.RawMessage {
	missing := []any{}
	for _, id := range store.ContentFrameIDs(d.Article.Sections, d.Article.Citations) {
		note := d.Notes[id]
		seen := slices.Contains(s.cp.ArticleLoop.SeenFrames, id)
		if len(note.VisibleFacts) > 0 && seen {
			continue
		}
		alias := id
		for i, frame := range s.cp.ArticleLoop.Frames {
			if frame == id {
				alias = fmt.Sprint("f", i+1)
			}
		}
		missing = append(missing, map[string]any{"frame": alias, "pixels_delivered": seen, "observation_missing": len(note.VisibleFacts) == 0})
	}
	return domain.JSON(map[string]any{"draft_version": version, "failure": "candidate_validation", "detail": cause.Error(), "missing_pixel_observations": missing, "action": "The draft was saved but this candidate was not admitted for independent review. Correct the concrete condition, then request finish at the current version. For text citing delivered frames, upsert section_observations with visible facts, or remove unsupported visual claims/citations. Read-only actions and challenges do not close this condition; a changed draft must be validated again."})
}
