package investigator

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func (s *session) investigationLimit() (int, int64) {
	switch s.spec.DetailLevel {
	case "concise":
		return 4, 120000
	case "detailed":
		return 10, 480000
	default:
		return 6, 240000
	}
}

func (s *session) wrapUp(stats store.ModelExecutionStats) bool {
	calls, millis := s.investigationLimit()
	return s.cp.Calls >= calls || stats.ResponseMS >= millis || s.profile.MaxIterations-s.cp.Calls <= 8
}

// Plans are projections of accepted evidence and chapters, never inferred from
// a streamed tool name or a model's claim that work has finished.
func (s *session) saveExecutionPlan(ctx context.Context, ready map[string]any) error {
	version, draft, err := s.loadDraft(ctx)
	if err != nil {
		return err
	}
	canCompose, _ := ready["can_compose"].(bool)
	wrap, _ := ready["wrap_up"].(bool)
	plan := &domain.ExecutionPlan{Items: []domain.PlanItem{
		{ID: "overview", Title: "通读全片文字与概览画面", Status: "active"},
		{ID: "evidence", Title: "核对影响主要结论的画面", Status: "pending"},
		{ID: "outline", Title: "确定覆盖全片的文章提纲", Status: "pending"},
		{ID: "write", Title: "分批写作并保存章节", Status: "pending"},
		{ID: "validate", Title: "核对全文、引用和真实配图", Status: "pending"},
		{ID: "publish", Title: "发布可独立阅读的图文文章", Status: "pending"},
	}, WrapUp: wrap}
	observed, err := s.observations(ctx)
	if err != nil {
		return err
	}
	if len(observed) > 0 {
		plan.Items[0].Status = "done"
		plan.Items[1].Status = "active"
	}
	if canCompose {
		plan.Items[1].Status = "done"
		plan.Items[2].Status = "active"
	}
	if version > 0 {
		plan.Items[2].Status = "done"
		plan.Items[3].Status = "active"
		plan.Items[3].Detail = fmt.Sprintf("已保存 %d / %d 章", len(draft.Sections), len(draft.Plan))
		next := true
		for _, chapter := range draft.Plan {
			status := "pending"
			if _, saved := draft.Sections[chapter.ID]; saved {
				status = "done"
			} else if next {
				status = "active"
				next = false
			}
			plan.Sections = append(plan.Sections, domain.PlanItem{ID: chapter.ID, Title: chapter.Title, Status: status})
		}
	}
	if version > 0 && len(draft.Sections) == len(draft.Plan) {
		plan.Items[3].Status = "done"
		plan.Items[4].Status = "active"
	}
	if s.cp.DraftIssue != nil {
		plan.Note = "正在修正本次校验指出的问题，已接受的内容继续保留。"
	} else if wrap && version == 0 {
		plan.Note = "已进入收尾：优先完成主要内容，非关键未知将写入文章限制说明。"
	} else if wrap {
		plan.Note = "已进入写作与核对收尾，继续保存剩余章节。"
	}
	// Readiness carries only bounded public questions. Their source records and
	// all earlier versions remain in PostgreSQL; a changed plan does not erase actions.
	if gaps, ok := ready["open_questions"].([]domain.PlanItem); ok {
		plan.Items = append(plan.Items, gaps...)
	}
	hash := store.Hash(plan)
	if hash == s.cp.PlanHash {
		return nil
	}
	_, err = s.engine.Store.Step(ctx, s.claim, domain.Step{ID: "plan_" + s.claim.Run.ID, Kind: "execution_plan", Title: "当前执行计划", Status: "completed", Plan: plan, FrameIDs: []string{}})
	if err == nil {
		s.cp.PlanHash = hash
	}
	return err
}

// Remove schemas irrelevant to the current phase before freezing the call.
// Frozen inputs bypass this projection on replay and retain their exact schema.
func (s *session) phaseTools(in []*schema.ToolInfo) []*schema.ToolInfo {
	if !s.policy().Paced || s.turn != nil || s.forceDraftSubmit {
		return in
	}
	out := []*schema.ToolInfo{}
	repair := s.requiredRepairTool()
	for _, t := range in {
		if repair != "" {
			if t.Name == repair {
				out = append(out, t)
			}
			continue
		}
		keep := false
		switch t.Name {
		case "record_observation", "inspect_video_window", "find_evidence", "read_source":
			keep = true
		case "begin_article":
			keep = !s.cp.Composing
		case "repair_article_plan":
			keep = !s.cp.Composing && s.cp.DraftIssue != nil
		case "read_candidate_fields":
			keep = s.cp.LastCandidate != nil
		case "repair_article_sections":
			keep = s.cp.Composing && s.cp.LastCandidate != nil
		case "save_article_sections", "read_article_sections", "patch_article_draft", "update_article_limitations", "submit_article_draft":
			keep = s.cp.Composing
		}
		if s.authoring() && t.Name == "submit_article_draft" {
			keep = false
		}
		if s.authoring() && t.Name == "patch_article_draft" {
			keep = s.cp.Composing && s.cp.DraftIssue != nil
		}
		if keep {
			out = append(out, t)
		}
	}
	return out
}

// Repeated candidate reads are preparation, not evidence or accepted writing.
// Direct the model to the pending correction before it exhausts the durable
// stall guard; do not reset call, correction or no-progress budgets.
func (s *session) requiredRepairTool() string {
	if !s.policy().Recovery || s.turn != nil || s.cp.DraftIssue == nil || s.cp.Progress.Stalled < 3 {
		return ""
	}
	switch s.cp.LastCandidate["tool"] {
	case "begin_article":
		return "repair_article_plan"
	case "save_article_sections":
		return "repair_article_sections"
	}
	return ""
}

const repairProgressInstructions = `
错误修复：计划被拒时用repair_article_plan，正文被拒时用repair_article_sections，复制readiness.rejected_candidate的candidate_id与expected_hash。read_candidate_fields返回的fields会保留到候选更新；已读过的字段直接用于replace修正，不要重复读取。导图修复可以替换/mind_map或具体已有节点字段：只有一个根且parent_id=null，分组section_id可null，叶节点section_id必须是当前真实章节ID。连续准备却没有保存修正时，下一轮只允许执行修复；原纠错次数、总调用数与无进展上限不会重置。`

func modelCallTitle(call int) string { return "第 " + strconv.Itoa(call) + " 次模型调用" }

const pacedHarnessInstructions = `
服务器执行计划按真实观察、关键问题、提纲、已保存章节和最终校验动态更新；按readiness.next_action推进，不另写冗长计划或私密推理。首次观察尽量一次record_observation涵盖全部overview图片，只保留与文章有关的简洁事实和明确缺口；不要逐帧复述整篇字幕。先记录再决定是否补看，不能把实际没有看到的细节写成事实。
readiness.wrap_up=true表示调查回合或累计模型等待已较多：停止非关键扩展，立即begin_article或继续剩余章节。critical=true仅用于主要结论缺少不可替代证据的具体问题；不能把所有可选小字都设为关键。非关键未知写入limitations，必要时在文章开场说明观察范围，不能用其阻止全片成文。关键问题未解决仍不能发布。
提纲按真正主题合并相邻内容，标准解读不要把每个示例或转场拆成独立章节；key_points简洁且不重复，evidence_ids优先选最能支持要点的少量代表，不枚举每条字幕。一次save_article_sections写最多3章，按当前版本串行保存；无需先输出长自由文本再生成相同工具参数。不要重复提交已有工具结果，不要在最后一章后另开取证循环。`
