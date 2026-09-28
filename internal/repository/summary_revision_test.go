package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func summaryRevisionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func summaryEditFixture(userID, taskID int64, base string) (*model.SummaryEditOperation, *model.AgentRun) {
	now := time.Now().UTC()
	op := &model.SummaryEditOperation{ID: "summary-op-" + artifact.Hash(base + string(rune(userID)))[:10], UserID: userID, TaskID: taskID, Key: "key-" + artifact.Hash(base + string(rune(userID)))[:10], RequestHash: artifact.Hash("request"), RunID: "summary-run-" + artifact.Hash(base + string(rune(userID)))[:10], Mode: "apply", Status: "running", Instruction: "correct term", BaseVersion: 0, BaseContentHash: artifact.Hash(base), BaseContent: base, BaseGeneratedHash: artifact.Hash(base), RuleDigest: artifact.Hash("[]"), RuleSnapshotJSON: `{"version":0,"digest":"unused","rules":[]}`, PatchJSON: `{}`, CreatedAt: now, UpdatedAt: now}
	run := &model.AgentRun{ID: op.RunID, UserID: userID, SubjectKind: model.AgentRunSubjectSummaryEdit, SubjectID: op.ID, ExecutionKind: "artifact", RecipeVersion: "summary-edit-v1", ScopeType: "video", TaskID: taskID, Goal: op.Instruction, Mode: "apply", AgentProfile: "default", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, BudgetSnapshot: `{}`, Status: model.AgentRunStatusRunning, Version: 1, MaxSteps: 1, MaxLLMCalls: 1, MaxAttemptsPerStep: 1, MaxDurationMs: 60_000, CreatedAt: now, UpdatedAt: now}
	return op, run
}

func TestSummaryRevisionIsolatesSameContentUsersAndSurvivesRegeneration(t *testing.T) {
	runSummaryRevisionIsolation(t, summaryRevisionDB(t))
}

func TestPostgresSummaryRevisionIsolatesSameContentUsersAndSurvivesRegeneration(t *testing.T) {
	runSummaryRevisionIsolation(t, openPostgresRepositoryTestDB(t).db)
}

func runSummaryRevisionIsolation(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	md5 := "11111111111111111111111111111111"
	for _, task := range []model.VideoTask{{ID: 41, UserID: 17, FileMD5: md5, Filename: "a.mp4", Status: model.TaskStatusCompleted}, {ID: 42, UserID: 18, FileMD5: md5, Filename: "b.mp4", Status: model.TaskStatusCompleted}} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	generated := model.AISummary{TaskID: 41, FileMD5: md5, Content: "旧名称的安装步骤。"}
	if err := db.Create(&generated).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewSummaryRevisionRepository(db)
	op, run := summaryEditFixture(17, 41, generated.Content)
	if _, err := repo.Begin(ctx, op, run); err != nil {
		t.Fatal(err)
	}
	result, err := repo.Commit(ctx, op.ID, "新名称的安装步骤。", `{"base_hash":"x","edits":[]}`)
	if err != nil || result.Version != 1 {
		t.Fatalf("commit = %+v, %v", result, err)
	}
	if _, err = repo.Begin(ctx, op, run); err != nil {
		t.Fatalf("same-key retry: %v", err)
	}
	if again, err := repo.Commit(ctx, op.ID, "新名称的安装步骤。", `{"base_hash":"x","edits":[]}`); err != nil || again.ID != result.ID {
		t.Fatalf("commit replay = %+v, %v", again, err)
	}
	a, err := repo.Effective(ctx, 17, 41)
	if err != nil || a.Content != "新名称的安装步骤。" || a.Version != 1 {
		t.Fatalf("owner A = %+v, %v", a, err)
	}
	b, err := repo.Effective(ctx, 18, 42)
	if err != nil || b.Content != generated.Content || b.Version != 0 {
		t.Fatalf("owner B leaked: %+v, %v", b, err)
	}
	if _, err = repo.Effective(ctx, 18, 41); err == nil {
		t.Fatal("cross-user task read")
	}
	if err = db.Model(&generated).Update("content", "重新生成的摘要。").Error; err != nil {
		t.Fatal(err)
	}
	a, err = repo.Effective(ctx, 17, 41)
	if err != nil || a.Content != "新名称的安装步骤。" || a.SourceStatus != "needs_merge" {
		t.Fatalf("regeneration overwrote revision: %+v, %v", a, err)
	}
	if _, err = repo.ResolveBase(ctx, 17, 41, 1, "keep_revision"); err != nil {
		t.Fatal(err)
	}
	a, err = repo.Effective(ctx, 17, 41)
	if err != nil || a.Version != 2 || a.SourceStatus != "current" || a.Content != "新名称的安装步骤。" {
		t.Fatalf("resolve base = %+v, %v", a, err)
	}
	now := time.Now().UTC()
	inFlight := model.AgentRun{ID: "summary-run-in-flight", UserID: 17, SubjectKind: model.AgentRunSubjectSummaryEdit, SubjectID: "pending-summary-operation", ExecutionKind: "artifact", RecipeVersion: "summary-edit-v1", ScopeType: "video", TaskID: 41, Goal: "private correction", Mode: "apply", AgentProfile: "default", ProfileSnapshot: "{}", PolicySnapshot: "{}", BudgetSnapshot: "{}", Status: model.AgentRunStatusRunning, Version: 1, MaxSteps: 1, MaxLLMCalls: 1, MaxAttemptsPerStep: 1, CreatedAt: now, UpdatedAt: now}
	if err = db.Create(&inFlight).Error; err != nil { t.Fatal(err) }
	step := model.AgentStep{ID: "summary-step-in-flight", RunID: inFlight.ID, StepID: "summary-plan", Attempt: 1, Sequence: 1, Kind: "plan", Action: "propose_summary_patch", Status: model.AgentStepStatusRunning, LeaseToken: "old-lease", ResultCheckpoint: "private patch", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err = db.Create(&step).Error; err != nil { t.Fatal(err) }
	if err = repo.RevokeSource(41); err != nil {
		t.Fatal(err)
	}
	if err = db.Where("id = ?", inFlight.ID).First(&inFlight).Error; err != nil || inFlight.Status != model.AgentRunStatusCancelled || inFlight.Goal != "" { t.Fatalf("in-flight run not cancelled/scrubbed: %+v, %v", inFlight, err) }
	if err = db.Where("id = ?", step.ID).First(&step).Error; err != nil || step.Status != model.AgentStepStatusFailed || step.LeaseToken != "" || step.ResultCheckpoint != "" { t.Fatalf("in-flight step not fenced/scrubbed: %+v, %v", step, err) }
	var scrubbed model.SummaryRevision
	if err = db.Where("user_id = ? AND task_id = ? AND version = ?", 17, 41, 1).First(&scrubbed).Error; err != nil || scrubbed.Content != "" {
		t.Fatalf("revision not scrubbed: %+v, %v", scrubbed, err)
	}
	var scrubbedOp model.SummaryEditOperation
	if err = db.Where("id = ?", op.ID).First(&scrubbedOp).Error; err != nil || scrubbedOp.BaseContent != "" || scrubbedOp.PatchJSON != "{}" {
		t.Fatalf("operation not scrubbed: %+v, %v", scrubbedOp, err)
	}
	if err = db.Delete(&model.VideoTask{}, 41).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Effective(ctx, 17, 41); err == nil {
		t.Fatal("deleted source remained readable")
	}
	if _, err = repo.Operation(ctx, 17, 41, op.ID); err == nil {
		t.Fatal("deleted source operation remained readable")
	}
	b, err = repo.Effective(ctx, 18, 42)
	if err != nil || b.Content != "重新生成的摘要。" {
		t.Fatalf("other user was affected by deletion: %+v, %v", b, err)
	}
}

func TestVideoTermRuleVersionIsolatedAndDisabledAsNewSnapshot(t *testing.T) {
	runVideoTermRuleVersionTest(t, summaryRevisionDB(t))
}

func TestPostgresVideoTermRuleVersionIsolatedAndDisabledAsNewSnapshot(t *testing.T) {
	runVideoTermRuleVersionTest(t, openPostgresRepositoryTestDB(t).db)
}

func runVideoTermRuleVersionTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	for _, task := range []model.VideoTask{{ID: 51, UserID: 17, FileMD5: "22222222222222222222222222222222", Filename: "a.mp4"}, {ID: 52, UserID: 18, FileMD5: "22222222222222222222222222222222", Filename: "b.mp4"}} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewVideoTermRuleRepository(db)
	v1, err := repo.Save(ctx, 17, 51, 0, artifact.Hash("rule-v1"), `[{"id":"term","enabled":true}]`, nil)
	if err != nil || v1.Version != 1 {
		t.Fatalf("save v1 = %+v, %v", v1, err)
	}
	if _, err = repo.Current(ctx, 18, 52); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Version(ctx, 18, 52, 1); err == nil {
		t.Fatal("cross-user rule read")
	}
	v2, err := repo.Save(ctx, 17, 51, 1, artifact.Hash("rule-v2"), `[{"id":"term","enabled":false}]`, nil)
	if err != nil || v2.Version != 2 {
		t.Fatalf("save disabled = %+v, %v", v2, err)
	}
	frozen, err := repo.Version(ctx, 17, 51, 1)
	var frozenRules []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(frozen.RulesJSON), &frozenRules)
	}
	if err != nil || len(frozenRules) != 1 || frozenRules[0].ID != "term" || !frozenRules[0].Enabled {
		t.Fatalf("frozen rule changed = %+v, %v", frozen, err)
	}
	if _, err = repo.Save(ctx, 17, 51, 1, artifact.Hash("rule-v3"), `[]`, nil); err == nil {
		t.Fatal("stale rule version accepted")
	}
	if err = db.Delete(&model.VideoTask{}, 51).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Current(ctx, 17, 51); err == nil {
		t.Fatal("deleted source rule remained readable")
	}
}
