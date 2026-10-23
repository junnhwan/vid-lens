package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

type articleLoopBudget struct {
	InputCharsLimit int       `json:"input_chars_limit"`
	ContextTokens   int       `json:"context_tokens"`
	MaxOutputTokens int       `json:"max_output_tokens"`
	TokenCeiling    int64     `json:"token_ceiling,omitempty"`
	CostStatus      string    `json:"cost_status"`
	Calls           int       `json:"calls"`
	Sends           int       `json:"sends"`
	NoProgress      int       `json:"no_progress"`
	Sections        int       `json:"sections"`
	DraftBytes      int       `json:"draft_bytes"`
	Revisions       int       `json:"revisions"`
	MediaBytes      int       `json:"media_bytes"`
	ActiveSeconds   int64     `json:"active_seconds"`
	Deadline        time.Time `json:"deadline"`
}
type articleLoopState struct {
	Version              int                                    `json:"version"`
	Budget               articleLoopBudget                      `json:"budget"`
	Pending              *articleLoopCall                       `json:"pending,omitempty"`
	ReadSources          []string                               `json:"read_sources"`
	SeenFrames           []string                               `json:"seen_frames"`
	Frames               []string                               `json:"frames"`
	NextSections         []string                               `json:"next_sections,omitempty"`
	NextSources          []string                               `json:"next_sources,omitempty"`
	NextFrames           []string                               `json:"next_frames,omitempty"`
	CandidateHash        string                                 `json:"candidate_hash"`
	DraftVersion         int                                    `json:"draft_version"`
	EvidenceFingerprint  string                                 `json:"evidence_fingerprint"`
	PolicyRevision       string                                 `json:"policy_revision"`
	Scopes               []domain.ArticleReviewScope            `json:"scopes,omitempty"`
	ReviewKeys           []string                               `json:"review_keys,omitempty"`
	Issues               []domain.ArticleReviewIssue            `json:"issues,omitempty"`
	Feedback             json.RawMessage                        `json:"feedback,omitempty"`
	NoProgress           int                                    `json:"no_progress"`
	BlockedCandidates    map[string][]domain.ArticleReviewIssue `json:"blocked_candidates,omitempty"`
	Failures             map[string]int                         `json:"failures"`
	OutputTokens         int                                    `json:"output_tokens"`
	SourceChars          int                                    `json:"source_chars"`
	ReviewReserve        int                                    `json:"review_reserve"`
	ReviewSourceLimit    int                                    `json:"review_source_limit,omitempty"`
	OutputSections       int                                    `json:"output_sections"`
	InspectionKeys       []string                               `json:"inspection_keys,omitempty"`
	ReviewRevision       string                                 `json:"review_revision,omitempty"`
	ReviewCache          map[string]string                      `json:"review_cache,omitempty"`
	RepairRounds         int                                    `json:"repair_rounds,omitempty"`
	RepairActions        int                                    `json:"repair_actions,omitempty"`
	ReviewWorkLimit      int                                    `json:"review_work_limit,omitempty"`
	LimitedReviewContext bool                                   `json:"limited_review_context,omitempty"`
	ReviewPlan           []domain.ArticleReviewScope            `json:"review_plan,omitempty"`
	ReviewLatest         map[string]string                      `json:"review_latest,omitempty"`
	ReviewBases          map[string]string                      `json:"review_bases,omitempty"`
	Challenges           []loopReviewChallenge                  `json:"review_challenges,omitempty"`
	ChallengeCandidate   string                                 `json:"challenge_candidate,omitempty"`
	ReviewBaseKey        string                                 `json:"review_base_key,omitempty"`
}
type articleLoopCall struct {
	Slot         int                        `json:"slot"`
	Mode         string                     `json:"mode"`
	Sections     []string                   `json:"sections,omitempty"`
	Sources      []string                   `json:"sources"`
	Frames       []string                   `json:"frames"`
	Scope        *domain.ArticleReviewScope `json:"scope,omitempty"`
	OutputTokens int                        `json:"output_tokens"`
	Delta        *domain.ArticleReviewDelta `json:"delta,omitempty"`
}

func (s *session) articleLoop() bool {
	return s != nil && s.cp.ExecutionContract != nil && domain.ArticleLoopContract(s.cp.ExecutionContract.Version)
}
func (s *session) validateArticleLoopState() error {
	l := s.cp.ArticleLoop
	if l == nil {
		if s.cp.Calls != 0 {
			return domain.Err(422, "INTERNAL_FAILURE", "文章循环预算账缺失，不能重置。")
		}
		return nil
	}
	if l.Version != 1 || l.Budget.Calls < 1 || l.Budget.Calls > min(20, s.profile.MaxIterations) || l.Budget.Sends != l.Budget.Calls || l.Budget.InputCharsLimit != separateInputLimit(s.profile) || l.Budget.ContextTokens != s.profile.ContextTokens || l.Budget.MaxOutputTokens != s.profile.MaxOutputTokens || l.Budget.CostStatus != "unknown" || l.Budget.NoProgress < 1 || l.Budget.NoProgress > 6 || l.Budget.DraftBytes != 512<<10 || l.Budget.Sections != 40 || l.Budget.Revisions != 48 || l.Budget.MediaBytes != 20<<20 || l.Budget.ActiveSeconds != s.profile.ArticleActiveSeconds || !l.Budget.Deadline.Equal(s.claim.Run.DeadlineAt) || l.Failures == nil || l.SourceChars < 1 || l.OutputSections < 1 || l.OutputTokens < 1 || s.cp.Calls < 0 || s.cp.Calls > l.Budget.Calls {
		return domain.Err(422, "INTERNAL_FAILURE", "文章循环冻结限制或检查点损坏。")
	}
	if l.Pending != nil && (l.Pending.Slot != s.cp.Calls-1 || l.Pending.Slot < 0 || (l.Pending.Mode != "author" && l.Pending.Mode != "review")) {
		return domain.Err(422, "INTERNAL_FAILURE", "文章循环待完成调用身份损坏。")
	}
	if l.ReviewSourceLimit < 0 {
		return domain.Err(422, "INTERNAL_FAILURE", "文章核验工作范围损坏。")
	}
	if l.ReviewRevision != "" && !domain.ArticleConvergenceReview(l.ReviewRevision) || l.RepairRounds < 0 || l.RepairActions < 0 || l.ReviewWorkLimit < 0 || l.ReviewWorkLimit > loopReviewWorkRunes {
		return domain.Err(422, "INTERNAL_FAILURE", "文章收敛策略或修订账损坏。")
	}
	return nil
}
func loopStop(code, message string) error {
	return domain.Err(422, code, message+"；原文、稿件、回执和调用账均保留。")
}
func (s *session) runArticleLoop(ctx context.Context) error {
	if op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate"); err == nil && op.Status == "completed" {
		return loopStop("ARTICLE_APPROVAL_REQUIRED", "旧候选不能绕过新契约核验")
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if run, err := s.engine.Store.Run(ctx, s.claim.Run.ID); err != nil {
		return err
	} else if run.Status == "succeeded" && run.ResultID != nil {
		a, err := s.engine.Store.PublishedArticle(ctx, *run.ResultID)
		if err != nil {
			return err
		}
		if s.cp.ArticleLoop == nil || domain.ArticleHash(a) != s.cp.ArticleLoop.CandidateHash {
			return loopStop("INTERNAL_FAILURE", "已发布文章与冻结候选不一致")
		}
		return nil
	}

	if s.cp.ArticleLoop == nil {
		limit := 6
		if s.spec.DetailPolicy != nil && s.spec.DetailPolicy.NoProgressLimit > 0 {
			limit = min(limit, s.spec.DetailPolicy.NoProgressLimit)
		}
		s.cp.ArticleLoop = &articleLoopState{Version: 1, Budget: articleLoopBudget{InputCharsLimit: separateInputLimit(s.profile), ContextTokens: s.profile.ContextTokens, MaxOutputTokens: s.profile.MaxOutputTokens, TokenCeiling: int64(min(20, s.profile.MaxIterations)) * int64(s.profile.ContextTokens), CostStatus: "unknown", Calls: min(20, s.profile.MaxIterations), Sends: min(20, s.profile.MaxIterations), NoProgress: limit, Sections: 40, DraftBytes: 512 << 10, Revisions: 48, MediaBytes: 20 << 20, ActiveSeconds: s.profile.ArticleActiveSeconds, Deadline: s.claim.Run.DeadlineAt}, Failures: map[string]int{}, OutputTokens: min(s.profile.MaxOutputTokens, 32768), SourceChars: max(1000, separateInputLimit(s.profile)/2), OutputSections: 40}
		if s.cp.ExecutionContract.Version == domain.ArticleLoopV2 {
			s.cp.ArticleLoop.ReviewRevision = domain.ArticleConvergenceReviewV4
			s.cp.ArticleLoop.ReviewCache = map[string]string{}
		}
		s.cp.Protocol = s.cp.ExecutionContract.Version
		s.cp.LayoutVersion = "article-workspace-v1"
		s.cp.Ready = true
		if err := s.persist(ctx, nil); err != nil {
			return err
		}
	}
	if err := s.validateArticleLoopState(); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.engine.Store.Check(ctx, s.claim); err != nil {
			return err
		}
		l := s.cp.ArticleLoop
		if l.CandidateHash != "" {
			op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-loop:approval:"+l.CandidateHash)
			if err == nil && op.Status == "completed" {
				return s.publishLoopCandidate(ctx)
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		if l.Pending == nil {
			if s.cp.Calls >= l.Budget.Calls {
				return loopStop("ARTICLE_BUDGET_EXHAUSTED", "文章总调用预算已用完")
			}
			if l.NoProgress >= l.Budget.NoProgress {
				return loopStop("ARTICLE_NO_PROGRESS", "连续行动没有推进当前义务")
			}
			pending, input, infos, err := s.prepareLoopCall(ctx)
			if err != nil {
				return err
			}
			// Capacity is resolved before reserving a logical slot. All transport sends
			// then share its exact durable input, tools, source range and pixels.
			if _, err = s.freezeModelInput(ctx, fmt.Sprint("model:", pending.Slot), input, map[string]any{"messages": stripPixels(input), "budget": s.articleCallBudget, "tools_hash": digest(domain.JSON(infos)), "policy_revision": domain.ArticleLoopPolicy, "parallel_tool_calls": false}, s.articleCallBudget, infos); err != nil {
				return err
			}
			l.Pending = pending
			if s.loopConvergence() && pending.Mode == "author" && len(l.Issues) > 0 {
				l.RepairActions++
			}
			s.cp.Calls++
			if err = s.persist(ctx, nil); err != nil {
				return err
			}
		}
		pending := *l.Pending
		// Completed local turn recovery precedes generation and action replay.
		turnkey := fmt.Sprint("article-loop:turn:", pending.Slot)
		op, err := s.engine.Store.BeginOperation(ctx, s.claim, turnkey, "article_loop_turn", map[string]int{"slot": pending.Slot}, false)
		if err != nil {
			return err
		}
		if op.Status == "completed" {
			var next articleLoopState
			if err = json.Unmarshal(op.Result, &next); err != nil {
				return err
			}
			s.cp.ArticleLoop = &next
			if err = s.persist(ctx, nil); err != nil {
				return err
			}
			continue
		}
		infos, err := s.loopTools(pending.Mode == "review")
		if err != nil {
			return err
		}
		s.articleCall = &pending
		reserved := s.cp.Calls
		s.cp.Calls = pending.Slot
		bound, err := (&meteredModel{session: s}).WithTools(infos)
		var response *schema.Message
		if err == nil {
			response, err = bound.Generate(ctx, nil)
		}
		s.cp.Calls = reserved
		s.articleCall = nil
		if err != nil {
			if err = s.recoverLoopFailure(ctx, pending, err); err != nil {
				return err
			}
		} else {
			evidenceProgress := false
			for _, id := range pending.Sources {
				evidenceProgress = evidenceProgress || !slices.Contains(l.ReadSources, id)
				l.ReadSources = appendUnique(l.ReadSources, id)
			}
			for _, id := range pending.Frames {
				evidenceProgress = evidenceProgress || !slices.Contains(l.SeenFrames, id)
				l.SeenFrames = appendUnique(l.SeenFrames, id)
			}
			progress, e := s.applyLoopResponse(ctx, pending, response)

			if e != nil {
				var conflict *domain.Problem
				if !errors.As(e, &conflict) || conflict.Code != "DRAFT_VERSION_CONFLICT" {
					return e
				}
				if err := s.engine.Store.Check(ctx, s.claim); err != nil {
					return err
				}
				l.Feedback = domain.JSON(map[string]any{"failure": "workspace_changed", "action": "Authoritative state has been reloaded. Keep accepted chapters and use the current version for the intended local edit."})
			}
			if progress || evidenceProgress {
				l.NoProgress = 0
			} else {
				l.NoProgress++
			}
		}
		l = s.cp.ArticleLoop
		l.Pending = nil
		if err = s.engine.Store.FinishOperation(ctx, s.claim, turnkey, l, "", nil); err != nil {
			return err
		}
		if err = s.persist(ctx, nil); err != nil {
			return err
		}
	}
}
func appendUnique(values []string, value string) []string {
	if !slices.Contains(values, value) {
		return append(values, value)
	}
	return values
}
func (s *session) loopSave(ctx context.Context, key, kind string, value any) error {
	if _, err := s.engine.Store.BeginOperation(ctx, s.claim, key, kind, map[string]string{"identity": store.Hash(value)}, false); err != nil {
		return err
	}
	return s.engine.Store.FinishOperation(ctx, s.claim, key, value, "", nil)
}
func (s *session) recoverLoopFailure(ctx context.Context, p articleLoopCall, cause error) error {
	l := s.cp.ArticleLoop
	var work *domain.WorkError
	var problem *domain.Problem
	code := ""
	if errors.As(cause, &work) {
		code = work.Code
	}
	if errors.As(cause, &problem) {
		code = problem.Code
	}
	if code == "MODEL_OUTPUT_TRUNCATED" || code == "MODEL_RESPONSE_BUDGET_EXHAUSTED" {
		l.OutputSections = max(1, l.OutputSections/2)
		// A truncated review is split as a scope, never routed to the author.
		if p.Mode == "review" {
			if s.loopConvergence() {
				_, draft, err := s.loadLoopDraft(ctx)
				if err != nil {
					return err
				}
				l.ReviewWorkLimit = min(loopReviewWorkRunes, max(1000, s.loopScopeWork(draft.Article, *p.Scope)/2))
			} else if s.cp.ExecutionContract != nil && s.cp.ExecutionContract.Version == domain.ArticleLoopV2 && len(p.Scope.SourceIDs) > 1 {
				limit := (len(p.Scope.SourceIDs) + 1) / 2
				if l.ReviewSourceLimit == 0 || limit < l.ReviewSourceLimit {
					l.ReviewSourceLimit = limit
				}
			}
			if err := s.splitLoopReview(ctx, *p.Scope); err != nil {
				return err
			}
		}
		l.Feedback = domain.JSON(map[string]any{"failure": code, "action": "Narrow the next complete output; do not repeat saved sections or complete a partial JSON object.", "max_sections": l.OutputSections})
		l.NoProgress++
		return nil
	}
	if code == "RESOURCE_LIMIT_EXCEEDED" || code == "MODEL_CONTEXT_EXCEEDED" {
		if p.Mode == "review" {
			l.NoProgress++
			return s.splitLoopReview(ctx, *p.Scope)
		}
		if n := len(p.Sources); n > 0 {
			originals := s.loopOriginals(p.Sources)
			l.SourceChars = min(l.SourceChars, max(256, len(domain.JSON(originals))/4))
		}
		if len(p.Frames) > 1 {
			l.NextFrames = append([]string{}, p.Frames[:len(p.Frames)/2]...)
		}
		l.OutputTokens = max(1024, l.OutputTokens/2)
		l.OutputSections = max(1, l.OutputSections/2)
		l.SourceChars = max(256, l.SourceChars/2)
		l.NextSources = nil
		l.NoProgress++
		l.Feedback = domain.JSON(map[string]any{"failure": code, "action": "Read smaller original ranges; all missing source coverage remains required."})
		return nil
	}
	// Provider/unknown outcome stays in the same pending operation. Worker retry
	// uses the existing deadlines and physical-send ledger; no new logical slot.
	return cause
}

func (s *session) applyLoopResponse(ctx context.Context, p articleLoopCall, msg *schema.Message) (bool, error) {
	l := s.cp.ArticleLoop
	action := msg
	if msg != nil && s.cp.ExecutionContract != nil && s.cp.ExecutionContract.Version == domain.ArticleLoopV2 {
		// Commentary has no authority to save or approve anything. A complete
		// tool action still goes through all argument and business validation;
		// don't spend another model call merely to remove accompanying prose.
		// Keep the original response intact in the operation ledger.
		copy := *msg
		copy.Content = ""
		action = &copy
	}
	name, raw, err := loopArguments(action)
	if err != nil {
		if p.Mode == "review" && s.loopConvergence() && p.Scope != nil {
			l.Failures[s.loopReviewProtocolKey(p.Scope.ID)]++
		}
		l.Feedback = domain.JSON(map[string]any{"failure": "envelope", "detail": err.Error(), "action": "Return one complete named tool call; no partial content was saved."})
		if msg != nil && len(msg.ToolCalls) == 1 {
			call := msg.ToolCalls[0].Function
			if p.Mode == "review" && call.Name == loopReviewTool {
				l.Feedback = loopReviewFailureFeedback(err, []byte(call.Arguments))
			} else if p.Mode == "author" {
				l.Feedback = loopAuthorFailureFeedback("envelope", err, call.Name, []byte(call.Arguments))
			}
		}
		if p.Mode == "author" {
			s.narrowLoopAuthorFailure()
		}
		return false, nil
	}
	// Display metadata must not become a business argument. The original
	// response remains intact in the model operation ledger.
	if s.cp.ExecutionContract != nil && s.cp.ExecutionContract.Version == domain.ArticleLoopV2 {
		var args map[string]json.RawMessage
		if json.Unmarshal(raw, &args) == nil && args["activity_title"] != nil {
			delete(args, "activity_title")
			raw = domain.JSON(args)
		}
	}
	if p.Mode == "review" {
		if name != loopReviewTool {
			if s.loopConvergence() && p.Scope != nil {
				l.Failures[s.loopReviewProtocolKey(p.Scope.ID)]++
			}
			l.Feedback = domain.JSON(map[string]any{"review_failure": "wrong review tool", "action": "Return exactly one report_article_review call; the article cannot be edited during review."})
			return false, nil
		}
		return s.applyLoopReview(ctx, *p.Scope, raw, p.Delta)
	}
	key := fmt.Sprint("loop:", p.Slot)
	switch name {
	case loopSaveTool, loopPatchTool:
		var in loopSaveInput
		var result any
		var progress bool
		var e error
		if name == loopPatchTool {
			if !s.loopConvergence() {
				err = errors.New("block revision is unavailable under the admitted contract")
				break
			}
			var patch loopPatchInput
			if err = loopDecode(raw, &patch); err != nil {
				break
			}
			in.Finish = patch.Finish
			result, progress, e = s.patchLoopBlocks(ctx, key, patch)
		} else {
			if err = loopDecode(raw, &in); err != nil {
				break
			}
			if s.cp.ExecutionContract.Version != domain.ArticleLoopV2 && len(in.Sections) > l.OutputSections {
				l.Feedback = domain.JSON(map[string]any{"failure": "output_scope", "max_sections": l.OutputSections})
				return false, nil
			}
			result, progress, e = s.saveLoopSections(ctx, key, in)
		}
		// V2's smaller output target guides recovery from malformed/truncated
		// responses. A complete valid write is still governed by the frozen
		// 40-section/128-KiB limits, not rejected for exceeding that suggestion.
		if e != nil {
			return false, e
		}
		l.Feedback = loopWriteFeedback(result, raw)
		write, _ := result.(map[string]any)
		receipt, _ := write["receipt"].(*loopReceipt)
		message := "本次章节未保存，已将具体错误反馈给模型；原有章节保留。"
		if receipt != nil {
			message = fmt.Sprintf("本次接纳 %d 章，退回 %d 章；文章仍需通过独立核验。", len(receipt.Accepted), len(receipt.Rejected))
		}
		if write["noop"] == true && receipt != nil && len(receipt.Rejected) == 0 {
			message = "章节内容未变化，未创建新版本，待修订问题仍然保留。"
		}
		now := time.Now().UTC()
		if _, e := s.engine.Store.Step(ctx, s.claim, domain.Step{ID: fmt.Sprintf("loop_write_%s_%d", s.claim.Run.ID, p.Slot), Kind: "tool_execution", ModelCall: p.Slot + 1, Title: "章节保存结果", Phase: "compose", Status: "completed", StartedAt: &now, FinishedAt: &now, Outcome: &domain.Outcome{Message: message, EvidenceStatus: "not_assessed"}}); e != nil {
			return false, e
		}
		if progress {
			l.ChallengeCandidate = ""
			l.Challenges = nil
			l.CandidateHash = ""
			l.Scopes = nil
			l.ReviewKeys = nil
			l.DraftVersion = 0
		}

		if in.Finish {
			// Only this action's receipt can request review. Early write failures
			// do not persist a new draft; the old draft's successful receipt says
			// nothing about whether this revision was accepted.
			if write["error"] == nil && receipt != nil && len(receipt.Rejected) == 0 {
				latest, e := s.engine.Store.ArticleDraft(ctx, s.claim.Run.ID)
				if e != nil {
					return false, e
				}
				frozen, e := s.freezeLoopCandidate(ctx, loopFinishInput{ExpectedVersion: latest.Version})
				return progress || frozen, e
			}
		}
		return progress, nil
	case loopChallengeTool:
		var in loopChallengeInput
		if !s.loopEvidenceReview() {
			err = errors.New("review challenge unavailable")
			break
		}
		if err = loopDecode(raw, &in); err != nil {
			break
		}
		return s.challengeLoopReview(ctx, in)
	case loopFinishTool:
		var in loopFinishInput
		if err = loopDecode(raw, &in); err != nil {
			break
		}
		return s.freezeLoopCandidate(ctx, in)
	case "read_article_state":
		var in loopStateInput
		err = loopDecode(raw, &in)
		if err == nil {
			_, draft, e := s.loadLoopDraft(ctx)
			if e != nil {
				return false, e
			}
			for _, id := range in.SectionIDs {
				if !slices.ContainsFunc(draft.Article.Sections, func(sec domain.Section) bool { return sec.ID == id }) {
					l.Feedback = domain.JSON(map[string]any{"error": "unknown section identity", "id": id})
					return false, nil
				}
			}
			l.NextSections = append([]string{}, in.SectionIDs...)
			l.Feedback = domain.JSON(map[string]any{"state": "The next request contains the authoritative current draft, remaining coverage and budget."})
		}
		return false, nil

	case "stop_article":
		var in loopCannotCompleteInput
		if err = loopDecode(raw, &in); err != nil {
			break
		}
		if strings.TrimSpace(in.Reason) == "" || len([]rune(in.Reason)) > 1000 {
			err = errors.New("a specific reason of at most 1000 characters is required")
			break
		}
		catalog, e := s.loopCatalog(ctx)
		if e != nil {
			return false, e
		}
		for _, id := range in.EvidenceIDs {
			if _, ok := catalog[id]; !ok {
				err = errors.New("stop reason references undelivered evidence")
				break
			}
		}
		if err != nil {
			break
		}
		return false, loopStop("ARTICLE_CANNOT_COMPLETE", in.Reason)
	case "read_evidence":
		var in loopReadInput
		if err = loopDecode(raw, &in); err != nil {
			break
		}
		ids := []string{}
		for i, t := range s.transcript {
			alias := fmt.Sprint("t", i+1)
			selected := len(in.SourceIDs) == 0 && !slices.Contains(l.ReadSources, alias)
			selected = selected || slices.Contains(in.SourceIDs, alias) || in.Query != "" && strings.Contains(t.Text, in.Query)
			if selected {
				ids = append(ids, alias)
			}
		}
		for _, id := range in.SourceIDs {
			found := false
			for i := range s.transcript {
				found = found || id == fmt.Sprint("t", i+1)
			}
			if !found {
				l.Feedback = domain.JSON(map[string]any{"error": "unknown source", "id": id})
				return false, nil
			}
		}
		l.NextSources = ids
		l.Feedback = domain.JSON(map[string]any{"selected_sources": ids, "note": "Only sources actually delivered in the next input count as read; follow remaining coverage."})
		return false, nil
	case "view_frames":
		var in loopViewInput
		if err = loopDecode(raw, &in); err != nil {
			break
		}
		if strings.TrimSpace(in.Question) == "" {
			err = errors.New("a concrete visual question is required")
			break
		}
		ids := []string{}
		for _, alias := range in.FrameIDs {
			index := -1
			for i := range l.Frames {
				if alias == fmt.Sprint("f", i+1) {
					index = i
				}
			}
			if index < 0 {
				err = errors.New("unknown frame alias")
				break
			}
			ids = append(ids, l.Frames[index])
		}
		if err != nil {
			break
		}
		if len(ids) == 0 {
			if s.cp.Calls+1+l.ReviewReserve > l.Budget.Calls {
				l.Feedback = domain.JSON(map[string]any{"error": "optional evidence would spend reserved review budget"})
				return false, nil
			}
			window := media.Window{StartMS: in.StartMS, EndMS: in.EndMS, Count: in.Count, Mode: "targeted", Crop: in.Crop}
			if window.Count == 0 {
				window.Count = 4
			}
			if window.Crop != nil {
				window.Count = 1
			}
			inspectionKey := "article-loop:inspect:" + store.Hash(window)
			if s.cp.ExecutionContract.Version == domain.ArticleLoopV2 && !slices.Contains(l.InspectionKeys, inspectionKey) && len(l.InspectionKeys) >= loopExtraWindowLimit {
				l.Feedback = domain.JSON(map[string]any{"error": "visual_search_limit", "action": "Stop searching for illustrations. Save the complete article using supported text; omit decorative or uninformative figures. Reload existing frame_ids only for a necessary visual claim. Missing optional illustrations is not a failure; do not invent essential visual evidence."})
				return false, nil
			}
			var result media.Inspection
			old, e := s.engine.Store.Operation(ctx, s.claim.Run.ID, inspectionKey)
			if e == nil && old.Status == "completed" {
				e = json.Unmarshal(old.Result, &result)
			} else if errors.Is(e, pgx.ErrNoRows) || e == nil {
				result, e = s.engine.Media.Inspect(ctx, s.claim, inspectionKey, in.Question, window, nil)
			}

			if e != nil {
				return false, e
			}
			ids = result.FrameIDs
			l.InspectionKeys = appendUnique(l.InspectionKeys, inspectionKey)
			for _, id := range ids {
				l.Frames = appendUnique(l.Frames, id)
			}
		}
		l.NextFrames = ids
		l.Feedback = domain.JSON(map[string]any{"next_pixels": ids, "visual_question": in.Question, "note": "Sparse frames do not establish continuous actions; use the frame navigation ledger to reload earlier pixels."})
		return false, nil
	default:
		err = errors.New("tool is not authorized for the author")
	}
	if err != nil {
		l.Feedback = loopAuthorFailureFeedback("tool_contract", err, name, raw)
	}
	return false, nil
}

func (s *session) narrowLoopAuthorFailure() {
	if s.cp.ExecutionContract == nil || s.cp.ExecutionContract.Version != domain.ArticleLoopV2 {
		return
	}
	l := s.cp.ArticleLoop
	l.Failures["author_envelope"]++
	l.OutputSections = max(1, l.OutputSections/2)
	// Syntax/output failure does not establish an input-capacity limit. Keep
	// the source workspace available; only real context rejection narrows it.
}

func (s *session) freezeLoopCandidate(ctx context.Context, in loopFinishInput) (bool, error) {
	l := s.cp.ArticleLoop
	saved, d, err := s.loadLoopDraft(ctx)
	if err != nil {
		return false, err
	}
	if saved.Version != in.ExpectedVersion {
		return false, domain.Err(409, "DRAFT_VERSION_CONFLICT", "候选版本冲突，停止旧写入并重读。")
	}
	missing := []string{}
	for i := range s.transcript {
		alias := fmt.Sprint("t", i+1)
		if !slices.Contains(l.ReadSources, alias) {
			missing = append(missing, alias)
		}
	}
	if len(missing) > 0 {
		l.Feedback = domain.JSON(map[string]any{"failure": "source_coverage", "unread": missing})
		l.NextSources = missing
		return false, nil
	}
	if err = s.validateLoopArticle(ctx, d.Article, d); err != nil {
		l.Feedback = domain.JSON(map[string]any{"failure": "candidate_validation", "detail": err.Error()})
		return false, nil
	}
	fingerprint, err := s.loopEvidenceFingerprint(ctx, d.Article)
	if err != nil {
		return false, err
	}
	hash := domain.ArticleHash(d.Article)
	if blocking := l.BlockedCandidates[hash]; len(blocking) > 0 && !(s.loopEvidenceReview() && l.ChallengeCandidate == hash) {
		l.Issues = blocking
		l.Feedback = domain.JSON(map[string]any{"failure": "unchanged_blocking_candidate", "issues": blocking, "action": "The same candidate still has unresolved review findings; revise affected content instead of requesting the same review."})
		return false, nil
	}
	if l.CandidateHash == hash && len(l.Scopes) > 0 {
		return false, nil
	}
	l.CandidateHash = hash
	l.EvidenceFingerprint = fingerprint
	l.PolicyRevision = domain.ArticleReviewPolicyIdentity(s.spec, s.cp.ExecutionContract.Version, l.ReviewRevision)
	l.DraftVersion = saved.Version
	l.ReviewKeys = nil
	l.Issues = nil
	if err = s.loopSave(ctx, "article-loop:candidate:"+hash, "article_loop_candidate", d.Article); err != nil {
		return false, err
	}
	scopes, err := s.planLoopReview(ctx, d.Article)
	if err != nil {
		return false, err
	}
	if !s.loopConvergence() && len(scopes) > l.Budget.Calls-s.cp.Calls {
		return false, loopStop("ARTICLE_BUDGET_EXHAUSTED", "剩余预算不足以完成当前候选的全部核验")
	}
	l.Scopes = scopes
	if s.loopConvergence() {
		if err := s.reuseLoopReviews(ctx, d.Article); err != nil {
			return false, err
		}
	}
	if len(l.Scopes) > l.Budget.Calls-s.cp.Calls {
		return false, loopStop("ARTICLE_BUDGET_EXHAUSTED", "剩余预算不足以完成新稿的必要局部核验和全文关系核对")
	}
	l.Feedback = nil
	return true, nil
}
func (s *session) loopEvidenceFingerprint(ctx context.Context, a domain.Article) (string, error) {
	assets := []domain.Asset{}
	for _, id := range store.ContentFrameIDs(a.Sections, a.Citations) {
		asset, err := s.engine.Store.InternalFrame(ctx, s.claim, id)
		if err != nil {
			return "", err
		}
		assets = append(assets, asset)
	}
	// Read the durable source, rather than trusting a possibly stale session.
	snap, err := s.engine.Store.Snapshot(ctx, s.snapshot.ID)
	if err != nil {
		return "", err
	}
	text, err := s.engine.Store.Transcript(ctx, s.snapshot.ID)
	if err != nil {
		return "", err
	}
	if store.Hash(snap) != store.Hash(s.snapshot) || store.Hash(text) != store.Hash(s.transcript) {
		return "", loopStop("ARTICLE_EVIDENCE_CHANGED", "冻结来源已变化")
	}
	return domain.ArticleEvidenceIdentity(snap, text, assets), nil
}
func (s *session) publishLoopCandidate(ctx context.Context) error {
	l := s.cp.ArticleLoop
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "article-loop:candidate:"+l.CandidateHash)
	if err != nil {
		return err
	}
	var a domain.Article
	if err = json.Unmarshal(op.Result, &a); err != nil {
		return err
	}
	_, d, err := s.loadLoopDraft(ctx)
	if err != nil {
		return err
	}
	if domain.ArticleHash(d.Article) != l.CandidateHash {
		return loopStop("ARTICLE_APPROVAL_REQUIRED", "批准不属于当前稿件")
	}
	if err = s.validateLoopArticle(ctx, a, d); err != nil {
		return err
	}
	fingerprint, err := s.loopEvidenceFingerprint(ctx, a)
	if err != nil {
		return err
	}
	if fingerprint != l.EvidenceFingerprint {
		return loopStop("ARTICLE_EVIDENCE_CHANGED", "候选证据身份已变化")
	}
	if err = s.loopProgress(ctx, "publish"); err != nil {
		return err
	}
	if err = s.engine.Store.Publish(ctx, s.claim, a); err != nil {
		return err
	}
	// Success is the committed terminal run plus the immutable row, never a step.
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil {
		return err
	}
	if run.Status != "succeeded" || run.ResultID == nil {
		return loopStop("INTERNAL_FAILURE", "发布事务尚未确认终态")
	}
	published, err := s.engine.Store.PublishedArticle(ctx, *run.ResultID)
	if err != nil {
		return err
	}
	if domain.ArticleHash(published) != l.CandidateHash {
		return loopStop("INTERNAL_FAILURE", "发布回读与批准候选不一致")
	}
	return nil
}
func (s *session) validateLoopArticle(ctx context.Context, a domain.Article, d articleLoopDraft) error {
	if a.SnapshotID != s.snapshot.ID || store.Hash(a.Source) != store.Hash(s.snapshot.Source) {
		return invalid("来源快照不一致")
	}
	if err := s.validateArticle(ctx, a); err != nil {
		return err
	}
	for _, id := range store.ContentFrameIDs(a.Sections, a.Citations) {
		n, ok := d.Notes[id]
		if !ok || len(n.VisibleFacts) == 0 || !slices.Contains(s.cp.ArticleLoop.SeenFrames, id) {
			return invalid("所用图片缺少实际像素交付与观察")
		}
	}
	var total int64
	for _, id := range store.ContentFrameIDs(a.Sections, a.Citations) {
		asset, err := s.engine.Store.Asset(ctx, id)
		if err != nil {
			return err
		}
		total += asset.Bytes
	}
	if total > 20<<20 {
		return invalid("配图总量超过20MiB；需要选择仍能解释正文的有效图片集合")
	}
	return nil
}
func (s *session) loopProgress(ctx context.Context, phase string) error {
	saved, d, err := s.loadLoopDraft(ctx)
	if err != nil {
		return err
	}
	plan := &domain.ExecutionPlan{OpenEnded: true, Items: []domain.PlanItem{{ID: "write", Title: "阅读证据并保存文章", Status: "active"}, {ID: "validate", Title: "独立核对全片主题、原文和配图", Status: "pending"}, {ID: "publish", Title: "发布正式文章", Status: "pending"}}, Note: fmt.Sprintf("已保存%d章，稿件版本%d；核验中的修改会重新检查当前候选。", len(d.Article.Sections), saved.Version)}
	if phase == "validate" {
		plan.Items[0].Status = "done"
		plan.Items[1].Status = "active"
	}
	if phase == "publish" {
		plan.Items[0].Status = "done"
		plan.Items[1].Status = "done"
		plan.Items[2].Status = "active"
	}
	for _, sec := range d.Article.Sections {
		plan.Sections = append(plan.Sections, domain.PlanItem{ID: sec.ID, Title: sec.Title, Status: "done"})
	}
	hash := store.Hash(plan)
	if hash == s.cp.PlanHash {
		return nil
	}
	_, err = s.engine.Store.Step(ctx, s.claim, domain.Step{ID: "plan_" + s.claim.Run.ID, Kind: "execution_plan", Title: "当前文章工作", Status: "completed", Plan: plan, Phase: phase, FrameIDs: []string{}})
	if err == nil {
		s.cp.PlanHash = hash
	}
	return err
}
