package service

import (
	"context"
	"fmt"
	"strings"

	"vid-lens/internal/model"
)

// docs/architecture/reliability.md 档2：LLM 失败 → 无 LLM 模式（检索片段 + 已有摘要直拼 + degraded:true，不调 LLM）。
//
// 前置诚信检查（当前实现约束硬约束）：这是现有 buildRAGMessages 的降级补全，不是
// 从零新建生成路径。片段拼装（prepared.Messages / prepared.Contexts）已由 chat_prepare.go
// 完成；本方法只补"不调 LLM + degraded 标志 + 复用 docs/architecture/data-model.md FindByMD5 跨 task 摘要"。
//
// docs/architecture/data-model.md 的 FindByMD5 跨 task 摘要复用：档2 触发时，若该内容已有成功摘要（按
// file_md5 查任意 task/任意用户），直接拼进来；没有则只给检索片段。摘要来源优先
// 当前会话绑定的 task，其次按 file_md5 跨 task 复用（docs/architecture/reliability.md 行为约束 6）。
//
// 降级答案诚实标注（当前实现约束）：不暴露"档2/vector_only"内部术语，UI 侧由
// degraded:true 标志触发"参考片段（AI 摘要暂不可用）"折叠呈现。答案体本身是可读的
// 片段拼装，不带内部标记。
type degradedAnswer struct {
	answer string
}

// applyTier2Degradation 是 Ask / AskStream 共用的档2降级入口：构造降级答案 + 计一次
// 档2触发，返回降级答案体。消除 chat_ask.go / chat_stream.go 三处重复的
// "buildDegradedAnswer + recordDegradationTier2"序列（docs/architecture/reliability.md 当前实现约束）。
//
// 已发出的流式 delta 由 caller 决定是否保留（docs/architecture/reliability.md 档2 只要求"片段+摘要直拼 +
// degraded:true 不调 LLM"，不规定部分 delta 的处理；caller 在流式路径上用本方法
// 返回的答案体覆盖/补全）。
func (s *ChatService) applyTier2Degradation(ctx context.Context, prepared *preparedRAGChat) string {
	recordDegradationTier2()
	return s.buildDegradedAnswer(ctx, prepared).answer
}

// buildDegradedAnswer 构造档2降级答案：检索片段 + 已有摘要（docs/architecture/data-model.md FindByMD5）直拼。
//
// 不调 LLM。若 prepared.Contexts 为空（概览/兜底路径落到档2），仍尝试拼摘要；
// 摘要也没有则返回最小诚实提示（仍标 degraded:true，不全废请求）。
func (s *ChatService) buildDegradedAnswer(ctx context.Context, prepared *preparedRAGChat) degradedAnswer {
	sections := make([]string, 0, 2)

	// 1. 已有摘要（docs/architecture/data-model.md FindByMD5 跨 task 复用）。优先当前 task 的摘要，
	//    其次按 file_md5 跨 task 复用（docs/architecture/reliability.md 行为约束 6）。
	if summary := s.degradedSummary(prepared.Session); summary != "" {
		sections = append(sections, "已有摘要：\n"+summary)
	}

	// 2. 检索片段直拼（buildRAGMessages 已构造 prompt，但档2 不调 LLM，故直接把
	//    片段内容按引用编号拼成可读文本，作为降级答案体）。
	if chunks := prepared.Contexts; len(chunks) > 0 {
		lines := make([]string, 0, len(chunks))
		for i, c := range chunks {
			content := strings.TrimSpace(c.Content)
			if content == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("[参考片段%d]\n%s", i+1, content))
		}
		if len(lines) > 0 {
			sections = append(sections, strings.Join(lines, "\n\n"))
		}
	}

	if len(sections) == 0 {
		// 没有片段也没有摘要：仍返回降级态而非错误，避免请求全废（④ 稀缺点）。
		// 诚实提示用户当前 AI 生成不可用，已保存为降级消息。
		return degradedAnswer{answer: "AI 摘要暂不可用，且未检索到参考片段。请稍后重试。"}
	}
	return degradedAnswer{answer: strings.Join(sections, "\n\n")}
}

// degradedSummary 取档2 降级答案要拼进去的已有摘要。
//
// docs/architecture/data-model.md 的 FindByMD5 跨 task 复用：当前会话绑定的 task 有摘要 → 用；
// 没有但该 task 的 asset 有 file_md5 且按 file_md5 查到其他 task 的成功摘要 → 复用
// （docs/architecture/reliability.md 行为约束 6：档2 复用 docs/architecture/data-model.md 已有摘要，降级答案有摘要加持而非纯片段）。
func (s *ChatService) degradedSummary(session *model.ChatSession) string {
	if s.repos == nil || s.repos.Summary == nil || session == nil {
		return ""
	}
	// 优先当前 task 自有摘要。
	if session.TaskID > 0 {
		if summary, err := s.repos.Summary.FindByTaskID(session.TaskID); err == nil && summary != nil {
			if c := strings.TrimSpace(summary.Content); c != "" {
				return c
			}
		}
	}
	// 跨 task 按 file_md5 复用（docs/architecture/data-model.md FindByMD5）。
	// session 不直接持 file_md5；通过 task 取 asset 的 file_md5 再查摘要。
	if session.TaskID > 0 && s.repos.Task != nil {
		task, err := s.repos.Task.FindByID(session.TaskID)
		if err != nil || task == nil || task.FileMD5 == "" {
			return ""
		}
		if summary, err := s.repos.Summary.FindByMD5(task.FileMD5); err == nil && summary != nil {
			if c := strings.TrimSpace(summary.Content); c != "" {
				return c
			}
		}
	}
	return ""
}
