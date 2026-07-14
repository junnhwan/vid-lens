package service

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxChatSessionTitleRunes = 36

// ResolveChatSessionTitle 创建会话时的标题优先级：
// 1) 调用方显式 title
// 2) 视频 LLM 标题 task.Title
// 3) 文件名
// 4) 「新对话」
func ResolveChatSessionTitle(explicit, videoTitle, filename string) string {
	if t := sanitizeChatSessionTitle(explicit); t != "" {
		return t
	}
	if t := sanitizeChatSessionTitle(videoTitle); t != "" {
		return t
	}
	if t := sanitizeChatSessionTitle(filename); t != "" {
		return t
	}
	return "新对话"
}

// IsPlaceholderChatSessionTitle 判断是否仍是「视频级」占位标题（可用首问覆盖）。
// 自定义过的短标题（与视频标题/文件名不同）视为已定稿，不再自动改写。
func IsPlaceholderChatSessionTitle(current, videoTitle, filename string) bool {
	cur := sanitizeChatSessionTitle(current)
	if cur == "" || cur == "新对话" {
		return true
	}
	vt := sanitizeChatSessionTitle(videoTitle)
	fn := sanitizeChatSessionTitle(filename)
	if vt != "" && cur == vt {
		return true
	}
	if fn != "" && cur == fn {
		return true
	}
	// 旧逻辑曾用纯文件名；去掉扩展名后也算占位
	if fn != "" {
		if base := stripFilenameExt(fn); base != "" && cur == base {
			return true
		}
	}
	return false
}

// AutoTitleChatSessionFromQuestion 用首条用户提问提炼会话标题。
// 仅当 current 仍是占位标题时返回 (title, true)。
func AutoTitleChatSessionFromQuestion(current, videoTitle, filename, question string) (string, bool) {
	if !IsPlaceholderChatSessionTitle(current, videoTitle, filename) {
		return "", false
	}
	title := deriveTitleFromQuestion(question)
	if title == "" {
		return "", false
	}
	// 与占位相同则无意义
	if title == sanitizeChatSessionTitle(current) {
		return "", false
	}
	return title, true
}

func deriveTitleFromQuestion(question string) string {
	q := strings.TrimSpace(question)
	if q == "" {
		return ""
	}
	// 压成单行
	q = strings.ReplaceAll(q, "\r", " ")
	q = strings.ReplaceAll(q, "\n", " ")
	for strings.Contains(q, "  ") {
		q = strings.ReplaceAll(q, "  ", " ")
	}
	q = strings.TrimSpace(q)

	// 去掉常见礼貌/提问前缀（尽量轻量，避免误伤）
	prefixes := []string{
		"请问", "请帮我", "请你", "帮我", "麻烦", "我想问", "我想了解",
		"这个视频", "本视频", "视频里", "视频中", "关于",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(q, p) {
			q = strings.TrimSpace(q[len(p):])
		}
	}
	q = strings.TrimLeftFunc(q, func(r rune) bool {
		return unicode.IsSpace(r) || r == '，' || r == ',' || r == '：' || r == ':' || r == '、'
	})
	if q == "" {
		return ""
	}

	// 在句号/问号处截断第一句
	runes := []rune(q)
	for i, r := range runes {
		switch r {
		case '。', '？', '?', '！', '!', '；', ';':
			if i > 0 {
				runes = runes[:i]
			}
			goto afterCut
		}
	}
afterCut:
	q = strings.TrimSpace(string(runes))
	// 去尾部标点
	q = strings.TrimRightFunc(q, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
	return sanitizeChatSessionTitle(q)
}

func sanitizeChatSessionTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'`")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) > maxChatSessionTitleRunes {
		r := []rune(s)
		s = string(r[:maxChatSessionTitleRunes])
		s = strings.TrimSpace(s)
	}
	return s
}

func stripFilenameExt(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// 只剥一层常见视频扩展
	lower := strings.ToLower(name)
	for _, ext := range []string{".mp4", ".mkv", ".mov", ".webm", ".avi", ".flv", ".m4v"} {
		if strings.HasSuffix(lower, ext) {
			return strings.TrimSpace(name[:len(name)-len(ext)])
		}
	}
	return name
}
