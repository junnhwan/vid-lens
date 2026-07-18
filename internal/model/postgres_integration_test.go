package model

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresMigrateCreatesPortableSchema(t *testing.T) {
	db, schemaName := openPostgresModelTestDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() on PostgreSQL error = %v", err)
	}

	var tableCount int64
	if err := db.Raw(`
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = ? AND table_type = 'BASE TABLE'`, schemaName).Scan(&tableCount).Error; err != nil {
		t.Fatalf("count migrated tables: %v", err)
	}
	if want := int64(len(AllModels())); tableCount != want {
		t.Fatalf("migrated table count = %d, want %d", tableCount, want)
	}

	checks := []struct {
		table    string
		column   string
		dataType string
		udtName  string
	}{
		{table: "chat_messages", column: "content", dataType: "text", udtName: "text"},
		{table: "ai_summaries", column: "content", dataType: "text", udtName: "text"},
		{table: "video_transcriptions", column: "content", dataType: "text", udtName: "text"},
		{table: "video_transcription_chunks", column: "content", dataType: "text", udtName: "text"},
		{table: "video_tasks", column: "status", dataType: "smallint", udtName: "int2"},
		{table: "task_jobs", column: "status", dataType: "smallint", udtName: "int2"},
		{table: "video_rag_indexes", column: "chunk_manifest_sha256", dataType: "character varying", udtName: "varchar"},
		{table: "kafka_message_failures", column: "message_key", dataType: "bytea", udtName: "bytea"},
		{table: "kafka_message_failures", column: "payload", dataType: "bytea", udtName: "bytea"},
		{table: "chat_messages", column: "retrieval_snapshot", dataType: "json", udtName: "json"},
	}
	for _, check := range checks {
		t.Run(check.table+"."+check.column, func(t *testing.T) {
			var got struct {
				DataType string `gorm:"column:data_type"`
				UDTName  string `gorm:"column:udt_name"`
			}
			err := db.Raw(`
SELECT data_type, udt_name
FROM information_schema.columns
WHERE table_schema = ? AND table_name = ? AND column_name = ?`,
				schemaName, check.table, check.column,
			).Scan(&got).Error
			if err != nil {
				t.Fatalf("query column type: %v", err)
			}
			if got.DataType != check.dataType || got.UDTName != check.udtName {
				t.Fatalf("type = (%q, %q), want (%q, %q)", got.DataType, got.UDTName, check.dataType, check.udtName)
			}
		})
	}
}

func openPostgresModelTestDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("VIDLENS_POSTGRES_INTEGRATION_DSN"))
	if dsn == "" {
		t.Skip("VIDLENS_POSTGRES_INTEGRATION_DSN is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse integration DSN: %v", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		t.Fatalf("integration DSN scheme = %q, want postgres or postgresql", parsed.Scheme)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL integration database: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatalf("get PostgreSQL integration pool: %v", err)
	}
	t.Cleanup(func() { _ = adminSQL.Close() })

	schemaName := fmt.Sprintf("vidlens_model_test_%d", time.Now().UnixNano())
	quotedSchema := `"` + schemaName + `"`
	if err := admin.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatalf("create integration schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error; err != nil {
			t.Errorf("drop integration schema: %v", err)
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
	return db, schemaName
}
