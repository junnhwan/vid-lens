package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLLMVideoAgentLoopPlannerReceivesToolArgumentSchemas(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{`{"done":false,"tool":"search_transcript","reason":"locate evidence","arguments":{"question":"four steps"}}`}}
	definitions := NewVideoAgentTools(nil, nil, nil).Registry().Definitions()
	_, err := NewLLMVideoAgentLoopPlanner(chat).NextDecision(context.Background(), VideoAgentLoopState{Goal: "four steps"}, definitions)
	if err != nil {
		t.Fatal(err)
	}
	prompt := chat.messages[0][1].Content
	for _, definition := range definitions {
		var schema map[string]any
		if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
			t.Fatalf("%s has no valid input schema: %v", definition.Name, err)
		}
		encoded, _ := json.Marshal(definition)
		if !strings.Contains(prompt, string(encoded)) {
			t.Fatalf("planner did not receive %s argument schema", definition.Name)
		}
		if schema["additionalProperties"] != false || schema["properties"] == nil || schema["required"] == nil {
			t.Fatalf("%s schema does not define its accepted arguments", definition.Name)
		}
	}
	var visualSchema map[string]any
	if err := json.Unmarshal(videoAgentToolInputSchema(VideoAgentToolInvestigateVisual), &visualSchema); err != nil {
		t.Fatal(err)
	}
}

func TestLLMVideoAgentLoopPlannerParsesDecisionAndCodeFence(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{"```json\n{\"done\":false,\"tool\":\"search_transcript\",\"reason\":\"先定位证据\",\"arguments\":{\"question\":\"owner\"}}\n```"}}
	planner := NewLLMVideoAgentLoopPlanner(chat)

	decision, err := planner.NextDecision(context.Background(), VideoAgentLoopState{Goal: "验证 owner", CurrentStep: 0}, []VideoAgentToolDefinition{{Name: VideoAgentToolSearchTranscript, Description: "检索"}})
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

func TestLLMVideoAgentLoopPlannerRejectsInvalidJSON(t *testing.T) {
	planner := NewLLMVideoAgentLoopPlanner(&scriptedChatClient{responses: []string{"not json"}})
	_, err := planner.NextDecision(context.Background(), VideoAgentLoopState{Goal: "验证 owner"}, nil)
	if err == nil || !strings.Contains(err.Error(), "解析 video research planner 输出失败") {
		t.Fatalf("NextDecision() error = %v", err)
	}
}
