package repository

import (
	"context"
	"testing"
	"vid-lens/internal/model"
)

func TestRecentRunHistoryKeepsPendingAndRunningAfterRefreshAndIsolatesOwner(t *testing.T) {
	repo := newAgentExecutionTestRepository(t)
	createAgentExecutionRun(t, repo, "running", 4, 4)
	pending := createAgentExecutionRun(t, repo, "pending", 4, 4)
	if err := repo.db.Model(&model.AgentRun{}).Where("id = ?", pending.ID).Update("status", model.AgentRunStatusPending).Error; err != nil {
		t.Fatal(err)
	}
	runs, err := repo.ListSessionRecentRuns(context.Background(), 7, 9)
	if err != nil || len(runs) != 2 {
		t.Fatalf("refresh lost active identity %+v %v", runs, err)
	}
	for _, owner := range [][2]int64{{8, 9}, {7, 10}} {
		rows, err := repo.ListSessionRecentRuns(context.Background(), owner[0], owner[1])
		if err != nil || len(rows) != 0 {
			t.Fatal("cross-owner/session history leak")
		}
	}
}
