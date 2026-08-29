package service

import (
	"context"
	"strings"
	"testing"
)

func TestLLMVideoResearchPlannerParsesDecisionAndCodeFence(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{"```json\n{\"done\":false,\"tool\":\"search_transcript\",\"reason\":\"先定位证据\",\"arguments\":{\"question\":\"owner\"}}\n```"}}
	planner := NewLLMVideoResearchPlanner(chat)

	decision, err := planner.NextDecision(context.Background(), VideoResearchState{Goal: "验证 owner", CurrentStep: 0}, []VideoAgentToolDefinition{{Name: VideoAgentToolSearchTranscript, Description: "检索"}})
	if err != nil {
		t.Fatalf("NextDecision() error = %v", err)
	}
	if decision.Done || decision.Tool != VideoAgentToolSearchTranscript || decision.Reason != "先定位证据" {
		t.Fatalf("decision = %+v", decision)
	}
	if string(decision.Arguments) != `{"question":"owner"}` {
		t.Fatalf("arguments = %s", decision.Arguments)
	}
	if len(chat.messages) != 1 || len(chat.messages[0]) != 2 || !strings.Contains(chat.messages[0][1].Content, "验证 owner") || !strings.Contains(chat.messages[0][1].Content, VideoAgentToolSearchTranscript) {
		t.Fatalf("planner prompt = %+v", chat.messages)
	}
}

func TestLLMVideoResearchPlannerRejectsInvalidJSON(t *testing.T) {
	planner := NewLLMVideoResearchPlanner(&scriptedChatClient{responses: []string{"not json"}})
	_, err := planner.NextDecision(context.Background(), VideoResearchState{Goal: "验证 owner"}, nil)
	if err == nil || !strings.Contains(err.Error(), "解析 video research planner 输出失败") {
		t.Fatalf("NextDecision() error = %v", err)
	}
}
