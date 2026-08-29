package dbmigration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func newPreflightTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open preflight test database: %v", err)
	}
	if err := model.Migrate(db); err != nil {
		t.Fatalf("migrate preflight test database: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("disable SQLite foreign keys for logical-orphan fixtures: %v", err)
	}
	return db
}

func TestSourceSchemaPreflightReportsMissingTableAndColumns(t *testing.T) {
	db := newPreflightTestDB(t)
	if err := db.Migrator().DropTable(&model.TaskCleanupJob{}); err != nil {
		t.Fatalf("drop cleanup table fixture: %v", err)
	}
	for _, indexName := range []string{"idx_video_assets_lifecycle_state", "idx_video_assets_delete_owner_job_id"} {
		if err := db.Exec("DROP INDEX IF EXISTS " + indexName).Error; err != nil {
			t.Fatalf("drop asset index %s: %v", indexName, err)
		}
	}
	for _, column := range []string{"lifecycle_state", "delete_owner_job_id"} {
		if err := db.Exec("ALTER TABLE video_assets DROP COLUMN " + column).Error; err != nil {
			t.Fatalf("drop asset column %s: %v", column, err)
		}
	}

	err := CheckSourceSchema(context.Background(), db)
	if err == nil {
		t.Fatal("CheckSourceSchema() error = nil, want missing schema error")
	}
	message := err.Error()
	for _, want := range []string{
		"task_cleanup_jobs",
		"video_assets.lifecycle_state",
		"video_assets.delete_owner_job_id",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("CheckSourceSchema() error = %q, want %q", message, want)
		}
	}
	for _, forbidden := range []string{"mysql://", "postgres://", "password", "secret"} {
		if strings.Contains(strings.ToLower(message), forbidden) {
			t.Errorf("CheckSourceSchema() leaked connection material %q in %q", forbidden, message)
		}
	}
}

func TestTargetEmptyPreflightIgnoresVectorProjectionButRejectsBusinessRows(t *testing.T) {
	db := newPreflightTestDB(t)
	if err := db.Exec("CREATE TABLE vidlens_rag_vectors (chunk_id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create vector projection fixture: %v", err)
	}
	if err := db.Exec("INSERT INTO vidlens_rag_vectors (chunk_id) VALUES (1)").Error; err != nil {
		t.Fatalf("insert vector projection fixture: %v", err)
	}
	if err := CheckTargetEmpty(context.Background(), db); err != nil {
		t.Fatalf("vector-only target should be accepted: %v", err)
	}

	user := &model.User{Username: "occupied", PasswordHash: "hash"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("insert business row fixture: %v", err)
	}
	err := CheckTargetEmpty(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "users=1") {
		t.Fatalf("CheckTargetEmpty() error = %v, want users=1", err)
	}
}

func TestCollectExactCountsUsesOnlyCatalogTables(t *testing.T) {
	db := newPreflightTestDB(t)
	if err := db.Create(&model.User{Username: "counted", PasswordHash: "hash"}).Error; err != nil {
		t.Fatalf("insert count fixture: %v", err)
	}
	if err := db.Exec("CREATE TABLE unrelated_rows (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create unrelated table: %v", err)
	}
	if err := db.Exec("INSERT INTO unrelated_rows (id) VALUES (1), (2)").Error; err != nil {
		t.Fatalf("insert unrelated rows: %v", err)
	}

	counts, err := CollectExactCounts(context.Background(), db)
	if err != nil {
		t.Fatalf("CollectExactCounts() error = %v", err)
	}
	if len(counts) != len(Catalog()) {
		t.Fatalf("CollectExactCounts() tables = %d, want %d", len(counts), len(Catalog()))
	}
	if counts["users"] != 1 {
		t.Fatalf("users count = %d, want 1", counts["users"])
	}
	if _, included := counts["unrelated_rows"]; included {
		t.Fatal("CollectExactCounts() included a table outside Catalog")
	}
}

func TestLogicalRelationshipPreflightReportsBlockingOrphans(t *testing.T) {
	db := newPreflightTestDB(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	orphanAssetID := int64(700)

	fixtures := []any{
		&model.VideoTask{ID: 701, UserID: 700, AssetID: &orphanAssetID, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "orphan.mp4"},
		&model.VideoTranscription{ID: 702, TaskID: 700, Content: "orphan"},
		&model.ChatSession{ID: 703, UserID: 700, TaskID: 700},
		&model.ChatMessage{ID: 704, SessionID: 700, UserID: 700, Role: "user", Content: "orphan"},
		&model.AIRetryBudget{BudgetID: "orphan-budget-links", TaskID: 700, JobID: 700, Operation: "summary", MaxAttempts: 3, Deadline: now.Add(time.Hour)},
		&model.AIRetryAttempt{ID: 705, BudgetID: "missing-budget", AttemptKey: "attempt", Layer: model.RetryAttemptLayerProvider, CreatedAt: now},
		&model.AIUsageLedger{
			ID: 706, IdempotencyKey: "orphan-ledger", UserID: 700, TaskID: 700, JobID: 700,
			Kind: model.AICallKindLLM, Unit: model.UsageUnitToken, UsageDate: "2026-07-17",
			Status: model.UsageLedgerReserved, ReservedUnits: 10, UsageSource: model.UsageSourceUnknown,
			ReservedAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
		},
		&model.QuotaCompensation{
			ID: 707, EventKey: "orphan-compensation", LedgerID: 700, UserID: 700,
			UsageDate: "2026-07-17", Kind: model.AICallKindLLM, Unit: model.UsageUnitToken,
			Action: "reserve", DeltaUnits: 10, Status: model.CompensationPending,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, fixture := range fixtures {
		if err := db.Create(fixture).Error; err != nil {
			t.Fatalf("create orphan fixture %T: %v", fixture, err)
		}
	}

	err := CheckLogicalRelationships(context.Background(), db)
	if err == nil {
		t.Fatal("CheckLogicalRelationships() error = nil, want orphan report")
	}
	message := err.Error()
	for _, relationship := range []string{
		"video_tasks.user_id -> users.id",
		"video_tasks.asset_id -> video_assets.id",
		"video_transcriptions.task_id -> video_tasks.id",
		"chat_sessions.user_id -> users.id",
		"chat_messages.session_id -> chat_sessions.id",
		"ai_retry_attempts.budget_id -> ai_retry_budgets.budget_id",
		"ai_usage_ledgers.user_id -> users.id",
		"quota_compensations.ledger_id -> ai_usage_ledgers.id",
	} {
		if !strings.Contains(message, relationship) {
			t.Errorf("orphan report = %q, want relationship %q", message, relationship)
		}
	}
}

func TestLogicalRelationshipAuditClassifiesDeletedTaskHistoryAsWarnings(t *testing.T) {
	db := newPreflightTestDB(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	user := &model.User{ID: 1, Username: "history-owner", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create history user: %v", err)
	}
	task := &model.VideoTask{
		ID: 1, UserID: user.ID, FileMD5: "dddddddddddddddddddddddddddddddd", Filename: "deleted.mp4",
		CreatedAt: now, UpdatedAt: now, DeletedAt: gorm.DeletedAt{Time: now.Add(time.Hour), Valid: true},
	}
	if err := db.Unscoped().Create(task).Error; err != nil {
		t.Fatalf("create soft-deleted task: %v", err)
	}
	fixtures := []any{
		&model.AICallLog{
			ID: 10, UserID: user.ID, TaskID: task.ID, JobID: 90, SessionID: 80,
			Kind: model.AICallKindLLM, Status: model.AICallStatusSuccess, CreatedAt: now,
		},
		&model.AIRetryBudget{
			BudgetID: "deleted-task-budget", TaskID: task.ID, JobID: 90, Operation: "analyze",
			MaxAttempts: 3, Deadline: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
		},
		&model.AIUsageLedger{
			ID: 11, IdempotencyKey: "deleted-task-ledger", UserID: user.ID, TaskID: task.ID, JobID: 90,
			Kind: model.AICallKindLLM, Operation: "chat", UsageDate: "2026-07-17", Unit: model.UsageUnitToken,
			Status: model.UsageLedgerSettled, ReservedUnits: 10, UsageSource: model.UsageSourceActual,
			ReservedAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, fixture := range fixtures {
		if err := db.Create(fixture).Error; err != nil {
			t.Fatalf("create retained history fixture %T: %v", fixture, err)
		}
	}

	audit, err := AuditLogicalRelationships(context.Background(), db)
	if err != nil {
		t.Fatalf("AuditLogicalRelationships() error = %v", err)
	}
	if !audit.Valid || len(audit.Violations) != 0 {
		t.Fatalf("relationship audit = %+v, want valid historical references", audit)
	}
	wantWarnings := map[string]int64{
		"ai_call_logs.job_id -> task_jobs.id":         1,
		"ai_call_logs.session_id -> chat_sessions.id": 1,
		"ai_retry_budgets.job_id -> task_jobs.id":     1,
		"ai_usage_ledgers.job_id -> task_jobs.id":     1,
	}
	if len(audit.Warnings) != len(wantWarnings) {
		t.Fatalf("relationship warnings = %+v, want %d categories", audit.Warnings, len(wantWarnings))
	}
	for _, warning := range audit.Warnings {
		if wantWarnings[warning.Relationship] != warning.OrphanRows {
			t.Errorf("warning = %+v, want count from %v", warning, wantWarnings)
		}
		if strings.TrimSpace(warning.Reason) == "" {
			t.Errorf("warning %q has no retention reason", warning.Relationship)
		}
	}
	if err := CheckLogicalRelationships(context.Background(), db); err != nil {
		t.Fatalf("historical references must not block migration: %v", err)
	}
}

func TestLogicalRelationshipAuditStillBlocksMissingJobForActiveTask(t *testing.T) {
	db := newPreflightTestDB(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	user := &model.User{ID: 2, Username: "active-owner", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create active user: %v", err)
	}
	task := &model.VideoTask{ID: 2, UserID: user.ID, FileMD5: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Filename: "active.mp4", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create active task: %v", err)
	}
	call := &model.AICallLog{
		ID: 12, UserID: user.ID, TaskID: task.ID, JobID: 91, SessionID: 81,
		Kind: model.AICallKindLLM, Status: model.AICallStatusSuccess, CreatedAt: now,
	}
	if err := db.Create(call).Error; err != nil {
		t.Fatalf("create active task orphan fixture: %v", err)
	}

	audit, err := AuditLogicalRelationships(context.Background(), db)
	if err != nil {
		t.Fatalf("AuditLogicalRelationships() error = %v", err)
	}
	if audit.Valid || len(audit.Violations) != 1 {
		t.Fatalf("relationship audit = %+v, want one blocking active-task violation", audit)
	}
	if got := audit.Violations[0]; got.Relationship != "ai_call_logs.job_id -> task_jobs.id" || got.OrphanRows != 1 {
		t.Fatalf("blocking violation = %+v", got)
	}
	if len(audit.Warnings) != 1 || audit.Warnings[0].Relationship != "ai_call_logs.session_id -> chat_sessions.id" {
		t.Fatalf("historical session warnings = %+v, want deleted-session reference", audit.Warnings)
	}
	if err := CheckLogicalRelationships(context.Background(), db); err == nil || !strings.Contains(err.Error(), "ai_call_logs.job_id -> task_jobs.id=1") {
		t.Fatalf("CheckLogicalRelationships() error = %v, want active-task job violation", err)
	}
}

func TestLogicalRelationshipPreflightAcceptsEmptyConsistentDatabase(t *testing.T) {
	db := newPreflightTestDB(t)
	if err := CheckLogicalRelationships(context.Background(), db); err != nil {
		t.Fatalf("empty migrated database should be logically consistent: %v", err)
	}
}

func TestInspectTargetReadinessAcceptsAllMissingBusinessTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open empty target fixture: %v", err)
	}
	if err := db.Exec("CREATE TABLE vidlens_rag_vectors (vector_id TEXT PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create vector-only target fixture: %v", err)
	}

	readiness, err := InspectTargetReadiness(context.Background(), db)
	if err != nil {
		t.Fatalf("InspectTargetReadiness() error = %v", err)
	}
	if !readiness.Ready() || readiness.State != TargetStateAbsent {
		t.Fatalf("target readiness = %+v, want absent and ready", readiness)
	}
	if len(readiness.MissingTables) != len(Catalog()) || len(readiness.EmptyTables) != 0 {
		t.Fatalf("target readiness tables = %+v, want every catalog table missing", readiness)
	}
}

func TestInspectTargetReadinessAcceptsCompleteEmptySchema(t *testing.T) {
	db := newPreflightTestDB(t)

	readiness, err := InspectTargetReadiness(context.Background(), db)
	if err != nil {
		t.Fatalf("InspectTargetReadiness() error = %v", err)
	}
	if !readiness.Ready() || readiness.State != TargetStateEmpty {
		t.Fatalf("target readiness = %+v, want empty and ready", readiness)
	}
	if len(readiness.EmptyTables) != len(Catalog()) || len(readiness.MissingTables) != 0 {
		t.Fatalf("target readiness tables = %+v, want every catalog table empty", readiness)
	}
}

func TestInspectTargetReadinessRejectsMixedAndOccupiedSchemas(t *testing.T) {
	t.Run("mixed", func(t *testing.T) {
		db := newPreflightTestDB(t)
		if err := db.Migrator().DropTable(&model.TaskCleanupJob{}); err != nil {
			t.Fatalf("drop one catalog table: %v", err)
		}

		readiness, err := InspectTargetReadiness(context.Background(), db)
		if err == nil || !strings.Contains(err.Error(), "mixed") {
			t.Fatalf("InspectTargetReadiness() error = %v, want mixed schema rejection", err)
		}
		if readiness.State != TargetStateMixed || len(readiness.MissingTables) != 1 {
			t.Fatalf("target readiness = %+v, want one missing table", readiness)
		}
	})

	t.Run("occupied", func(t *testing.T) {
		db := newPreflightTestDB(t)
		if err := db.Create(&model.User{Username: "occupied-target", PasswordHash: "hash"}).Error; err != nil {
			t.Fatalf("insert occupied target row: %v", err)
		}

		readiness, err := InspectTargetReadiness(context.Background(), db)
		if err == nil || !strings.Contains(err.Error(), "users=1") {
			t.Fatalf("InspectTargetReadiness() error = %v, want occupied schema rejection", err)
		}
		if readiness.State != TargetStateOccupied || len(readiness.OccupiedTables) != 1 {
			t.Fatalf("target readiness = %+v, want one occupied table", readiness)
		}
	})
}

func TestInspectTargetReadinessRejectsExistingColumnDrift(t *testing.T) {
	db := newPreflightTestDB(t)
	if err := db.Exec("DROP INDEX IF EXISTS idx_video_assets_lifecycle_state").Error; err != nil {
		t.Fatalf("drop target column index fixture: %v", err)
	}
	if err := db.Exec("ALTER TABLE video_assets DROP COLUMN lifecycle_state").Error; err != nil {
		t.Fatalf("drop target column fixture: %v", err)
	}

	readiness, err := InspectTargetReadiness(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "video_assets.lifecycle_state") {
		t.Fatalf("InspectTargetReadiness() error = %v, want target column drift", err)
	}
	if readiness.State != TargetStateDrifted {
		t.Fatalf("target readiness = %+v, want drifted", readiness)
	}
}

func TestLogicalRelationshipAuditIgnoresZeroTaskIDForKnowledgeBaseSession(t *testing.T) {
	db := newPreflightTestDB(t)
	user := model.User{Username: "kb-session-owner", PasswordHash: "hash"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	session := model.ChatSession{
		UserID: user.ID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: 99, TaskID: 0, Title: "kb",
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create knowledge-base session: %v", err)
	}

	audit, err := AuditLogicalRelationships(context.Background(), db)
	if err != nil {
		t.Fatalf("AuditLogicalRelationships() error = %v", err)
	}
	if !audit.Valid {
		t.Fatalf("knowledge-base session task_id=0 reported as orphan: %+v", audit)
	}
}
