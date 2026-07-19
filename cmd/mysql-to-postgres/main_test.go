package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/config"
	"vid-lens/internal/dbmigration"
	"vid-lens/internal/model"
)

func TestMigrationApplicationDryRunDoesNotCreateTargetBusinessTables(t *testing.T) {
	source := newCLIWorkflowDB(t, "dry_source", true)
	target := newCLIWorkflowDB(t, "dry_target", false)
	cfg := validMigrationConfig()
	var captured dbmigration.MigrationReport

	app := defaultTestMigrationApplication()
	app.loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	app.openSource = fixedConnectionOpener(source)
	app.openTarget = fixedSchemaConnectionOpener(target)
	app.writeReport = func(_ string, report dbmigration.MigrationReport) error {
		captured = report
		return nil
	}
	app.now = func() time.Time { return time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC) }
	app.output = &bytes.Buffer{}

	if err := app.run(context.Background(), options{configPath: "ignored.yaml", targetSchema: "public", reportPath: ".logs/test.json", batchSize: 100}); err != nil {
		t.Fatalf("migrationApplication.run() error = %v", err)
	}
	if !captured.Success || captured.SourceAudit == nil || captured.TargetReadiness == nil {
		t.Fatalf("captured dry-run report = %+v, want complete success evidence", captured)
	}
	if captured.CompletionState != dbmigration.MigrationCompletionComplete || captured.Phase != dbmigration.MigrationPhaseComplete || captured.CopyCommitted {
		t.Fatalf("captured dry-run lifecycle = %+v, want complete without relational commit", captured)
	}
	if captured.StartedAt.IsZero() || captured.CompletedAt.IsZero() {
		t.Fatalf("captured dry-run timestamps = started %v completed %v", captured.StartedAt, captured.CompletedAt)
	}
	for _, spec := range dbmigration.Catalog() {
		if target.Migrator().HasTable(spec.Model) {
			t.Fatalf("dry-run created target business table %s", spec.Name)
		}
	}
}

func TestMigrationApplicationUpgradeSourceSchemaDoesNotOpenPostgres(t *testing.T) {
	source := newCLIWorkflowDB(t, "upgrade_source", false)
	cfg := validMigrationConfig()
	cfg.Database = config.DatabaseConfig{}
	openedTarget := false
	var captured dbmigration.MigrationReport

	app := defaultTestMigrationApplication()
	app.loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	app.openSource = fixedConnectionOpener(source)
	app.openTarget = func(context.Context, *config.Config, string) (*databaseConnection, error) {
		openedTarget = true
		return nil, fmt.Errorf("must not open target")
	}
	app.writeReport = func(_ string, report dbmigration.MigrationReport) error {
		captured = report
		return nil
	}
	app.now = time.Now
	app.output = &bytes.Buffer{}

	opts := options{configPath: "ignored.yaml", targetSchema: "public", reportPath: ".logs/test.json", batchSize: 100, upgradeSourceSchema: true}
	if err := app.run(context.Background(), opts); err != nil {
		t.Fatalf("migrationApplication.run() error = %v", err)
	}
	if openedTarget {
		t.Fatal("upgrade-source-schema opened PostgreSQL")
	}
	if !captured.Success || captured.Mode != string(modeUpgradeSourceSchema) {
		t.Fatalf("upgrade report = %+v, want successful upgrade mode", captured)
	}
	for _, spec := range dbmigration.Catalog() {
		if !source.Migrator().HasTable(spec.Model) {
			t.Errorf("upgrade-source-schema did not create %s", spec.Name)
		}
	}
	if source.Migrator().HasTable(&model.KnowledgeBase{}) || source.Migrator().HasTable(&model.ChatMessageSource{}) {
		t.Fatal("upgrade-source-schema created online knowledge-base tables in legacy source")
	}
	if source.Migrator().HasColumn(&model.LegacyChatSession{}, "scope_type") || source.Migrator().HasColumn(&model.LegacyChatSession{}, "knowledge_base_id") {
		t.Fatal("upgrade-source-schema added online chat-session scope columns to legacy source")
	}
}

func TestMigrationApplicationExecuteReconnectsBeforeIndependentAudit(t *testing.T) {
	cfg := validMigrationConfig()
	events := make([]string, 0)
	openCount := map[string]int{}
	newConnection := func(name string) *databaseConnection {
		openCount[name]++
		generation := openCount[name]
		events = append(events, fmt.Sprintf("open-%s-%d", name, generation))
		return &databaseConnection{
			DB: &gorm.DB{},
			Close: func() error {
				events = append(events, fmt.Sprintf("close-%s-%d", name, generation))
				return nil
			},
		}
	}
	var captured dbmigration.MigrationReport

	app := defaultTestMigrationApplication()
	app.loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	app.openSource = func(context.Context, *config.Config) (*databaseConnection, error) {
		return newConnection("source"), nil
	}
	app.openTarget = func(context.Context, *config.Config, string) (*databaseConnection, error) {
		return newConnection("target"), nil
	}
	app.dryRun = func(context.Context, *gorm.DB, *gorm.DB) (*dbmigration.DryRunResult, error) {
		events = append(events, "dry-run")
		return &dbmigration.DryRunResult{
			Source: &dbmigration.SourceAudit{Relationships: dbmigration.RelationshipAudit{Valid: true}},
			Target: &dbmigration.TargetReadiness{State: dbmigration.TargetStateAbsent},
		}, nil
	}
	app.migrateTarget = func(*gorm.DB) error {
		events = append(events, "migrate-target")
		return nil
	}
	app.copyData = func(context.Context, *gorm.DB, *gorm.DB, dbmigration.CopyOptions) (*dbmigration.CopyResult, error) {
		events = append(events, "copy")
		return &dbmigration.CopyResult{}, nil
	}
	app.auditExisting = func(context.Context, *gorm.DB, *gorm.DB) (*dbmigration.ExistingMigrationAudit, error) {
		events = append(events, "independent-audit")
		return &dbmigration.ExistingMigrationAudit{
			Data: &dbmigration.DataAudit{
				SourceRelationships: dbmigration.RelationshipAudit{Valid: true},
				TargetRelationships: dbmigration.RelationshipAudit{Valid: true},
			},
		}, nil
	}
	app.writeReport = func(_ string, report dbmigration.MigrationReport) error {
		captured = report
		return nil
	}
	app.now = time.Now
	app.output = &bytes.Buffer{}

	opts := options{configPath: "ignored.yaml", targetSchema: "public", reportPath: ".logs/test.json", batchSize: 100, execute: true}
	if err := app.run(context.Background(), opts); err != nil {
		t.Fatalf("migrationApplication.run() error = %v\nevents: %v", err, events)
	}
	if !captured.Success || openCount["source"] != 2 || openCount["target"] != 2 {
		t.Fatalf("execute report/open counts = %+v / %+v", captured, openCount)
	}
	if captured.CompletionState != dbmigration.MigrationCompletionRelationalAudited || !captured.CopyCommitted || captured.CopyCommittedAt == nil {
		t.Fatalf("execute lifecycle = %+v, want committed and independently audited", captured)
	}
	joined := strings.Join(events, ",")
	for _, requiredOrder := range []string{
		"copy,close-target-1,close-source-1",
		"open-source-2,open-target-2,independent-audit",
	} {
		if !strings.Contains(joined, requiredOrder) {
			t.Errorf("events = %s, want ordered segment %s", joined, requiredOrder)
		}
	}
}

func TestMigrationApplicationExecuteStopsWhenAdvisoryLockIsUnavailable(t *testing.T) {
	cfg := validMigrationConfig()
	preflightCalled := false
	var captured dbmigration.MigrationReport

	app := defaultTestMigrationApplication()
	app.loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	app.openSource = func(context.Context, *config.Config) (*databaseConnection, error) {
		return &databaseConnection{DB: &gorm.DB{}, Close: func() error { return nil }}, nil
	}
	app.openTarget = func(context.Context, *config.Config, string) (*databaseConnection, error) {
		return &databaseConnection{DB: &gorm.DB{}, Close: func() error { return nil }}, nil
	}
	app.acquireLock = func(context.Context, *gorm.DB) (func(context.Context) error, error) {
		return nil, fmt.Errorf("another migration holds the advisory lock")
	}
	app.dryRun = func(context.Context, *gorm.DB, *gorm.DB) (*dbmigration.DryRunResult, error) {
		preflightCalled = true
		return nil, nil
	}
	app.writeReport = func(_ string, report dbmigration.MigrationReport) error {
		captured = report
		return nil
	}

	err := app.run(context.Background(), options{
		configPath: "ignored.yaml", targetSchema: "public", reportPath: ".logs/test.json", batchSize: 100, execute: true,
	})
	if err == nil || !strings.Contains(err.Error(), "advisory lock") {
		t.Fatalf("migrationApplication.run() error = %v, want lock contention", err)
	}
	if preflightCalled {
		t.Fatal("relational preflight ran without owning the migration advisory lock")
	}
	if captured.FailureStage != dbmigration.MigrationPhaseAcquireLock || captured.CopyCommitted {
		t.Fatalf("report = %+v, want acquire-lock failure before commit", captured)
	}
	if captured.CompletionState != dbmigration.MigrationCompletionNotCommitted {
		t.Fatalf("completion_state = %q, want %q", captured.CompletionState, dbmigration.MigrationCompletionNotCommitted)
	}
}

func TestMigrationApplicationAuditReportsCompleteWithoutCopyCommit(t *testing.T) {
	cfg := validMigrationConfig()
	var captured dbmigration.MigrationReport
	app := defaultTestMigrationApplication()
	app.loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	app.openSource = func(context.Context, *config.Config) (*databaseConnection, error) {
		return &databaseConnection{DB: &gorm.DB{}, Close: func() error { return nil }}, nil
	}
	app.openTarget = func(context.Context, *config.Config, string) (*databaseConnection, error) {
		return &databaseConnection{DB: &gorm.DB{}, Close: func() error { return nil }}, nil
	}
	app.auditExisting = func(context.Context, *gorm.DB, *gorm.DB) (*dbmigration.ExistingMigrationAudit, error) {
		return &dbmigration.ExistingMigrationAudit{Data: &dbmigration.DataAudit{}}, nil
	}
	app.writeReport = func(_ string, report dbmigration.MigrationReport) error {
		captured = report
		return nil
	}

	if err := app.run(context.Background(), options{
		configPath: "ignored.yaml", targetSchema: "public", reportPath: ".logs/test.json", batchSize: 100, audit: true,
	}); err != nil {
		t.Fatalf("migrationApplication.run() audit error = %v", err)
	}
	if !captured.Success || captured.CompletionState != dbmigration.MigrationCompletionComplete || captured.CopyCommitted {
		t.Fatalf("audit lifecycle = %+v, want complete independent audit without copy commit", captured)
	}
}

func TestMigrationApplicationReportsCommittedCopyWhenIndependentAuditCannotStart(t *testing.T) {
	cfg := validMigrationConfig()
	targetOpens := 0
	var captured dbmigration.MigrationReport

	app := defaultTestMigrationApplication()
	app.loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	app.openSource = func(context.Context, *config.Config) (*databaseConnection, error) {
		return &databaseConnection{DB: &gorm.DB{}, Close: func() error { return nil }}, nil
	}
	app.openTarget = func(context.Context, *config.Config, string) (*databaseConnection, error) {
		targetOpens++
		if targetOpens == 2 {
			return nil, fmt.Errorf("simulated reopen failure")
		}
		return &databaseConnection{DB: &gorm.DB{}, Close: func() error { return nil }}, nil
	}
	app.dryRun = func(context.Context, *gorm.DB, *gorm.DB) (*dbmigration.DryRunResult, error) {
		return &dbmigration.DryRunResult{
			Source: &dbmigration.SourceAudit{Relationships: dbmigration.RelationshipAudit{Valid: true}},
			Target: &dbmigration.TargetReadiness{State: dbmigration.TargetStateAbsent},
		}, nil
	}
	app.migrateTarget = func(*gorm.DB) error { return nil }
	app.copyData = func(context.Context, *gorm.DB, *gorm.DB, dbmigration.CopyOptions) (*dbmigration.CopyResult, error) {
		return &dbmigration.CopyResult{}, nil
	}
	app.writeReport = func(_ string, report dbmigration.MigrationReport) error {
		captured = report
		return nil
	}
	app.now = func() time.Time { return time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC) }

	err := app.run(context.Background(), options{
		configPath: "ignored.yaml", targetSchema: "public", reportPath: ".logs/test.json", batchSize: 100, execute: true,
	})
	if err == nil || !strings.Contains(err.Error(), "reopen") {
		t.Fatalf("migrationApplication.run() error = %v, want reopen failure", err)
	}
	if captured.Success {
		t.Fatalf("report = %+v, want failed completion", captured)
	}
	if !captured.CopyCommitted || captured.CopyCommittedAt == nil {
		t.Fatalf("report = %+v, want durable copy commit evidence", captured)
	}
	if captured.FailureStage != dbmigration.MigrationPhaseReopenConnections {
		t.Fatalf("failure_stage = %q, want %q", captured.FailureStage, dbmigration.MigrationPhaseReopenConnections)
	}
	if captured.CompletionState != dbmigration.MigrationCompletionAuditPending {
		t.Fatalf("completion_state = %q, want %q", captured.CompletionState, dbmigration.MigrationCompletionAuditPending)
	}
}

func validMigrationConfig() *config.Config {
	return &config.Config{
		LegacyMySQL: config.LegacyMySQLConfig{Host: "mysql", Port: 3306, Username: "user", DBName: "vidlens", Charset: "utf8mb4"},
		Database:    config.DatabaseConfig{Host: "postgres", Port: 5432, Username: "vidlens", DBName: "vidlens"},
		RAG:         config.RAGConfig{EmbeddingDim: 3, VectorTable: "vidlens_rag_vectors"},
	}
}

func defaultTestMigrationApplication() migrationApplication {
	app := newMigrationApplication(&bytes.Buffer{})
	app.acquireLock = func(context.Context, *gorm.DB) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	}
	app.writeReport = func(string, dbmigration.MigrationReport) error { return nil }
	return app
}

func fixedConnectionOpener(db *gorm.DB) sourceConnectionOpener {
	return func(context.Context, *config.Config) (*databaseConnection, error) {
		return &databaseConnection{DB: db, Close: func() error { return nil }}, nil
	}
}

func fixedSchemaConnectionOpener(db *gorm.DB) targetConnectionOpener {
	return func(context.Context, *config.Config, string) (*databaseConnection, error) {
		return &databaseConnection{DB: db, Close: func() error { return nil }}, nil
	}
}

func newCLIWorkflowDB(t *testing.T, suffix string, migrate bool) *gorm.DB {
	t.Helper()
	name := strings.ReplaceAll(t.Name()+"_"+suffix, "/", "_")
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open CLI SQLite fixture: %v", err)
	}
	if migrate {
		if err := model.Migrate(db); err != nil {
			t.Fatalf("migrate CLI SQLite fixture: %v", err)
		}
	}
	return db
}

func TestMigrationApplicationUsesLegacySourceAndFullTargetMigrators(t *testing.T) {
	app := newMigrationApplication(nil)
	source := newCLIWorkflowDB(t, "legacy_contract", false)
	target := newCLIWorkflowDB(t, "online_contract", false)

	if err := app.migrateSource(source); err != nil {
		t.Fatalf("source migrator error = %v", err)
	}
	if source.Migrator().HasTable(&model.KnowledgeBase{}) || source.Migrator().HasTable(&model.ChatMessageSource{}) {
		t.Fatal("source migrator created online knowledge-base tables")
	}
	if source.Migrator().HasColumn(&model.LegacyChatSession{}, "scope_type") || source.Migrator().HasColumn(&model.LegacyChatSession{}, "knowledge_base_id") {
		t.Fatal("source migrator created online chat-session columns")
	}

	if err := app.migrateTarget(target); err != nil {
		t.Fatalf("target migrator error = %v", err)
	}
	for _, onlineModel := range []any{&model.KnowledgeBase{}, &model.KnowledgeBaseVideo{}, &model.ChatMessageSource{}} {
		if !target.Migrator().HasTable(onlineModel) {
			t.Fatalf("target migrator did not create %T", onlineModel)
		}
	}
	if !target.Migrator().HasColumn(&model.ChatSession{}, "scope_type") || !target.Migrator().HasColumn(&model.ChatSession{}, "knowledge_base_id") {
		t.Fatal("target migrator did not create chat-session scope columns")
	}
}
