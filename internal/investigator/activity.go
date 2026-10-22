package investigator

import (
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/schema"
)

func (s *session) modelActivity() (string, string) {
	if s.articleCall != nil {
		if s.articleCall.Mode == "review" {
			return "正在综合核对主题、原文与配图", "validate"
		}
		return "正在阅读证据并撰写文章", "compose"
	}
	if s.workflowStep != nil {
		switch s.workflowStep.Phase {
		case "plan":
			return "正在通读全片并组织文章结构", "compose"
		case "write", "revision":
			return "正在撰写与核对文章章节", "compose"
		case "review", "recheck":
			return "正在核对原文、引用和真实配图", "validate"
		case "answer":
			return "正在依据视频原文与画面回答", "answer"
		}
	}
	if s.reviewing {
		return "正在独立复核事实、引用和实际配图", "validate"
	}
	if s.turn != nil {
		return "正在分析问题与视频依据", "answer"
	}
	if s.cp.Composing && s.compositionReady {
		return "正在撰写与核对文章章节", "compose"
	}
	if s.cp.DraftIssue != nil || s.compositionReady {
		return "正在组织与校对文章结构", "compose"
	}
	for i := len(s.cp.Messages) - 1; i >= 0; i-- {
		m := s.cp.Messages[i]
		if m.Role == schema.Tool && m.ToolName == "inspect_video_window" {
			return "正在分析刚取得的视频画面", "investigate"
		}
		if m.Role == schema.Tool && m.ToolName == "record_observation" {
			return "正在梳理已记录的事实与内容缺口", "investigate"
		}
	}
	return "正在理解全片文字与画面", "investigate"
}

// This describes an observed response, never provider reasoning or an assumed
// business outcome. Actual chapter saving/validation has its own durable step.
func responseActivity(result *schema.Message) (string, string) {
	if !validModelToolArguments(result) {
		return "模型工具参数需要纠正", "响应已结束，但工具参数JSON不完整或无效；本次尚未执行动作，接下来简化并纠正参数。"
	}
	if len(result.ToolCalls) == 0 {
		return "模型响应已接收", "响应已接收，仍需通过正式提交与内容校验。"
	}
	names := []string{}
	for _, c := range result.ToolCalls {
		names = append(names, c.Function.Name)
	}
	if len(names) > 1 {
		return "模型处理动作已接收", "接下来执行所需的来源获取或记录；内容尚待校验。"
	}
	var metadata struct {
		ActivityTitle string `json:"activity_title"`
	}
	if json.Unmarshal([]byte(result.ToolCalls[0].Function.Arguments), &metadata) == nil && strings.TrimSpace(metadata.ActivityTitle) != "" {
		return "准备：" + publicTitle(metadata.ActivityTitle, "执行下一步处理"), "已收到动作请求，接下来执行并校验；内容尚未确认。"
	}
	switch names[0] {
	case "record_observation":
		return "已收到视频画面观察", "接下来保存事实与尚未确认的问题。"
	case "inspect_video_window", "read_source", "find_evidence", "read_evidence", "view_frames":
		var in struct {
			ActivityTitle string `json:"activity_title"`
		}
		if json.Unmarshal([]byte(result.ToolCalls[0].Function.Arguments), &in) == nil && strings.TrimSpace(in.ActivityTitle) != "" {
			return "模型来源核对请求已接收", "下一步执行：" + publicTitle(in.ActivityTitle, "核对所需来源") + "。"
		}
		return "已确定需要核对的来源", "接下来获取所需原文或真实画面。"
	case "begin_article", "repair_article_plan":
		return "已收到文章结构", "接下来校验覆盖范围并保存章节计划。"
	case "save_article_sections", loopSaveTool:
		return "已收到章节草稿", "接下来校验并保存本批章节；其他已保存章节保留。"
	case "read_article_sections", "read_article_state":
		return "正在回读待修正章节", "接下来读取已有草稿，仅修正相关章节。"
	case "update_article_limitations":
		return "已收到限制说明修订", "接下来保存修订，已写章节保持。"
	case "submit_article", "submit_article_draft", loopFinishTool:
		return "已收到文章校验请求", "文章尚未发布，接下来校验完整内容与引用。"
	case "submit_answer":
		return "已收到回答候选", "接下来校验回答与引用，保存后才能正式展示。"
	case "report_evidence_review", reviewUnitsTool, loopReviewTool:
		return "已收到独立证据复核意见", "接下来核对候选身份与复核意见，存在问题时先修正内容。"
	}
	return "已收到来源处理请求", "接下来执行所需动作并校验结果。"
}

func validModelToolArguments(result *schema.Message) bool {
	if result == nil {
		return false
	}
	for _, call := range result.ToolCalls {
		if !json.Valid([]byte(call.Function.Arguments)) {
			return false
		}
	}
	return true
}

func streamActivity(name string) string {
	switch name {
	case "begin_article", "repair_article_plan":
		return "正在组织文章结构"
	case "save_article_sections", loopSaveTool:
		return "正在生成文章章节草稿"
	case "record_observation":
		return "正在整理视频画面观察"
	case "submit_article", "submit_article_draft", loopFinishTool:
		return "正在准备文章校验请求"
	case "submit_answer":
		return "正在生成正式回答候选"
	case "inspect_video_window", "view_frames":
		return "正在选择需要核对的视频片段"
	case loopReviewTool:
		return "正在综合核对主题、原文与配图"
	case "read_evidence":
		return "正在读取原始证据"
	case "update_article_limitations":
		return "正在整理未确认内容的说明"
	}
	return ""
}
