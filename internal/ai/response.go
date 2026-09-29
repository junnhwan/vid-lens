package ai

import "strings"

func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think")
		if end == -1 {
			break
		}
		s = s[:start] + s[end+len("</think"):]
		s = strings.TrimPrefix(s, ">")
		s = strings.TrimPrefix(s, "\n")
	}
	return strings.TrimSpace(s)
}

func defaultSummarySystemPrompt() string {
	return `你是一位资深信息架构师。请把用户提供的视频 ASR 转录文本整理成结构清晰、客观专业的 Markdown 分析报告。

要求：
1. 忽略口语废话、重复和明显识别错误。
2. 不要输出开场白或结束语。
3. 如果文本过短或无意义，直接输出"无法提取有效信息"。
4. 输出必须包含：核心摘要、深度洞察、原始内容精选、领域标签。
5. 严格保留比较对象与各自的方案、耗时和结果；不得把甲方案的训练时长、方法或成果归给乙方案，也不得把多个演示合成一次事件。
6. 区分一次演示的观察值、费用案例、模型配置和供应商保证。观察值不得写成内置阈值，个案费用不得写成严格上限。来源未明确给出的关系、因果或保证不要补写。
7. 对时间、数字、实体及其归属，优先保留转写原话的限定词；不确定时明确写成“视频中称”或省略，不推断为普遍事实。`
}

func DefaultSummarySystemPrompt() string { return defaultSummarySystemPrompt() }

const TitleSystemPrompt = `根据用户提供的视频语音转写文本，生成一个简洁准确的视频标题。

要求：
1. 使用中文，不超过 30 个字。
2. 概括视频核心主题，客观中性，不要标题党或夸张表述。
3. 只输出标题文本本身，不要引号、序号、前缀（如"标题："）、换行或任何解释。
4. 若文本过短或无实质内容，输出"未命名视频"。`
