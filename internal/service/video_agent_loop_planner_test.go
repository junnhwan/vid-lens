package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vid-lens/internal/ai"
)

func TestVideoAgentPlannerUsesBoundedJSONWithoutChangingWriter(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests++
		if requests == 1 {
			if body["enable_thinking"] != false || body["max_tokens"] != float64(1024) || body["response_format"] == nil {
				t.Error("planner did not use bounded JSON mode")
			}
		} else if body["enable_thinking"] != nil || body["response_format"] != nil {
			t.Error("planner JSON settings leaked into the final writer")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"tool\":\"search_transcript\",\"reason\":\"定位\",\"arguments\":{\"question\":\"选择和评分\"}}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client := ai.NewOpenAIChatClient(server.URL, "test-key", "qwen3.6-flash")
	if _, err := NewLLMVideoAgentLoopPlanner(client).NextDecision(context.Background(), VideoAgentLoopState{Goal: "选择和评分"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat(context.Background(), []ai.ChatMessage{{Role: "user", Content: "生成回答"}}); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests=%d", requests)
	}
}

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
