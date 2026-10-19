package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type draftRepairContextKey struct{}

const planRepairInstructions = `
文章计划校验反馈中的error是结构化问题集合，issues逐项给出字段路径、实际值与要求；issue_count是总数，issues_truncated表示还有未列出的问题。保留全部有支持的要点，逐章纠正引用数量与真实来源时间，不要只改变JSON排序，也不要重复发原候选。计划尚未保存时，优先用repair_article_plan和readiness.rejected_candidate中的candidate_id、expected_hash替换指定字段；需要回看时用read_candidate_fields。只有计划通过并取得version后才写正文。纠正与最终提交共用有界额度。`

func canonicalArguments(raw string) any {
	var value any
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil || !json.Valid([]byte(raw)) {
		return raw
	}
	return value
}

func (s *session) repairUsage(ctx context.Context) (int, error) {
	kinds := []string{"field_repair", "draft_correction"}
	if s.policy().SectionRepairs {
		kinds = []string{"field_repair"}
	}
	count, err := s.engine.Store.OperationCount(ctx, s.claim.Run.ID, kinds...)
	if err != nil {
		return 0, err
	}
	return count + s.cp.Repairs, nil
}

func (s *session) correctionScope(ctx context.Context) (string, error) {
	version, _, err := s.loadDraft(ctx)
	return fmt.Sprintf("draft:%d", version), err
}

func (s *session) correctionUsage(ctx context.Context) (int, error) {
	if !s.policy().SectionRepairs {
		return s.repairUsage(ctx)
	}
	scope, err := s.correctionScope(ctx)
	if err != nil {
		return 0, err
	}
	count, err := s.engine.Store.OperationCount(ctx, s.claim.Run.ID, "draft_correction:"+scope)
	return count, err
}

func draftMutation(name string) bool {
	return name == "begin_article" || name == "save_article_sections" || name == "update_article_limitations" || name == "repair_article_plan"
}

// Reserve one logical correction before parsing or writing. It survives crashes
// and JSON key reordering; a worker takeover never grants a fresh repair budget.
func (s *session) reserveDraftCorrection(ctx context.Context, name, raw string) (bool, error) {
	if !s.policy().ContentPlan || !draftMutation(name) || ctx.Value(draftRepairContextKey{}) != nil {
		return false, nil
	}
	key := "draft-correction:" + callID(ctx)
	args := map[string]any{"tool": name, "arguments": canonicalArguments(raw)}
	kind := "draft_correction"
	if s.policy().SectionRepairs {
		scope, err := s.correctionScope(ctx)
		if err != nil {
			return false, err
		}
		kind += ":" + scope
	}
	if op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, key); err == nil {
		// A replay may occur after this correction advanced the draft version.
		validKind := op.Kind == kind || s.policy().SectionRepairs && strings.HasPrefix(op.Kind, "draft_correction:draft:")
		if !validKind || op.ArgsHash != store.Hash(args) {
			return false, errors.New("draft correction replay conflict")
		}
		return true, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if s.cp.DraftIssue == nil && name != "repair_article_plan" {
		return false, nil
	}
	used, err := s.correctionUsage(ctx)
	if err != nil {
		return false, err
	}
	if used >= s.repairLimit() {
		return false, s.endDraftCorrection(ctx)
	}
	_, err = s.engine.Store.BeginOperation(ctx, s.claim, key, kind, args, false)
	return err == nil, err
}

func feedbackIssue(message string) map[string]any {
	var issue map[string]any
	if json.Unmarshal([]byte(message), &issue) == nil && issue["code"] != nil {
		return issue
	}
	return map[string]any{"code": "candidate_schema", "detail": message, "repairable": true}
}

func (s *session) structuredDraftFeedback(ctx context.Context, message string) (*schema.ToolResult, error) {
	version, draft, err := s.loadDraft(ctx)
	if err != nil {
		return nil, err
	}
	issue := feedbackIssue(message)
	s.cp.DraftIssue = issue
	used, err := s.correctionUsage(ctx)
	if err != nil {
		return nil, err
	}
	if used >= s.repairLimit() {
		return nil, s.endDraftCorrection(ctx)
	}
	v := s.draftState(version, draft)
	v["error"] = issue
	v["correct_arguments_and_try_again"] = true
	v["repair_attempts_used"] = used
	v["repair_attempts_remaining"] = s.repairLimit() - used
	if version == 0 {
		v["next_action"] = "Repair the retained begin_article candidate with repair_article_plan using its exact candidate_id and expected_hash. Replace only the listed fields; keep all supported facts. No chapter body is saved yet."
	}
	return textResult(v), nil
}

func (s *session) endDraftCorrection(ctx context.Context) error {
	version, draft, err := s.loadDraft(ctx)
	if err != nil {
		return err
	}
	detail := "文章计划仍未通过校验；来源与观察已保留，文章计划尚未建立。"
	if version > 0 {
		detail = fmt.Sprintf("本次文章修订仍未通过校验；已保存%d章，来源与草稿保留。", len(draft.Sections))
		if len(draft.Sections) == 0 {
			detail = "文章计划已保存，正文尚未保存；来源与计划保留。"
		}
	}
	if n, ok := s.cp.DraftIssue["issue_count"]; ok {
		detail += fmt.Sprintf("最近一次仍有%v处字段或来源问题。", n)
	}
	p := &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: detail,
		Meta: map[string]any{"issue": s.cp.DraftIssue, "saved_sections": len(draft.Sections), "draft_version": version}}
	s.cp.FinalizationFailure = p
	s.cp.FinalizationFailed = true
	if err = s.engine.Store.Checkpoint(ctx, s.claim, s.cp); err != nil {
		return err
	}
	return p
}

// Keep structured errors structured. Rejection bookkeeping must not truncate
// the actionable collection into a string and lose the later chapter issues.
func rejectedCandidateIssue(result *schema.ToolResult, err error) map[string]any {
	if result != nil {
		for _, part := range result.Parts {
			var value map[string]json.RawMessage
			if part.Type != schema.ToolPartTypeText || json.Unmarshal([]byte(part.Text), &value) != nil {
				continue
			}
			if raw := value["error"]; len(raw) > 0 && string(raw) != "null" {
				var issue map[string]any
				if json.Unmarshal(raw, &issue) == nil && issue != nil {
					return issue
				}
				var message string
				if json.Unmarshal(raw, &message) == nil && message != "" {
					return feedbackIssue(message)
				}
			}
		}
	}
	var p *domain.Problem
	if errors.As(err, &p) {
		return candidateIssue(p)
	}
	return nil
}

func planRepairAllowed(path []string) bool {
	if len(path) == 1 {
		return path[0] == "title" || path[0] == "limitations"
	}
	if len(path) < 3 || path[0] != "sections" {
		return false
	}
	if len(path) == 3 {
		return path[2] == "evidence_ids" || path[2] == "start_ms" || path[2] == "end_ms" || path[2] == "title"
	}
	return len(path) == 4 && (path[2] == "key_points" || path[2] == "evidence_ids")
}

func (s *session) planRepairTool(add func(tool.EnhancedInvokableTool, error) error) error {
	description := "Repair a retained rejected begin_article plan before any draft exists. Require candidate_id and exact expected_hash. Use 1-16 replace patches for /sections/<index>/evidence_ids, an existing evidence_ids/<index>, start_ms, end_ms, title or an existing key_points/<index>, /title or /limitations. Preserve the complete chapter plan and supported points. Cannot alter source/model identity or publish. All source and full-video checks run again before saving version 1."
	if s.policy().ReviewRecovery {
		description += " If current feedback flags point_count_exceeded, replace that exact /sections/<index>/key_points array by merging its supported facts into 1-12 points. No other whole key_points array is editable."
	}
	return add(utils.InferEnhancedTool("repair_article_plan", description, func(ctx context.Context, in candidatePatchInput) (*schema.ToolResult, error) {
		op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate-attempt:"+in.CandidateID+":invalid")
		if errors.Is(err, pgx.ErrNoRows) {
			return toolError("候选不存在或不属于本次执行。")
		}
		if err != nil {
			return nil, err
		}
		var parent struct {
			Tool      string `json:"tool"`
			Arguments string `json:"arguments"`
		}
		if err = json.Unmarshal(op.Result, &parent); err != nil {
			return nil, err
		}
		if parent.Tool != "begin_article" || store.Hash(parent.Arguments) != in.ExpectedHash {
			return toolError("只能纠正本次执行保留的文章计划，且候选哈希必须完全一致。")
		}
		if in.ReplacementJSON != nil {
			return toolError("计划纠正仅允许替换已有字段，不能整篇替换或删除章节与要点。")
		}
		parentJSON := parent.Arguments
		if s.authoring() {
			parentJSON = normalizeAuthoringContainers("begin_article", normalizeAuthoringJSON(parentJSON))
		}
		raw, err := replaceFields([]byte(parentJSON), in.Patches, func(path []string) bool {
			return planRepairAllowed(path) || s.planCountRepairAllowed(path) || s.policy().ConceptMaps && len(path) > 0 && path[0] == "mind_map" || s.authoring() && len(path) == 3 && (path[0] == "sections" && path[2] == "id" || path[0] == "limitations" && (path[2] == "code" || path[2] == "message" || path[2] == "section_ids" || path[2] == "citation_ids"))
		})
		if err != nil {
			return toolError(err.Error())
		}
		// The ordinary checked begin tool records the child candidate, validates
		// every chapter/reference and performs the first-version CAS atomically.
		return s.tools["begin_article"].InvokableRun(context.WithValue(ctx, draftRepairContextKey{}, true), &schema.ToolArgument{Text: string(raw)})
	}))
}
