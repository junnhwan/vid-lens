package repository

import (
	"testing"
	"time"
)

func TestRetryBudgetDeadlineEqualUsesPostgresMicrosecondPrecision(t *testing.T) {
	original := time.Date(2026, 7, 14, 20, 0, 0, 987654321, time.UTC)
	stored := original.Truncate(time.Microsecond)
	if !retryBudgetDeadlineEqual(stored, original) {
		t.Fatalf("deadlines should match at PostgreSQL microsecond precision: stored=%s original=%s", stored, original)
	}
}

func TestRetryBudgetDeadlineEqualAcceptsPostgresRoundingUp(t *testing.T) {
	original := time.Date(2026, 7, 14, 20, 0, 0, 987654321, time.UTC)
	stored := original.Round(time.Microsecond)
	if !retryBudgetDeadlineEqual(stored, original) {
		t.Fatalf("deadlines should match when PostgreSQL rounds to the nearest microsecond: stored=%s original=%s", stored, original)
	}
}

func TestRetryBudgetDeadlineEqualRejectsDistinctPostgresMicroseconds(t *testing.T) {
	original := time.Date(2026, 7, 14, 20, 0, 0, 987654000, time.UTC)
	different := original.Add(time.Microsecond)
	if retryBudgetDeadlineEqual(original, different) {
		t.Fatalf("deadlines one microsecond apart must remain distinct: left=%s right=%s", original, different)
	}
}
