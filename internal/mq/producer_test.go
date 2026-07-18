package mq

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRAGIndexMessageIncludesTaskIDAndTraceID(t *testing.T) {
	ctx := ContextWithTraceID(context.Background(), "trace-rag-1")

	msg := newRAGIndexMessage(ctx, 42)

	if string(msg.Key) != "42" {
		t.Fatalf("message key = %q, want task id", string(msg.Key))
	}
	var payload RAGIndexPayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.TaskID != 42 {
		t.Fatalf("payload task_id = %d, want 42", payload.TaskID)
	}
	if payload.TraceID != "trace-rag-1" {
		t.Fatalf("payload trace_id = %q, want trace-rag-1", payload.TraceID)
	}
}

func TestCreateTopicsRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		brokers []string
		topics  []string
	}{
		{name: "missing brokers", topics: []string{"video-analyze"}},
		{name: "missing topics", brokers: []string{"127.0.0.1:9092"}},
		{name: "empty broker", brokers: []string{"  "}, topics: []string{"video-analyze"}},
		{name: "empty topic", brokers: []string{"127.0.0.1:9092"}, topics: []string{"  "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CreateTopics(tt.brokers, tt.topics); err == nil {
				t.Fatal("CreateTopics should reject invalid configuration")
			}
		})
	}
}

func TestPingBrokerRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		brokers []string
	}{
		{name: "missing brokers"},
		{name: "empty broker", brokers: []string{"  "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PingBroker(context.Background(), tt.brokers); err == nil {
				t.Fatal("PingBroker should reject invalid configuration")
			}
		})
	}
}
