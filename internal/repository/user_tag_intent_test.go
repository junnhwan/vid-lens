package repository

import (
	"context"
	"gorm.io/gorm"
	"testing"
	"time"
	"vid-lens/internal/model"
)

func TestUserTagIntentLeaseAndCancellation(t *testing.T) { runUserTagIntent(t, summaryRevisionDB(t)) }
func TestPostgresUserTagIntentLeaseAndCancellation(t *testing.T) {
	runUserTagIntent(t, openPostgresRepositoryTestDB(t).db)
}
func runUserTagIntent(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	_, req := tagTaskFixture(t, db, 851, 71)
	repo := NewUserTagRepository(db)
	req.Candidates = []TagCandidate{{Name: "数据库", Reason: "来源主题"}}
	req.LeaseToken = "owned"
	expiry := time.Now().Add(time.Hour)
	job := model.TaskJob{UserID: req.UserID, TaskID: req.TaskID, JobType: model.TaskJobTypeSummary, GenerationID: req.GenerationID, Status: model.TaskStatusRunning, ProcessingToken: req.LeaseToken, LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &expiry}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{ID: req.GenerationID, UserID: req.UserID, TaskID: req.TaskID, SubjectKind: model.AgentRunSubjectSummaryGeneration, SubjectID: req.GenerationID, ExecutionKind: "artifact", RecipeVersion: "summary-generation-v2", ScopeType: model.ChatScopeVideo, Status: model.AgentRunStatusCompleted}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PrepareTagIntent(ctx, req); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.TaskState(ctx, req.UserID, req.TaskID)
	if err != nil || pending.Classification == nil || pending.Classification.Status != "pending" {
		t.Fatal("pending classification missing", pending, err)
	}
	bad := req
	bad.LeaseToken = "stolen"
	if _, err := repo.PublishPendingCandidates(ctx, bad); err == nil {
		t.Fatal("foreign lease accepted")
	}
	now := time.Now()
	if err := db.Model(&run).Update("cancel_requested_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PublishPendingCandidates(ctx, req); err == nil {
		t.Fatal("cancelled run changed tags")
	}
	if err := db.Model(&run).Update("cancel_requested_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	// Candidate request payload is never trusted after the durable intent is frozen.
	req.Candidates = []TagCandidate{{Name: "forged", Reason: "forged"}}
	state, err := repo.PublishPendingCandidates(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if state.Classification == nil || state.Classification.Status != "completed" {
		t.Fatal("completed public classification missing", state)
	}
	if state.Version != 1 || len(state.Assignments) != 1 || state.Assignments[0].Tag.DisplayName != "数据库" {
		t.Fatalf("state=%+v", state)
	}
	state, err = repo.PatchTask(ctx, req.UserID, req.TaskID, TagPatch{ExpectedVersion: 1, RemoveIDs: []string{state.Assignments[0].TagID}})
	if err != nil {
		t.Fatal(err)
	}
	state, err = repo.PublishPendingCandidates(ctx, req)
	if err != nil || state.Version != 2 || len(state.Assignments) != 0 {
		t.Fatal("intent retry reversed user decision", err)
	}
	var intent model.SummaryTagIntent
	if err = db.First(&intent).Error; err != nil || intent.Status != "completed" {
		t.Fatal("intent not acknowledged", err)
	}
	// Cancellation state is durable and still cannot steal a different worker lease.
	if err = db.Model(&intent).Update("status", "pending").Error; err != nil {
		t.Fatal(err)
	}
	if err = repo.MarkTagIntentFailed(ctx, bad, "cancelled", true); err == nil {
		t.Fatal("cancellation bypassed lease")
	}
	if err = repo.MarkTagIntentFailed(ctx, req, "cancelled", true); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.PublishPendingCandidates(ctx, req); err == nil {
		t.Fatal("cancelled intent replayed")
	}
}

func TestTagIntentSupersedesOlderGenerationsWithoutRewritingHistory(t *testing.T) {
	runTagIntentSupersede(t, summaryRevisionDB(t))
}
func TestPostgresTagIntentSupersedesOlderGenerationsWithoutRewritingHistory(t *testing.T) {
	runTagIntentSupersede(t, openPostgresRepositoryTestDB(t).db)
}
func runTagIntentSupersede(t *testing.T, db *gorm.DB) {
	t.Helper()
	_, req := tagTaskFixture(t, db, 852, 72)
	old := []model.SummaryTagIntent{
		{UserID: req.UserID, TaskID: req.TaskID, GenerationID: "older-pending", GeneratedVersion: 1, Status: "pending"},
		{UserID: req.UserID, TaskID: req.TaskID, GenerationID: "older-complete", GeneratedVersion: 1, Status: "completed"},
		{UserID: req.UserID + 1, TaskID: req.TaskID + 1, GenerationID: "other-owner", GeneratedVersion: 1, Status: "pending"},
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := NewUserTagRepository(db).PrepareTagIntent(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"cancelled", "completed", "pending"} {
		var row model.SummaryTagIntent
		if err := db.Where("user_id=? AND task_id=? AND generation_id=? AND generated_version=?", old[i].UserID, old[i].TaskID, old[i].GenerationID, old[i].GeneratedVersion).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status != want {
			t.Fatalf("row %d status %s want %s", i, row.Status, want)
		}
		if i == 0 && row.ErrorCode != "superseded" {
			t.Fatal("missing superseded receipt")
		}
	}
}
