package dbmigration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestCopyPreservesExplicitIDsAndSoftDeletedRows(t *testing.T) {
	source := newCopySQLiteDB(t, "source")
	target := newCopySQLiteDB(t, "target")
	now := time.Date(2026, 7, 17, 2, 3, 4, 567000000, time.UTC)
	users := []model.User{
		{ID: 41, Username: "active-copy", PasswordHash: "hash-active", CreatedAt: now, UpdatedAt: now},
		{ID: 97, Username: "deleted-copy", PasswordHash: "hash-deleted", CreatedAt: now, UpdatedAt: now, DeletedAt: gorm.DeletedAt{Time: now.Add(time.Hour), Valid: true}},
	}
	for i := range users {
		if err := source.Unscoped().Create(&users[i]).Error; err != nil {
			t.Fatalf("insert source user %d: %v", users[i].ID, err)
		}
	}

	result, err := Copy(context.Background(), source, target, CopyOptions{BatchSize: 1})
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if len(result.Tables) != len(Catalog()) {
		t.Fatalf("audited tables = %d, want %d", len(result.Tables), len(Catalog()))
	}

	var copied []model.User
	if err := target.Unscoped().Order("id ASC").Find(&copied).Error; err != nil {
		t.Fatalf("read copied users: %v", err)
	}
	if len(copied) != 2 {
		t.Fatalf("copied users = %d, want 2", len(copied))
	}
	if copied[0].ID != 41 || copied[1].ID != 97 {
		t.Fatalf("copied IDs = [%d %d], want [41 97]", copied[0].ID, copied[1].ID)
	}
	if copied[0].DeletedAt.Valid || !copied[1].DeletedAt.Valid {
		t.Fatalf("copied deleted state = [%v %v], want [false true]", copied[0].DeletedAt.Valid, copied[1].DeletedAt.Valid)
	}
}

func TestPrepareMigrationValuePreservesNullAndCoercesDriverValues(t *testing.T) {
	if value, err := prepareMigrationValue(nil, digestValueString); err != nil || value != nil {
		t.Fatalf("prepare NULL = %#v, %v; want nil, nil", value, err)
	}
	cases := []struct {
		name  string
		value any
		kind  digestValueKind
		want  any
	}{
		{name: "string bytes", value: []byte("text"), kind: digestValueString, want: "text"},
		{name: "bytes", value: []byte{0, 1, 2}, kind: digestValueBytes, want: []byte{0, 1, 2}},
		{name: "boolean integer", value: int64(1), kind: digestValueBool, want: true},
		{name: "integer bytes", value: []byte("42"), kind: digestValueInteger, want: int64(42)},
		{name: "decimal bytes", value: []byte("001.2300"), kind: digestValueDecimal, want: "1.23"},
		{name: "JSON bytes", value: []byte(`{"a":1}`), kind: digestValueJSON, want: `{"a":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := prepareMigrationValue(tc.value, tc.kind)
			if err != nil {
				t.Fatalf("prepareMigrationValue() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("prepareMigrationValue() = %#v (%T), want %#v (%T)", got, got, tc.want, tc.want)
			}
		})
	}
}

func TestCopyPreservesSQLNullForNonPointerModelFields(t *testing.T) {
	source := newCopySQLiteDB(t, "null_source")
	target := newCopySQLiteDB(t, "null_target")
	now := time.Date(2026, 7, 17, 2, 3, 4, 0, time.UTC)
	if err := source.Create(&model.User{ID: 1, Username: "null-owner", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("insert source user: %v", err)
	}
	if err := source.Create(&model.VideoTask{
		ID: 1, UserID: 1, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "null-source.mp4",
		SourceURL: "temporary-value", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("insert source task: %v", err)
	}
	if err := source.Exec("UPDATE video_tasks SET source_url = NULL WHERE id = ?", 1).Error; err != nil {
		t.Fatalf("set source_url to SQL NULL: %v", err)
	}

	if _, err := Copy(context.Background(), source, target, CopyOptions{BatchSize: 10}); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	var sourceURL sql.NullString
	if err := target.Raw("SELECT source_url FROM video_tasks WHERE id = ?", 1).Scan(&sourceURL).Error; err != nil {
		t.Fatalf("read copied source_url: %v", err)
	}
	if sourceURL.Valid {
		t.Fatalf("copied source_url = %q, want SQL NULL", sourceURL.String)
	}
}

func TestCopyRollsBackEveryTargetTableWhenMiddleInsertFails(t *testing.T) {
	source := newCopySQLiteDB(t, "rollback_source")
	target := newCopySQLiteDB(t, "rollback_target")
	now := time.Date(2026, 7, 17, 2, 3, 4, 0, time.UTC)

	user := &model.User{ID: 10, Username: "rollback-user", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}
	task := &model.VideoTask{
		ID: 20, UserID: user.ID, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "rollback.mp4",
		CreatedAt: now, UpdatedAt: now,
	}
	transcription := &model.VideoTranscription{ID: 30, TaskID: task.ID, Content: "force target failure", CreatedAt: now}
	for _, fixture := range []any{user, task, transcription} {
		if err := source.Create(fixture).Error; err != nil {
			t.Fatalf("insert source fixture %T: %v", fixture, err)
		}
	}
	if err := target.Exec(`
CREATE TRIGGER fail_video_transcription_insert
BEFORE INSERT ON video_transcriptions
BEGIN
  SELECT RAISE(ABORT, 'forced transcription insert failure');
END`).Error; err != nil {
		t.Fatalf("create target failure trigger: %v", err)
	}

	_, err := Copy(context.Background(), source, target, CopyOptions{BatchSize: 2})
	if err == nil {
		t.Fatal("Copy() error = nil, want forced target failure")
	}
	if !strings.Contains(err.Error(), "video_transcriptions") {
		t.Fatalf("Copy() error = %q, want failing table context", err)
	}

	counts, countErr := CollectExactCounts(context.Background(), target)
	if countErr != nil {
		t.Fatalf("count target after rollback: %v", countErr)
	}
	for table, count := range counts {
		if count != 0 {
			t.Errorf("target table %s rows after rollback = %d, want 0", table, count)
		}
	}
}

func TestCopyRejectsInvalidBatchSizeBeforeOpeningTransactions(t *testing.T) {
	source := newCopySQLiteDB(t, "invalid_source")
	target := newCopySQLiteDB(t, "invalid_target")
	if _, err := Copy(context.Background(), source, target, CopyOptions{}); err == nil || !strings.Contains(err.Error(), "batch size") {
		t.Fatalf("Copy() error = %v, want batch size validation", err)
	}
}

func TestPostgresSourceSnapshotIsReadOnly(t *testing.T) {
	db := newCopyPostgresDB(t)
	if err := model.Migrate(db); err != nil {
		t.Fatalf("migrate PostgreSQL source fixture: %v", err)
	}

	snapshot, err := beginSourceSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("beginSourceSnapshot() error = %v", err)
	}
	defer snapshot.Rollback()

	err = snapshot.Create(&model.User{Username: "must-not-write", PasswordHash: "hash"}).Error
	if err == nil {
		t.Fatal("write through read-only source snapshot succeeded")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "read-only") && !strings.Contains(strings.ToLower(err.Error()), "read only") {
		t.Fatalf("read-only write error = %q, want read-only semantics", err)
	}
}

func TestMySQLSourceSnapshotIsReadOnly(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("VIDLENS_MYSQL_INTEGRATION_DSN"))
	if dsn == "" {
		t.Skip("VIDLENS_MYSQL_INTEGRATION_DSN is not set")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open MySQL integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get MySQL integration pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	snapshot, err := beginSourceSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("beginSourceSnapshot() error = %v", err)
	}
	defer snapshot.Rollback()

	err = snapshot.Create(&model.User{Username: fmt.Sprintf("read-only-%d", time.Now().UnixNano()), PasswordHash: "hash"}).Error
	if err == nil {
		t.Fatal("write through MySQL read-only source snapshot succeeded")
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "read-only") && !strings.Contains(message, "read only") {
		t.Fatalf("MySQL read-only write error = %q, want read-only semantics", err)
	}
}

func TestPostgresCopyResetsSequenceAfterExplicitIDs(t *testing.T) {
	source := newCopySQLiteDB(t, "sequence_source")
	target := newCopyPostgresDB(t)
	if err := model.Migrate(target); err != nil {
		t.Fatalf("migrate PostgreSQL target fixture: %v", err)
	}
	now := time.Date(2026, 7, 17, 2, 3, 4, 0, time.UTC)
	if err := source.Create(&model.User{ID: 500, Username: "explicit-id", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("insert source user: %v", err)
	}

	result, err := Copy(context.Background(), source, target, CopyOptions{BatchSize: 10})
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	var emptySequence *SequenceState
	for i := range result.Sequences {
		if result.Sequences[i].Table == "video_assets" {
			emptySequence = &result.Sequences[i]
			break
		}
	}
	if emptySequence == nil {
		t.Fatal("copy result has no video_assets sequence state")
	}
	if emptySequence.MaxID != nil || emptySequence.Value != 1 || emptySequence.IsCalled {
		t.Fatalf("empty sequence state = %+v, want max=nil value=1 is_called=false", *emptySequence)
	}
	generated := &model.User{Username: "generated-id", PasswordHash: "hash"}
	if err := target.Create(generated).Error; err != nil {
		t.Fatalf("insert post-copy user: %v", err)
	}
	if generated.ID <= 500 {
		t.Fatalf("generated ID = %d, want > 500", generated.ID)
	}
}

func newCopySQLiteDB(t *testing.T, suffix string) *gorm.DB {
	t.Helper()
	name := strings.ReplaceAll(t.Name()+"_"+suffix, "/", "_")
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open SQLite copy fixture: %v", err)
	}
	if err := model.Migrate(db); err != nil {
		t.Fatalf("migrate SQLite copy fixture: %v", err)
	}
	return db
}

func newCopyPostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("VIDLENS_POSTGRES_INTEGRATION_DSN"))
	if dsn == "" {
		t.Skip("VIDLENS_POSTGRES_INTEGRATION_DSN is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL integration DSN: %v", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		t.Fatalf("integration DSN scheme = %q, want postgres or postgresql", parsed.Scheme)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL integration admin: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatalf("get PostgreSQL admin pool: %v", err)
	}
	t.Cleanup(func() { _ = adminSQL.Close() })

	schemaName := fmt.Sprintf("vidlens_dbmigration_test_%d", time.Now().UnixNano())
	quotedSchema := `"` + schemaName + `"`
	if err := admin.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatalf("create PostgreSQL integration schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error; err != nil {
			t.Errorf("drop PostgreSQL integration schema: %v", err)
		}
	})

	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open scoped PostgreSQL integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get scoped PostgreSQL pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
