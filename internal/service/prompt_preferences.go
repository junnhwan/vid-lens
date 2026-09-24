package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"vid-lens/internal/ai"
)

var editablePromptFunctions = []string{"chat", "summary", "vision", "agent"}

type PromptPreferenceView struct {
	Function           string `json:"function"`
	Label              string `json:"label"`
	Scope              string `json:"scope"`
	ProductInstruction string `json:"product_instruction"`
	UserInstruction    string `json:"user_instruction"`
	EffectivePreview   string `json:"effective_preview"`
	Editable           bool   `json:"editable"`
}

func (s *AIProfileService) PromptPreferences(userID int64) ([]PromptPreferenceView, error) {
	views := make([]PromptPreferenceView, 0, len(editablePromptFunctions))
	for _, function := range editablePromptFunctions {
		userText, err := s.repo.PromptPreference(userID, function)
		if err != nil {
			return nil, err
		}
		label, scope, product := promptDescription(function)
		preview := product
		if userText != "" {
			switch function {
			case "summary":
				preview += "\n\n用户摘要偏好（不覆盖报告结构与事实约束）：\n" + userText + "\n若用户偏好与报告必需栏目或事实要求冲突，遵守产品指令。"
			case "vision":
				preview += "\n用户画面描述偏好（不得编造不可见内容）：\n" + userText + "\n如有冲突，遵守前面的事实与格式要求。"
			default:
				preview += "\n\n用户回答偏好（不能覆盖产品证据和引用约束）：\n" + userText + "\n若用户偏好与 VidLens 的证据范围、事实核查或引用格式冲突，遵守产品指令。"
			}
		}
		views = append(views, PromptPreferenceView{Function: function, Label: label, Scope: scope, ProductInstruction: product, UserInstruction: userText, EffectivePreview: preview, Editable: true})
	}
	views = append(views,
		PromptPreferenceView{Function: "title", Label: "自动标题", Scope: "转写后生成视频标题；内部结构固定，只取转写前 1000 字符。已生成标题不重算", ProductInstruction: ai.TitleSystemPrompt, EffectivePreview: ai.TitleSystemPrompt},
		PromptPreferenceView{Function: "planner", Label: "Agent 规划器", Scope: "Agent 新运行的工具选择；工具白名单、JSON 输出与预算由产品控制", ProductInstruction: agentPlannerProductPrompt, EffectivePreview: agentPlannerProductPrompt},
		PromptPreferenceView{Function: "retrieval", Label: "检索辅助", Scope: "查询改写和意图分类；每次输入会追加当前问题与有限上下文", ProductInstruction: "查询改写：你是 VidLens 视频转写检索查询改写器。\n意图分类：你是 VidLens 视频 RAG 的意图分类器。只输出 JSON。", EffectivePreview: "查询改写：你是 VidLens 视频转写检索查询改写器。\n意图分类：你是 VidLens 视频 RAG 的意图分类器。只输出 JSON。"},
		PromptPreferenceView{Function: "visual_query", Label: "按需视觉调查", Scope: "Agent 按问题临时检查画面；提示词由问题与所需事实动态生成，不改写已保存证据", ProductInstruction: QueryVisualPromptTemplate, EffectivePreview: strings.ReplaceAll(strings.ReplaceAll(QueryVisualPromptTemplate, "%s", "[当前问题 / 所需事实]"), "%%", "%")},
	)
	return views, nil
}

func (s *AIProfileService) SetPromptPreference(userID int64, function, text string) error {
	if _, _, product := promptDescription(function); product == "" {
		return fmt.Errorf("未知提示词功能")
	}
	text = strings.TrimSpace(text)
	limit := 2000
	if function == "summary" {
		limit = 500
	}
	if utf8.RuneCountInString(text) > limit {
		return fmt.Errorf("用户偏好最多 %d 字", limit)
	}
	return s.repo.SetPromptPreference(userID, function, text)
}

func promptDescription(function string) (label, scope, product string) {
	switch function {
	case "chat":
		return "普通问答", "当前用户的所有新 Chat 请求；含单视频、视频库和知识库，不按配置档或会话分别保存", ChatProductInstructions()
	case "summary":
		return "视频摘要", "当前用户之后发起的摘要模型请求；已完成摘要不会重算", ai.DefaultSummarySystemPrompt()
	case "vision":
		return "画面描述", "当前用户之后新生成的视觉描述；已保存画面证据不会重算", ai.DefaultVisionCaptionPrompt
	case "agent":
		return "Agent 最终回答", "当前用户之后发起的 Agent 最终回答；规划器和工具 JSON 结构约束保持固定", AgentProductInstructions()
	default:
		return "", "", ""
	}
}
