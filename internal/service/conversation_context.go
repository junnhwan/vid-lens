package service

import (
	"context"
	"encoding/json"
	"strings"

	"vid-lens/internal/model"
)

// KB history comes only from PG, where source edges and saved snapshots can be
// checked together. User messages are kept only with their safe answer so an
// orphan request cannot smuggle a removed video's discussion into the prompt.
func (s *ChatService) loadScopeSafeRecentMessages(ctx context.Context, userID int64, session *model.ChatSession, frozenMembers []int64, limit int) ([]model.ChatMessage, error) {
	if session.ScopeType != model.ChatScopeKnowledgeBase {
		return s.loadRecentMessages(ctx, userID, session.ID, limit)
	}
	if limit <= 0 || len(frozenMembers) == 0 {
		return nil, nil
	}
	if limit > 6 {
		limit = 6
	}
	limit -= limit % 2
	current, err := s.repos.KnowledgeBase.ListMemberTaskIDsForUser(userID, session.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	allowed := map[int64]bool{}
	for _, taskID := range current {
		for _, frozen := range frozenMembers {
			if taskID == frozen {
				allowed[taskID] = true
			}
		}
	}
	if len(allowed) == 0 {
		return nil, nil
	}
	messages, err := s.repos.Chat.ListRecentMessages(userID, session.ID, 12)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(messages))
	for _, message := range messages {
		if message.Role == "assistant" {
			ids = append(ids, message.ID)
		}
	}
	sources, err := s.repos.Chat.ListMessageSourcesForUser(ctx, userID, session.ID, ids)
	if err != nil {
		return nil, err
	}
	return safeKnowledgeHistoryPairs(messages, sources, allowed, limit), nil
}

func safeKnowledgeHistoryPairs(messages []model.ChatMessage, sources []model.ChatMessageSource, allowed map[int64]bool, limit int) []model.ChatMessage {
	byMessage := map[int64]map[int64]bool{}
	for _, source := range sources {
		if byMessage[source.MessageID] == nil {
			byMessage[source.MessageID] = map[int64]bool{}
		}
		byMessage[source.MessageID][source.TaskID] = true
	}
	var result []model.ChatMessage
	var pending *model.ChatMessage
	for i := range messages {
		message := messages[i]
		if message.Role == "user" {
			pending = &messages[i]
			continue
		}
		if message.Role != "assistant" || pending == nil {
			pending = nil
			continue
		}
		user := *pending
		pending = nil
		if strings.TrimSpace(user.Content) == "" || strings.TrimSpace(message.Content) == "" || message.RetrievalSnapshot == nil {
			continue
		}
		edges := byMessage[message.ID]
		if len(edges) == 0 {
			continue
		}
		snapshot, err := DecodeAgentSnapshot(*message.RetrievalSnapshot)
		if err != nil || len(snapshot.Citations) == 0 {
			continue
		}
		expected := map[int64]bool{}
		for _, citation := range snapshot.Citations {
			expected[citation.TaskID] = true
		}
		if len(expected) != len(edges) {
			continue
		}
		safe := true
		for taskID := range edges {
			if !allowed[taskID] || !expected[taskID] {
				safe = false
				break
			}
		}
		if safe {
			result = append(result, user, message)
		}
	}
	if limit > 6 {
		limit = 6
	}
	limit -= limit % 2
	if limit <= 0 {
		return nil
	}
	if len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result
}

// ConversationContextMessage carries only visible conversation text. Tool
// checkpoints and provider reasoning never become conversation history.
type ConversationContextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func boundedConversationContext(recent []model.ChatMessage) []ConversationContextMessage {
	start := len(recent) - 6
	if start < 0 {
		start = 0
	}
	var result []ConversationContextMessage
	for _, message := range recent[start:] {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		content := trimRunes(strings.TrimSpace(message.Content), 1000)
		if content != "" {
			result = append(result, ConversationContextMessage{Role: message.Role, Content: content})
		}
	}
	return result
}

func conversationContextPrompt(messages []ConversationContextMessage) string {
	encoded, _ := json.Marshal(messages)
	return "近期对话仅用于解析指代和讨论对象，不是视频事实或可引用证据；历史助手说法必须由当前授权视频证据核对：\n" + string(encoded)
}
