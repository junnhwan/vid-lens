package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type reviewBatchOutcome struct {
	State     string            `json:"state"`
	Mode      string            `json:"mode"`
	RequestID string            `json:"request_id"`
	UnitIDs   []string          `json:"unit_ids"`
	Report    *reviewUnitReport `json:"report,omitempty"`
}

// Own planning, explicit coverage and resumable execution behind one entry.
// Writer correction accounting stays in reviewContent after this returns.
func (s *session) reviewUnits(ctx context.Context, hash string, candidate any, hints map[string]any, catalog map[string]domain.Citation) ([]map[string]any, error) {
	units, err := candidateReviewUnits(candidate, catalog)
	if err != nil {
		return nil, err
	}
	tasks, err := planReviewTasks(units, s.profile.MaxOutputTokens)
	if err != nil {
		return nil, domain.Err(422, "REVIEW_INPUT_TOO_LARGE", "单个完整审核单元超出可交付的证据容量；草稿已保留，未拆散比较所需的证据。")
	}
	byID := map[string]reviewUnit{}
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	issues := []map[string]any{}
	for _, task := range tasks {
		results, err := s.executeReviewTask(ctx, hash, candidate, hints, catalog, task)
		if err != nil {
			return nil, err
		}
		for _, result := range results {
			if result.Verdict == "not_assessed" {
				return nil, domain.Err(502, "REVIEW_COVERAGE_INCOMPLETE", "独立复核未完成所有必需单元；草稿和已完成审核记录已保留。")
			}
			for _, issue := range result.Issues {
				issues = append(issues, map[string]any{"code": "evidence_not_supported", "path": byID[result.UnitID].Path, "reason": issue.Reason, "claim": issue.Claim, "citation_ids": issue.CitationIDs, "review_unit_id": result.UnitID, "verdict": result.Verdict})
			}
		}
		if task.Mode == "source_text" && len(issues) > 0 {
			break
		}
	}
	return issues, nil
}

func (s *session) executeReviewTask(ctx context.Context, hash string, candidate any, hints map[string]any, catalog map[string]domain.Citation, task reviewTask) ([]reviewUnitResult, error) {
	ids := []string{}
	for _, unit := range task.Units {
		ids = append(ids, unit.ID)
	}
	key := "review-units:" + store.Hash(map[string]any{"candidate_hash": hash, "mode": task.Mode, "unit_ids": ids})
	id := "r_" + strings.TrimPrefix(key, "review-units:")[:24]
	op, err := s.engine.Store.BeginOperation(ctx, s.claim, key, "review_batch", map[string]any{"request_id": id, "unit_ids": ids, "mode": task.Mode, "protocol": boundedReviewProtocol}, false)
	if err != nil {
		return nil, err
	}
	if op.Status == "completed" {
		var saved reviewBatchOutcome
		if err = json.Unmarshal(op.Result, &saved); err != nil {
			return nil, err
		}
		if saved.RequestID != id || saved.Mode != task.Mode || !slices.Equal(saved.UnitIDs, ids) {
			return nil, domain.Err(422, "INTERNAL_FAILURE", "审核操作身份不兼容。")
		}
		switch saved.State {
		case "complete":
			if saved.Report == nil {
				return nil, domain.Err(422, "INTERNAL_FAILURE", "审核操作缺少覆盖记录。")
			}
			if err = validateReviewUnits(*saved.Report, id, task, catalog); err != nil {
				return nil, err
			}
			return saved.Report.Results, nil
		case "split":
			return s.executeReviewChildren(ctx, hash, candidate, hints, catalog, task)
		case "exhausted":
			return nil, reviewCapacityExhausted()
		default:
			return nil, domain.Err(422, "INTERNAL_FAILURE", "审核操作状态不兼容。")
		}
	}
	input, err := s.reviewUnitInput(ctx, id, candidate, hints, catalog, task)
	if err != nil {
		return nil, err
	}
	info, err := s.tools[reviewUnitsTool].Info(ctx)
	if err != nil {
		return nil, err
	}
	step := reviewStep{RequestID: id, Units: len(task.Units), WorkTokens: estimateTextTokens(domain.JSON(task.Units))}
	formatErrors := 0
	for attempt := 0; ; attempt++ {
		slot, err := s.reserveReviewCall(ctx, key+":"+strconv.Itoa(attempt))
		if err != nil {
			return nil, err
		}
		reviewer := *s
		reviewer.reviewing, reviewer.forceDraftSubmit, reviewer.modelMessages = true, false, nil
		reviewer.reviewStep = &step
		reviewer.cp.Calls = slot
		response, err := (&meteredModel{session: &reviewer, tools: []*schema.ToolInfo{info}}).Generate(ctx, input, model.WithToolChoice(schema.ToolChoiceForced, info.Name))
		if err != nil {
			var work *domain.WorkError
			if !errors.As(err, &work) || work.Code != "MODEL_OUTPUT_TRUNCATED" {
				return nil, err
			}
			if len(task.Units) > 1 {
				out := reviewBatchOutcome{State: "split", Mode: task.Mode, RequestID: id, UnitIDs: ids}
				if err = s.engine.Store.FinishOperation(ctx, s.claim, key, out, "", nil); err != nil {
					return nil, err
				}
				return s.executeReviewChildren(ctx, hash, candidate, hints, catalog, task)
			}
			frozen, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "input:model:"+strconv.Itoa(slot))
			if err != nil {
				return nil, err
			}
			var previous frozenModelInput
			if err = json.Unmarshal(frozen.Result, &previous); err != nil {
				return nil, err
			}
			next := min(previous.Budget.AvailableOutputTokens, previous.Budget.OutputTokens*2)
			if next <= previous.Budget.OutputTokens {
				out := reviewBatchOutcome{State: "exhausted", Mode: task.Mode, RequestID: id, UnitIDs: ids}
				if err = s.engine.Store.FinishOperation(ctx, s.claim, key, out, "", nil); err != nil {
					return nil, err
				}
				return nil, reviewCapacityExhausted()
			}
			step.OutputTokens = next
			continue
		}
		report, err := parseReviewUnits(response, id, task, catalog)
		if err != nil {
			formatErrors++
			if formatErrors >= 2 {
				return nil, domain.Err(502, "REVIEW_RESULT_INVALID", "独立复核未返回完整合法的逐单元结论；草稿已保留，未消耗作者修订额度。")
			}
			message := err.Error()
			if len(message) > 240 {
				message = message[:240]
			}
			input = append(input, schema.UserMessage(string(domain.JSON(map[string]any{"review_format_error": message, "request_id": id, "required_unit_ids": ids, "instruction": "Return exactly one result for each required unit ID using the supplied tool schema."}))))
			continue
		}
		out := reviewBatchOutcome{State: "complete", Mode: task.Mode, RequestID: id, UnitIDs: ids, Report: &report}
		if err = s.engine.Store.FinishOperation(ctx, s.claim, key, out, "", nil); err != nil {
			return nil, err
		}
		return report.Results, nil
	}
}

func (s *session) executeReviewChildren(ctx context.Context, hash string, candidate any, hints map[string]any, catalog map[string]domain.Citation, task reviewTask) ([]reviewUnitResult, error) {
	if len(task.Units) < 2 {
		return nil, domain.Err(422, "INTERNAL_FAILURE", "审核拆分缺少可分离的单元。")
	}
	results := []reviewUnitResult{}
	middle := len(task.Units) / 2
	for _, units := range [][]reviewUnit{task.Units[:middle], task.Units[middle:]} {
		part, err := s.executeReviewTask(ctx, hash, candidate, hints, catalog, reviewTask{Mode: task.Mode, Units: units})
		if err != nil {
			return nil, err
		}
		results = append(results, part...)
	}
	return results, nil
}

func reviewCapacityExhausted() error {
	return domain.Err(422, "REVIEW_BUDGET_EXHAUSTED", "独立复核在当前模型及上下文容量内仍未完成；草稿、已完成单元和失败记录已保留。")
}

func (s *session) reviewUnitInput(ctx context.Context, id string, candidate any, hints map[string]any, catalog map[string]domain.Citation, task reviewTask) ([]*schema.Message, error) {
	ids := reviewTaskFrames(task)
	source := map[string]any{"snapshot_id": s.snapshot.ID, "source": s.snapshot.Source, "duration_ms": s.snapshot.DurationMS, "transcript": s.transcript}
	if s.turn != nil {
		source["question"] = s.turn.Question
	}
	// Context is deliberately complete and unchanged across smaller tasks. A
	// local verdict may depend on a premise in another chapter or source segment.
	payload := map[string]any{"request_id": id, "review_protocol": boundedReviewProtocol, "review_mode": task.Mode, "units": task.Units, "candidate_context": candidate, "citation_catalog": hints, "batch_frame_ids": ids}
	input := []*schema.Message{schema.SystemMessage(reviewUnitInstructions), schema.UserMessage(string(domain.JSON(source))), schema.UserMessage(string(domain.JSON(payload)))}
	parts, err := s.imageInput(ctx, ids, "review-units")
	if err != nil {
		return nil, err
	}
	for i, p := range parts {
		frame, ok := p.Extra["frame_id"].(string)
		if !ok {
			continue
		}
		if !slices.Contains(ids, frame) {
			return nil, &candidateError{err: missingEvidence("复核画面已重物化，需要更新原帧引用。")}
		}
		if i > 0 && parts[i-1].Type == schema.ChatMessagePartTypeText {
			aliases := []string{}
			for alias, c := range catalog {
				if c.Kind == "video_frame" && c.FrameID == frame {
					aliases = append(aliases, alias)
				}
			}
			slices.Sort(aliases)
			parts[i-1].Text += fmt.Sprint("\nsource_citation_ids=", strings.Join(aliases, ","))
		}
	}
	if len(parts) > 0 {
		input = append(input, &schema.Message{Role: schema.User, UserInputMultiContent: parts})
	}
	return s.projectReferences(ctx, input)
}
