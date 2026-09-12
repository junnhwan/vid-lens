package service

import (
	"context"
	"strconv"
	"strings"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// Extraction is deterministic and requires an explicit enduring instruction.
// A temporary or quoted request remains in the current conversation only.
func extractStructuredPreferences(request MemoryExtractionRequest) []MemoryCandidate {
	text := strings.ToLower(strings.TrimSpace(request.UserText))
	if !containsAny(text, "回答", "回复", "输出", "请用", "偏好", "answer", "respond") {
		return nil
	}
	if !containsAny(text, "以后", "今后", "始终", "一直", "默认", "长期", "记住", "from now on", "always", "remember my", "by default") || containsAny(text, "这次", "本次", "这一轮", "暂时", "临时", "just this", "this time", "for now", "this answer", "这回", "他说", "她说", "他要求", "她要求", "朋友要求", "别人", "引用", "he said", "she said", "they said", "he asked", "she asked", "they asked", "he wants", "she wants", "they want", "quoted", "“", "‘", "「", "『", "\"") {
		return nil
	}
	values := map[string]string{}
	clauses := strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("，,。;；\n", r) })
	for _, clause := range clauses {
		if containsAny(clause, "不要", "不想", "不喜欢", "不希望", "不需要", "不用", "不必", "无需", "避免", "不再", "不能", "不是", "别用", "don't", "do not", "never", "not ") {
			continue
		}
		if containsAny(clause, "中文", "chinese") {
			values["response.language"] = "回答语言：中文"
		}
		if containsAny(clause, "英文", "english") {
			values["response.language"] = "回答语言：英文"
		}
		if containsAny(clause, "简洁", "简短", "concise", "brief") {
			values["response.verbosity"] = "回答风格：简洁"
		}
		if containsAny(clause, "详细", "in detail") {
			values["response.verbosity"] = "回答风格：详细"
		}
		if containsAny(clause, "要点", "bullet", "列表") {
			values["response.format"] = "回答格式：优先使用要点列表"
		}
		if containsAny(clause, "段落", "paragraph") {
			values["response.format"] = "回答格式：优先使用段落"
		}
	}
	var candidates []MemoryCandidate
	for _, key := range []string{"response.language", "response.verbosity", "response.format"} {
		if value := values[key]; value != "" {
			candidates = append(candidates, MemoryCandidate{UserID: request.UserID, SessionID: request.SessionID, Scope: MemoryScope{Type: model.MemoryScopeUser, ID: strconv.FormatInt(request.UserID, 10)}, Kind: key, Content: value, SourceType: "user_message", SourceRef: strings.TrimSpace(request.SourceRef), Importance: .7})
		}
	}
	return candidates
}

func (s *ChatService) injectChatPreferences(ctx context.Context, prepared *preparedRAGChat, policy model.EffectiveMemoryPolicy) {
	if !policy.EffectiveEnabled || s.longTermMemory == nil {
		return
	}
	snapshot, err := s.longTermMemory.Snapshot(ctx, MemorySnapshotRequest{UserID: prepared.Session.UserID, Query: prepared.Question, Scopes: []MemoryScope{{Type: model.MemoryScopeUser, ID: strconv.FormatInt(prepared.Session.UserID, 10)}}})
	if err != nil || len(snapshot.Items) == 0 {
		return
	}
	prepared.Messages = append([]ai.ChatMessage{{Role: "system", Content: snapshot.PromptContext()}}, prepared.Messages...)
}
