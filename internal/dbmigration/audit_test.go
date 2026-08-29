package dbmigration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestAuditDataReportsAllTableMismatchesInCatalogOrder(t *testing.T) {
	source := newCopySQLiteDB(t, "audit_source")
	target := newCopySQLiteDB(t, "audit_target")
	now := time.Date(2026, 7, 17, 3, 4, 5, 0, time.UTC)

	if err := source.Create(&model.User{ID: 1, Username: "source-user", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("insert source user: %v", err)
	}
	if err := target.Create(&model.User{ID: 1, Username: "target-user", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("insert target user: %v", err)
	}
	if err := target.Create(&model.VideoAsset{
		ID: 2, FileMD5: "cccccccccccccccccccccccccccccccc", ObjectName: "target-only.mp4",
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("insert target-only asset: %v", err)
	}

	report, err := AuditData(context.Background(), source, target)
	if err == nil {
		t.Fatal("AuditData() error = nil, want table mismatch error")
	}
	if report == nil || len(report.Tables) != len(Catalog()) {
		t.Fatalf("audit report tables = %v, want %d catalog entries", report, len(Catalog()))
	}
	if report.Tables[0].Table != "users" || report.Tables[1].Table != "video_assets" {
		t.Fatalf("audit report order starts [%q %q], want catalog order", report.Tables[0].Table, report.Tables[1].Table)
	}
	for _, want := range []string{"users digest", "video_assets row count", "video_assets digest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("AuditData() error = %q, want %q", err, want)
		}
	}
	if report.Tables[0].Matches || report.Tables[1].Matches {
		t.Fatalf("mismatch flags = users:%v assets:%v, want both false", report.Tables[0].Matches, report.Tables[1].Matches)
	}
	if len(report.Tables[0].ColumnMismatches) != 1 || report.Tables[0].ColumnMismatches[0].Column != "username" {
		t.Fatalf("users column mismatches = %+v, want username only", report.Tables[0].ColumnMismatches)
	}
	encodedMismatches := fmt.Sprintf("%+v", report.Tables[0].ColumnMismatches)
	if strings.Contains(encodedMismatches, "source-user") || strings.Contains(encodedMismatches, "target-user") {
		t.Fatalf("column mismatch diagnostics leaked values: %s", encodedMismatches)
	}
	if !strings.Contains(err.Error(), "users.username") {
		t.Fatalf("AuditData() error = %q, want users.username diagnostic", err)
	}
	if !report.Tables[2].Matches {
		t.Fatalf("empty equivalent table %q marked as mismatch", report.Tables[2].Table)
	}
}

func TestAuditDataAcceptsEquivalentSoftDeletedRows(t *testing.T) {
	source := newCopySQLiteDB(t, "audit_equal_source")
	target := newCopySQLiteDB(t, "audit_equal_target")
	now := time.Date(2026, 7, 17, 3, 4, 5, 678000000, time.UTC)
	fixture := model.User{
		ID: 9, Username: "same-deleted", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now,
		DeletedAt: gorm.DeletedAt{Time: now.Add(time.Hour), Valid: true},
	}
	if err := source.Unscoped().Create(&fixture).Error; err != nil {
		t.Fatalf("insert source soft-deleted user: %v", err)
	}
	if err := target.Unscoped().Create(&fixture).Error; err != nil {
		t.Fatalf("insert target soft-deleted user: %v", err)
	}

	report, err := AuditData(context.Background(), source, target)
	if err != nil {
		t.Fatalf("AuditData() error = %v", err)
	}
	if report == nil || len(report.Tables) != len(Catalog()) {
		t.Fatalf("audit report = %+v, want %d tables", report, len(Catalog()))
	}
	for _, table := range report.Tables {
		if !table.Matches {
			t.Errorf("equivalent table %q marked as mismatch", table.Table)
		}
	}
	if !report.SourceRelationships.Valid || !report.TargetRelationships.Valid {
		t.Fatalf("relationship audits = source:%+v target:%+v, want valid", report.SourceRelationships, report.TargetRelationships)
	}
}

func TestPostgresAuditSequencesRejectsSequenceThatCanReuseCopiedID(t *testing.T) {
	db := newCopyPostgresDB(t)
	if err := model.Migrate(db); err != nil {
		t.Fatalf("migrate PostgreSQL sequence audit fixture: %v", err)
	}
	now := time.Date(2026, 7, 17, 3, 4, 5, 0, time.UTC)
	user := &model.User{ID: 50, Username: "sequence-lag", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}
	if err := db.Session(&gorm.Session{SkipHooks: true}).Select("*").Create(user).Error; err != nil {
		t.Fatalf("insert explicit-ID sequence fixture: %v", err)
	}

	states, err := AuditSequences(context.Background(), db)
	if err == nil {
		t.Fatal("AuditSequences() error = nil, want lagging users sequence")
	}
	if len(states) == 0 {
		t.Fatal("AuditSequences() returned no sequence states")
	}
	if !strings.Contains(err.Error(), "users_id_seq") || !strings.Contains(err.Error(), "next value") {
		t.Fatalf("AuditSequences() error = %q, want users sequence next-value evidence", err)
	}

	transaction := db.Begin()
	if transaction.Error != nil {
		t.Fatalf("begin sequence reset transaction: %v", transaction.Error)
	}
	if _, resetErr := resetTargetSequences(context.Background(), transaction); resetErr != nil {
		_ = transaction.Rollback().Error
		t.Fatalf("resetTargetSequences() error = %v", resetErr)
	}
	if commitErr := transaction.Commit().Error; commitErr != nil {
		t.Fatalf("commit sequence reset: %v", commitErr)
	}
	if _, err := AuditSequences(context.Background(), db); err != nil {
		t.Fatalf("AuditSequences() after reset error = %v", err)
	}
}

func TestAuditDataAcceptsKnowledgeBaseSessionWithZeroLegacyTaskID(t *testing.T) {
	source, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"_source?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	target, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"_target?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	if err := model.MigrateLegacy(source); err != nil {
		t.Fatalf("MigrateLegacy(source): %v", err)
	}
	if err := model.Migrate(target); err != nil {
		t.Fatalf("Migrate(target): %v", err)
	}
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	for _, db := range []*gorm.DB{source, target} {
		if err := db.Create(&model.User{ID: 1, Username: "audit-kb", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	legacy := model.LegacyChatSession{ID: 1, UserID: 1, TaskID: 0, Title: "kb", CreatedAt: now, UpdatedAt: now}
	if err := source.Create(&legacy).Error; err != nil {
		t.Fatalf("create legacy session: %v", err)
	}
	if err := target.Create(&model.ChatSession{
		ID: 1, UserID: 1, TaskID: 0, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: 9, Title: "kb", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create target session: %v", err)
	}

	report, err := AuditData(context.Background(), source, target)
	if err != nil {
		t.Fatalf("AuditData() error = %v; report=%+v", err, report)
	}
	if !report.SourceRelationships.Valid || !report.TargetRelationships.Valid {
		t.Fatalf("relationship audits = source %+v target %+v", report.SourceRelationships, report.TargetRelationships)
	}
}
