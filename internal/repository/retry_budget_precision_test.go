package repository

import (
	"testing"
	"time"
)

func TestRetryBudgetDeadlineEqualUsesMySQLMillisecondPrecision(t *testing.T) {
	original := time.Date(2026, 7, 14, 20, 0, 0, 987654321, time.Local)
	stored := original.Truncate(time.Millisecond)
	if !retryBudgetDeadlineEqual(stored, original) {
		t.Fatalf("deadlines should match at MySQL datetime(3) precision: stored=%s original=%s", stored, original)
	}
}

func TestRetryBudgetDeadlineEqualAcceptsMySQLRoundingUp(t *testing.T) {
	original := time.Date(2026, 7, 14, 20, 0, 0, 987654321, time.Local)
	stored := original.Round(time.Millisecond)
	if !retryBudgetDeadlineEqual(stored, original) {
		t.Fatalf("deadlines should match when MySQL datetime(3) rounds up: stored=%s original=%s", stored, original)
	}
}
