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
