package mq

import "testing"

func TestDispatchMessageIDSeparatesTaskAttempts(t *testing.T) {
	first := dispatchMessageID(TaskJobAnalyze, 3, "first-lease")
	second := dispatchMessageID(TaskJobAnalyze, 3, "second-lease")
	if first == second {
		t.Fatal("new dispatch reused the previous message id")
	}
	if first != dispatchMessageID(TaskJobAnalyze, 3, "first-lease") {
		t.Fatal("redelivery of the same dispatch changed its message id")
	}
	if got := dispatchMessageID(TaskJobAnalyze, 3, ""); got != "analyze:3" {
		t.Fatalf("legacy message id = %q", got)
	}
}
