package dbmigration

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"vid-lens/internal/model"
)

func TestDryRunAuditsOneSourceSnapshotWithoutCreatingTargetTables(t *testing.T) {
	source := newCopySQLiteDB(t, "dry_run_source")
	target := newCopySQLiteDB(t, "dry_run_vector_target")
	for _, spec := range Catalog() {
		if err := target.Migrator().DropTable(spec.Model); err != nil {
			t.Fatalf("drop target business table %s: %v", spec.Name, err)
		}
	}
	result, err := DryRun(context.Background(), source, target)
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if result.Source == nil || len(result.Source.Tables) != len(Catalog()) || !result.Source.Relationships.Valid {
		t.Fatalf("source audit = %+v, want complete catalog evidence", result.Source)
	}
	if result.Target == nil || result.Target.State != TargetStateAbsent || !result.Target.Ready() {
		t.Fatalf("target readiness = %+v, want absent and ready", result.Target)
	}
	for _, spec := range Catalog() {
		if target.Migrator().HasTable(spec.Model) {
			t.Fatalf("DryRun() created target business table %s", spec.Name)
		}
	}
}

func TestDryRunPreservesHistoricalRelationshipWarnings(t *testing.T) {
	source := newCopySQLiteDB(t, "dry_run_history_source")
	target := newCopySQLiteDB(t, "dry_run_history_target")
	for _, spec := range Catalog() {
		if err := target.Migrator().DropTable(spec.Model); err != nil {
			t.Fatalf("drop target business table %s: %v", spec.Name, err)
		}
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	user := &model.User{ID: 41, Username: "dry-run-history", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}
	if err := source.Create(user).Error; err != nil {
		t.Fatalf("create source user: %v", err)
	}
	task := &model.VideoTask{
		ID: 42, UserID: user.ID, FileMD5: "ffffffffffffffffffffffffffffffff", Filename: "history.mp4",
		CreatedAt: now, UpdatedAt: now, DeletedAt: gorm.DeletedAt{Time: now.Add(time.Hour), Valid: true},
	}
	if err := source.Unscoped().Create(task).Error; err != nil {
		t.Fatalf("create source soft-deleted task: %v", err)
	}
	call := &model.AICallLog{
		ID: 43, UserID: user.ID, TaskID: task.ID, JobID: 404,
		Kind: model.AICallKindLLM, Status: model.AICallStatusSuccess, CreatedAt: now,
	}
	if err := source.Create(call).Error; err != nil {
		t.Fatalf("create source historical call: %v", err)
	}

	result, err := DryRun(context.Background(), source, target)
	if err != nil {
		t.Fatalf("DryRun() historical references error = %v", err)
	}
	if result.Source == nil || !result.Source.Relationships.Valid || len(result.Source.Relationships.Warnings) != 1 {
		t.Fatalf("source relationship evidence = %+v, want one non-blocking warning", result.Source)
	}
	warning := result.Source.Relationships.Warnings[0]
	if warning.Relationship != "ai_call_logs.job_id -> task_jobs.id" || warning.OrphanRows != 1 {
		t.Fatalf("source warning = %+v", warning)
	}
}

func TestDryRunIgnoresVectorProjectionState(t *testing.T) {
	source := newCopySQLiteDB(t, "dry_run_drift_source")
	target := newCopySQLiteDB(t, "dry_run_drift_target")
	for _, spec := range Catalog() {
		if err := target.Migrator().DropTable(spec.Model); err != nil {
			t.Fatalf("drop target business table %s: %v", spec.Name, err)
		}
	}
	if err := target.Exec(`CREATE TABLE vidlens_rag_vectors (
		vector_id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		task_id INTEGER NOT NULL,
		chunk_id INTEGER NOT NULL,
		chunk_index INTEGER NOT NULL,
		content_hash TEXT NOT NULL,
		embedding_model TEXT NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create unrelated vector projection table: %v", err)
	}
	if err := target.Exec(`INSERT INTO vidlens_rag_vectors
(vector_id, user_id, task_id, chunk_id, chunk_index, content_hash, embedding_model)
VALUES ('target-only', 1, 1, 1, 0, 'hash', 'model')`).Error; err != nil {
		t.Fatalf("insert target-only manifest: %v", err)
	}

	result, err := DryRun(context.Background(), source, target)
	if err != nil {
		t.Fatalf("DryRun() error = %v, relational migration must not depend on vector state", err)
	}
	if result.Target == nil || !result.Target.Ready() {
		t.Fatalf("target readiness = %+v, want preserved ready evidence", result.Target)
	}
}

func TestPostgresAuditExistingMigrationAfterCopy(t *testing.T) {
	source := newCopySQLiteDB(t, "existing_audit_source")
	target := newCopyPostgresDB(t)
	if err := model.Migrate(target); err != nil {
		t.Fatalf("migrate PostgreSQL audit target: %v", err)
	}
	if err := source.Create(&model.User{ID: 500, Username: "audit-user", PasswordHash: "hash"}).Error; err != nil {
		t.Fatalf("insert source audit fixture: %v", err)
	}
	if _, err := Copy(context.Background(), source, target, CopyOptions{BatchSize: 10}); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}

	audit, err := AuditExistingMigration(context.Background(), source, target)
	if err != nil {
		t.Fatalf("AuditExistingMigration() error = %v", err)
	}
	if audit.Data == nil || len(audit.Data.Tables) != len(Catalog()) {
		t.Fatalf("data audit = %+v, want every catalog table", audit.Data)
	}
	if len(audit.Sequences) == 0 {
		t.Fatal("sequence audit is empty")
	}
}
