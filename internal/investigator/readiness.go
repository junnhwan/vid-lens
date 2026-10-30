package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type progressState struct {
	Total               int            `json:"total"`
	Stalled             int            `json:"stalled"`
	OptionalInspections int            `json:"optional_inspections"`
	Seen                map[string]int `json:"seen"`
	Recovery            *loopRecovery  `json:"loop_recovery,omitempty"`
	Epoch               int            `json:"progress_epoch,omitempty"`
}

type loopRecovery struct {
	Signature string `json:"signature"`
	Tool      string `json:"tool"`
	Reason    string `json:"reason"`
	Attempts  int    `json:"attempts"`
	NudgeCall int    `json:"nudge_call"`
}

func (s *session) loopCorrectionPending() bool {
	r := s.cp.Progress.Recovery
	return s.usesBoundedReview() && r != nil && s.cp.Calls <= r.NudgeCall
}

func (s *session) repairLimit() int {
	if !s.policy().ContentPlan && s.cp.FinalizationFailure != nil && s.cp.FinalizationFailure.Code == "MODEL_RESULT_NOT_SUBMITTED" {
		return 1
	}
	if s.policy().Enabled && s.spec.DetailPolicy != nil {
		return s.spec.DetailPolicy.RepairLimit
	}
	return 1
}

func rejectInspectionBatch(calls []schema.ToolCall) bool {
	n := 0
	for _, c := range calls {
		if c.Function.Name == "inspect_video_window" {
			n++
		}
	}
	return n > 1
}

func (s *session) readiness(ctx context.Context) (map[string]any, error) {
	room, err := s.inspectionHeadroom(ctx)
	if err != nil {
		return nil, err
	}
	questions, err := s.engine.Store.Questions(ctx, s.claim.Run.ID)
	if err != nil {
		return nil, err
	}
	blockers := []string{}
	for _, raw := range questions {
		var q struct {
			ID       string
			Critical bool
			Status   string
		}
		if json.Unmarshal(raw, &q) != nil {
			return nil, errors.New("invalid question state")
		}
		if q.Critical && q.Status != "supported" {
			blockers = append(blockers, q.ID)
		}
	}
	slices.Sort(blockers)
	pending := s.unobserved(ctx)
	slices.Sort(pending)
	optional := 0
	reserved := 4
	if p := s.spec.DetailPolicy; p != nil {
		optional = max(0, p.OptionalInspections-s.cp.Progress.OptionalInspections)
		reserved = p.FinalCallsReserved
	}
	canCompose := s.turn == nil && len(pending) == 0 && len(blockers) == 0 && len(s.cp.OverviewIDs) > 0 && len(s.cp.PendingOverview) == 0 && !s.sourceSurveyPending()
	action := "Resolve listed critical questions; optional gaps may be documented as limitations."
	if len(pending) > 0 {
		action = "Record visible facts for pending frames first; do not reload or request new pixels."
	} else if canCompose {
		action = "Main evidence is ready. Begin/save the full-video article now; inspect only a concrete remaining gap."
	}
	if s.cp.DraftIssue != nil && s.cp.LastCandidate["tool"] == "begin_article" {
		action = "Repair the retained plan with repair_article_plan using rejected_candidate.candidate_id and expected_hash. Fix all listed fields before writing chapter bodies."
	}
	if s.forceDraftSubmit {
		action = "Call submit_article_draft with the latest version now."
	}
	if repair := s.requiredRepairTool(); repair != "" {
		action = "Use the candidate fields already returned in context and call " + repair + " now. Do not read the same fields again. Preserve supported content and fix the listed paths."
	}
	reviewIssue := s.pendingEvidenceReview()
	if reviewIssue != nil {
		action = "Use evidence_review_issues.affected_section_ids (actual IDs, not array offsets) to read_article_sections, then save_article_sections with the latest version and only those corrected chapters. Preserve other chapters. If patching, copy the exact draft_path: blocks use numeric array offsets, never block IDs. Do not rebuild the plan. The unresolved review persists until a revised candidate passes fresh source review."
		if s.usesBoundedReview() {
			action += " read_article_sections returns at most three chapters per page; keep the same section_ids and copy next_offset until it is absent, then repair the affected chapters using the latest version. Unknown IDs are errors: copy valid_section_ids instead of inventing section numbers."
		}
		action += " Delete unnecessary decorative or uncertain visual details instead of replacing them with new unverified assertions. Keep the source's actual conditions and useful figures; do not add prose merely to maintain length."
		action += " If an issue names save_field=title, include the corrected section title in save_article_sections; patch only fields that provide a draft_path."
		action += " A heading in the same reviewer-named chapter may also be corrected consistently with its listed body issue."
		if s.turn != nil {
			action = "Use evidence_review_issues to correct the listed fields of this answer, preserving supported content and real source references. expected_type/current_value describe the actual field; a text string is not an inline array. Call submit_answer with the corrected complete answer; do not use article tools or draft-field patches. A fresh review must pass before publication."
		}
		if len(pending) > 0 {
			action = "Record visible facts for pending_frame_ids first. " + action
		}
	}
	out := map[string]any{"pending_frame_ids": pending, "critical_question_ids": blockers, "comparison_frame_headroom": room, "window_max_ms": 60000, "inspect_calls_per_response": 1, "can_compose": canCompose, "optional_inspections_remaining": optional, "model_calls_remaining": max(0, s.profile.MaxIterations-s.cp.Calls), "final_calls_reserved": reserved, "consecutive_no_progress": s.cp.Progress.Stalled, "rejected_candidate": s.cp.LastCandidate, "next_action": action}
	if s.boundedSourceContext() {
		out["overview_frames_waiting_delivery"] = len(s.cp.PendingOverview)
		if s.sourceSurveyPending() {
			out["next_action"] = "Read the entire delivered current_chunk, then record_source_notes. Continue every chunk before planning."
		} else if len(s.cp.PendingOverview) > 0 {
			out["next_action"] = "Record the delivered overview frames. The next batch arrives automatically. Release finished comparisons with retain_for_comparison=false if headroom is zero."
		}
	}
	if reviewIssue != nil {
		out["evidence_review_issues"] = reviewIssue
	}
	if s.questionReading() {
		delete(out, "can_compose")
		out["can_submit_answer"] = len(pending) == 0 && len(blockers) == 0
		out["required_scope"] = "current_question"
		out["submission_is_not_approval"] = true
		remaining := max(time.Duration(0), s.profile.ActiveBudget("qa.answer")-time.Duration(s.claim.Run.ActiveMS)*time.Millisecond)
		if deadline, ok := ctx.Deadline(); ok {
			remaining = min(remaining, max(time.Duration(0), time.Until(deadline)))
		}
		if !s.claim.Run.DeadlineAt.IsZero() {
			remaining = min(remaining, max(time.Duration(0), time.Until(s.claim.Run.DeadlineAt)))
		}
		stats, err := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
		if err != nil {
			return nil, err
		}
		reserve := max(time.Minute, 2*time.Duration(stats.ResponseMS/int64(max(1, stats.Submissions)))*time.Millisecond)
		if s.cp.QuestionTiming != nil && s.cp.QuestionTiming.Valid() {
			reserve = max(reserve, time.Duration(s.cp.QuestionTiming.ReviewCallSeconds)*time.Second)
		}
		out["remaining_active_ms"] = remaining.Milliseconds()
		out["submission_and_review_time_reserve_ms"] = reserve.Milliseconds()
		if len(pending) == 0 && len(blockers) == 0 && reviewIssue == nil && s.requiredRepairTool() == "" {
			out["next_action"] = "Read only the original evidence needed for the current question, then submit a concise complete answer with representative citations. There is no full-video or article-composition prerequisite. If the required fact remains unavailable, submit an honest insufficient_evidence answer rather than researching unrelated ranges. Submission still requires source and semantic validation."
			if remaining <= reserve {
				out["next_action"] = "The remaining frozen active time is reserved for submission and independent validation. Stop optional reading and submit the focused answer now using delivered evidence; preserve any essential unknown as insufficient_evidence rather than inventing facts. Submission never bypasses source/semantic review."
			}
		}
	}
	if s.policy().Paced && s.turn == nil {
		stats, err := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
		if err != nil {
			return nil, err
		}
		wrap := s.wrapUp(stats)
		out["model_execution"] = stats
		out["wrap_up"] = wrap
		if wrap && canCompose && s.cp.DraftIssue == nil && reviewIssue == nil && !s.forceDraftSubmit {
			out["next_action"] = "Investigation has used several model calls or substantial waiting time. Stop optional investigation now; begin/finish the full-video article, preserve noncritical unknowns as limitations. Never bypass critical evidence or final validation."
		}
		gaps := []domain.PlanItem{}
		for _, raw := range questions {
			var q struct {
				ID       string
				Question string
				Critical bool
				Status   string
			}
			if json.Unmarshal(raw, &q) != nil || q.Status == "supported" {
				continue
			}
			status := "active"
			if !q.Critical && wrap {
				status = "deferred"
			}
			gaps = append(gaps, domain.PlanItem{ID: "question:" + q.ID, Title: publicTitle(q.Question, "核对尚未确认的问题"), Status: status})
			if len(gaps) == 8 {
				break
			}
		}
		out["open_questions"] = gaps
	}
	if s.usesBoundedReview() && s.cp.Progress.Recovery != nil {
		r := s.cp.Progress.Recovery
		names := make([]string, 0, len(s.tools))
		for name := range s.tools {
			names = append(names, name)
		}
		slices.Sort(names)
		next := fmt.Sprintf("Repeated %s did not advance the current saved state: %s. Use the returned current values, valid IDs and latest draft version. Available tools: %s. %s", r.Tool, r.Reason, strings.Join(names, ", "), out["next_action"])
		out["loop_recovery"] = map[string]any{"tool": r.Tool, "reason": r.Reason, "attempts": r.Attempts, "next_action": next}
		out["next_action"] = next
	}
	return out, nil
}

func (s *session) progressFingerprint(ctx context.Context) (string, error) {
	obs, err := s.observations(ctx)
	if err != nil {
		return "", err
	}
	// Duplicate observations, network bytes and public activity titles are not
	// new evidence. Keep the original facts, not the number of rows written.
	facts := map[string]bool{}
	for _, o := range obs {
		o.QuestionID = ""
		o.Question = ""
		o.NextAction = ""
		facts[store.Hash(o)] = true
	}
	d, err := s.engine.Store.ArticleDraft(ctx, s.claim.Run.ID)
	if err != nil {
		return "", err
	}
	var content any = d.Data
	if len(d.Data) > 0 {
		var draft articleDraft
		if err = json.Unmarshal(d.Data, &draft); err != nil {
			return "", err
		}
		draft.BatchReceipt = nil
		content = draft
	}
	state := map[string]any{"facts": facts, "draft": content, "frames": s.cp.CitationFrames}
	if s.boundedSourceContext() {
		state["source_notes"] = s.cp.SourceNotes
	}
	return store.Hash(state), nil
}

func (s *session) toolFeedback(ctx context.Context, name, args, before string, result *schema.ToolResult, callErr error) error {
	key := "tool-feedback:" + callID(ctx)
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key)
	if err == nil && op.Status == "completed" {
		var saved struct {
			Progress progressState `json:"progress"`
		}
		if err = json.Unmarshal(op.Result, &saved); err != nil {
			return err
		}
		if saved.Progress.Total > s.cp.Progress.Total {
			s.cp.Progress = saved.Progress
		}
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	after, err := s.progressFingerprint(ctx)
	if err != nil {
		return err
	}
	var input map[string]any
	_ = json.Unmarshal([]byte(args), &input)
	delete(input, "activity_title")
	signature := store.Hash(map[string]any{"tool": name, "arguments": input, "raw_invalid": func() string {
		if input == nil {
			return args
		}
		return ""
	}()})
	if s.cp.Progress.Seen == nil {
		s.cp.Progress.Seen = map[string]int{}
	}
	s.cp.Progress.Seen[signature]++
	s.cp.Progress.Total++
	rejected := callErr != nil
	reason := toolRejection(result, callErr)
	if result != nil {
		for _, part := range result.Parts {
			var v map[string]any
			if part.Type == schema.ToolPartTypeText && json.Unmarshal([]byte(part.Text), &v) == nil && v["error"] != nil {
				rejected = true
			}
		}
	}
	progress := !rejected && before != after
	if s.usesBoundedReview() {
		// A->B->A is not new work. Ignore write/version receipts and retain the
		// meaningful state history across calls and worker takeovers.
		progress = progress && s.cp.Progress.Seen["state:"+after] == 0
		s.cp.Progress.Seen["state:"+before]++
		s.cp.Progress.Seen["state:"+after]++
	}
	if !rejected && result != nil && (name == "read_source" || name == "find_evidence" || name == "read_reference") {
		// Count new usable content, not a new spelling of a query that returns
		// the same candidates. Empty reads never reset the stall guard.
		for _, part := range result.Parts {
			var v map[string]any
			if json.Unmarshal([]byte(part.Text), &v) == nil {
				for _, field := range []string{"segments", "candidates", "body"} {
					useful := false
					switch x := v[field].(type) {
					case []any:
						useful = len(x) > 0
					case string:
						useful = x != ""
					}
					if useful {
						key := "read:" + name + ":" + store.Hash(v[field])
						progress = progress || s.cp.Progress.Seen[key] == 0
						s.cp.Progress.Seen[key]++
					}
				}
			}
		}
	}
	if s.usesBoundedReview() && !rejected && result != nil && name == "read_article_sections" {
		// A legitimate repair can need more than six pages. Count each newly
		// delivered chapter once, never offsets, versions or a changed grouping.
		for _, part := range result.Parts {
			var body struct {
				Sections []json.RawMessage `json:"sections"`
			}
			if part.Type == schema.ToolPartTypeText && json.Unmarshal([]byte(part.Text), &body) == nil {
				for _, raw := range body.Sections {
					var section domain.Section
					if json.Unmarshal(raw, &section) == nil && section.ID != "" && len(section.Blocks) > 0 {
						key := "read:read_article_sections:" + store.Hash(section)
						progress = progress || s.cp.Progress.Seen[key] == 0
						s.cp.Progress.Seen[key]++
					}
				}
			}
		}
	}
	countStall := true
	if s.boundedSourceContext() && name == "record_source_notes" {
		// One model response may contain several tool calls, but it has not
		// received any of their feedback yet. Charge at most one correction
		// turn, and never charge it after that response saved a new card.
		turnKey := fmt.Sprintf("source-note-turn:%d", s.cp.Calls)
		countStall = s.cp.Progress.Seen[turnKey] == 0
		if progress {
			s.cp.Progress.Seen[turnKey] = 2
		} else if countStall {
			s.cp.Progress.Seen[turnKey] = 1
		}
	}
	if progress {
		s.cp.Progress.Stalled = 0
		if s.usesBoundedReview() {
			s.cp.Progress.Recovery = nil
			s.cp.Progress.Epoch++
		}
	} else if countStall {
		s.cp.Progress.Stalled++
		if s.usesBoundedReview() {
			loop := store.Hash(map[string]any{"action": signature, "state": after, "reason": reason, "epoch": s.cp.Progress.Epoch})
			key := "loop:" + loop
			s.cp.Progress.Seen[key]++
			if s.cp.Progress.Seen[key] >= 2 {
				// A->B->A failures must retain A's original correction turn.
				// Zero means no nudge, so persist the call number plus one.
				nudgeKey := "nudge:" + loop
				if s.cp.Progress.Seen[nudgeKey] == 0 {
					s.cp.Progress.Seen[nudgeKey] = s.cp.Calls + 1
				}
				// The ordinary stall limit gets at most one correction turn per
				// progress epoch, even when later failures have brand-new signatures.
				epochNudge := fmt.Sprintf("nudge-epoch:%d", s.cp.Progress.Epoch)
				if s.cp.Progress.Seen[epochNudge] == 0 {
					s.cp.Progress.Seen[epochNudge] = s.cp.Calls + 1
				}
				nudgeCall := min(s.cp.Progress.Seen[nudgeKey], s.cp.Progress.Seen[epochNudge]) - 1
				if reason == "" {
					reason = "The action returned no new evidence, chapter content or saved state."
				}
				// Once a correction has failed, a different ineffective action
				// cannot hide it. Only actual information/content progress clears it.
				previous := s.cp.Progress.Recovery
				if previous == nil || previous.Attempts < 3 || s.cp.Calls <= previous.NudgeCall {
					s.cp.Progress.Recovery = &loopRecovery{Signature: loop, Tool: name, Reason: reason, Attempts: s.cp.Progress.Seen[key], NudgeCall: nudgeCall}
				}
			}
		}
	}
	if _, err = s.engine.Store.BeginOperation(ctx, s.claim, key, "tool_feedback", map[string]string{"tool": name, "arguments_hash": store.Hash(args)}, false); err != nil {
		return err
	}
	if err = s.engine.Store.FinishOperation(ctx, s.claim, key, map[string]any{"tool": name, "call_id": callID(ctx), "rejected": rejected, "rejection_reason": reason, "effective_progress": progress, "signature": signature, "progress": s.cp.Progress}, "", nil); err != nil {
		return err
	}
	return s.engine.Store.Checkpoint(ctx, s.claim, s.cp)
}

func toolRejection(result *schema.ToolResult, err error) string {
	if result != nil {
		for _, part := range result.Parts {
			var v struct {
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal([]byte(part.Text), &v) == nil && len(v.Error) > 0 && string(v.Error) != "null" {
				var message string
				if json.Unmarshal(v.Error, &message) != nil {
					message = string(v.Error)
				}
				return string([]rune(message)[:min(512, len([]rune(message)))])
			}
		}
	}
	var p *domain.Problem
	if errors.As(err, &p) {
		return string([]rune(p.Detail)[:min(512, len([]rune(p.Detail)))])
	}
	if err != nil {
		return "operation_failed"
	}
	return ""
}

func (s *session) unknownTool(ctx context.Context, name, args string) (string, error) {
	if callID(ctx) == "" {
		ctx = context.WithValue(ctx, callIDKey{}, compose.GetToolCallID(ctx))
	}
	before, err := s.progressFingerprint(ctx)
	if err != nil {
		return "", err
	}
	result, _ := toolError("未知工具。请只使用当前提供的工具名称；本次调用没有执行任何工作。")
	if s.policy().Enabled {
		if err = s.toolFeedback(ctx, name, args, before, result, nil); err != nil {
			return "", err
		}
	}
	return result.Parts[0].Text, nil
}

const harnessInstructions = `
readiness 是服务器持久状态：待观察图、关键阻塞、比较余量、可选取证和剩余回合。无待观察图且主要结论有支持时尽快开始成文，短片通常只需概览和明确缺口。可选问题的工具额度耗尽时用limitations诚实保留；必要取证仍须支持主要结论。每个回复最多一个inspect，多个inspect会整组拒绝且不产生图片。不要通过换activity_title重复同一请求。
预算为调查、成文、局部修复和最终提交分别预留。保留至少4个模型回合完成写作/修复/提交；不得用心跳、重复观察、重复候选或无效工具消耗预算。已保存草稿可用patch_article_draft只替换错误字段；正式问答候选可用repair_candidate修复指定字段。无论如何修复，只有全文校验通过才会发布。`
