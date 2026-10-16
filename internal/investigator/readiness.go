package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

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
}

func (s *session) repairLimit() int {
	if s.cp.FinalizationFailure != nil && s.cp.FinalizationFailure.Code == "MODEL_RESULT_NOT_SUBMITTED" {
		return 1
	}
	if s.cp.HarnessRevision > 0 && s.spec.DetailPolicy != nil {
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
	canCompose := s.turn == nil && len(pending) == 0 && len(blockers) == 0 && len(s.cp.OverviewIDs) > 0
	action := "Resolve listed critical questions; optional gaps may be documented as limitations."
	if len(pending) > 0 {
		action = "Record visible facts for pending frames first; do not reload or request new pixels."
	} else if canCompose {
		action = "Main evidence is ready. Begin/save the full-video article now; inspect only a concrete remaining gap."
	}
	if s.forceDraftSubmit {
		action = "Call submit_article_draft with the latest version now."
	}
	return map[string]any{"pending_frame_ids": pending, "critical_question_ids": blockers, "comparison_frame_headroom": room, "window_max_ms": 60000, "inspect_calls_per_response": 1, "can_compose": canCompose, "optional_inspections_remaining": optional, "model_calls_remaining": max(0, s.profile.MaxIterations-s.cp.Calls), "final_calls_reserved": reserved, "consecutive_no_progress": s.cp.Progress.Stalled, "rejected_candidate": s.cp.LastCandidate, "next_action": action}, nil
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
	return store.Hash(map[string]any{"facts": facts, "draft": d.Data, "frames": s.cp.CitationFrames}), nil
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
	if progress {
		s.cp.Progress.Stalled = 0
	} else {
		s.cp.Progress.Stalled++
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
				Error string `json:"error"`
			}
			if json.Unmarshal([]byte(part.Text), &v) == nil && v.Error != "" {
				return string([]rune(v.Error)[:min(512, len([]rune(v.Error)))])
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
	if s.cp.HarnessRevision > 0 {
		if err = s.toolFeedback(ctx, name, args, before, result, nil); err != nil {
			return "", err
		}
	}
	return result.Parts[0].Text, nil
}

const harnessInstructions = `
readiness 是服务器持久状态：待观察图、关键阻塞、比较余量、可选取证和剩余回合。无待观察图且主要结论有支持时尽快开始成文，短片通常只需概览和明确缺口。可选问题的工具额度耗尽时用limitations诚实保留；必要取证仍须支持主要结论。每个回复最多一个inspect，多个inspect会整组拒绝且不产生图片。不要通过换activity_title重复同一请求。
预算为调查、成文、局部修复和最终提交分别预留。保留至少4个模型回合完成写作/修复/提交；不得用心跳、重复观察、重复候选或无效工具消耗预算。已保存草稿可用patch_article_draft只替换错误字段；正式问答候选可用repair_candidate修复指定字段。无论如何修复，只有全文校验通过才会发布。`
