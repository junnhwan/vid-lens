package service

import (
	"context"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
)

type VideoAgentTemplate string

const (
	VideoAgentDirectQA       VideoAgentTemplate = "direct_qa"
	VideoAgentSummarizeTopic VideoAgentTemplate = "summarize_topic"
	VideoAgentCompareTopics  VideoAgentTemplate = "compare_topics"
	VideoAgentCritiqueTopic  VideoAgentTemplate = "critique_topic"
)

type VideoAgentRequest struct {
	UserID    int64
	SessionID int64
	Question  string
	TopK      int
}

type VideoAgentResult struct {
	MessageID int64            `json:"message_id"`
	Answer    string           `json:"answer"`
	Template  string           `json:"template"`
	Citations []RetrievedChunk `json:"citations"`
	Trace     []VideoAgentStep `json:"trace"`
	Model     string           `json:"model"`
}

type VideoAgentStep struct {
	Name      string         `json:"name"`
	Tool      string         `json:"tool"`
	Input     map[string]any `json:"input,omitempty"`
	OutputRef string         `json:"output_ref,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type VideoAgentService struct {
	chatSvc *ChatService
}

func NewVideoAgentService(chatSvc *ChatService) *VideoAgentService {
	return &VideoAgentService{chatSvc: chatSvc}
}

func ClassifyVideoAgentTemplate(question string) VideoAgentTemplate {
	question = strings.TrimSpace(question)
	if containsAny(question, "对比", "区别", "前后", "变化") {
		return VideoAgentCompareTopics
	}
	if containsAny(question, "问题", "风险", "不足", "不严谨", "反驳") {
		return VideoAgentCritiqueTopic
	}
	if containsAny(question, "总结", "归纳", "概括") {
		return VideoAgentSummarizeTopic
	}
	return VideoAgentDirectQA
}

func (s *VideoAgentService) Ask(ctx context.Context, req VideoAgentRequest) (*VideoAgentResult, error) {
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	return &VideoAgentResult{Template: string(ClassifyVideoAgentTemplate(req.Question))}, nil
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

var _ = ai.ChatMessage{}
