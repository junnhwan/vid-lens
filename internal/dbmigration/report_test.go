package dbmigration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteMigrationReportIsCredentialFreeAndAtomicallyReplaceable(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "migration.json")
	report := MigrationReport{
		Version:         MigrationReportVersion,
		GeneratedAt:     time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
		StartedAt:       time.Date(2026, 7, 17, 11, 59, 0, 0, time.FixedZone("CST", 8*60*60)),
		CompletedAt:     time.Date(2026, 7, 17, 12, 1, 0, 0, time.FixedZone("CST", 8*60*60)),
		Mode:            "dry-run",
		TargetSchema:    "rehearsal",
		Phase:           MigrationPhaseComplete,
		CompletionState: MigrationCompletionComplete,
		Success:         true,
		Source:          DatabaseEndpoint{Dialect: "mysql", Host: "mysql.local", Port: 3306},
		Target:          DatabaseEndpoint{Dialect: "postgres", Host: "postgres.local", Port: 5432},
		SourceAudit: &SourceAudit{Relationships: RelationshipAudit{Valid: true, Warnings: []RelationshipFinding{{
			Relationship: "ai_call_logs.job_id -> task_jobs.id", OrphanRows: 5, Reason: "retained audit history",
		}}}},
	}
	if err := WriteMigrationReport(path, report); err != nil {
		t.Fatalf("WriteMigrationReport() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration report: %v", err)
	}
	var decoded MigrationReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode migration report: %v\n%s", err, data)
	}
	if decoded.GeneratedAt.Location() != time.UTC || decoded.GeneratedAt.Hour() != 4 {
		t.Fatalf("generated_at = %v, want normalized UTC time", decoded.GeneratedAt)
	}
	if decoded.SourceAudit == nil || len(decoded.SourceAudit.Relationships.Warnings) != 1 || decoded.SourceAudit.Relationships.Warnings[0].OrphanRows != 5 {
		t.Fatalf("decoded relationship warnings = %+v", decoded.SourceAudit)
	}
	for _, forbidden := range []string{
		"password", "username", "dsn", "database_name", "db_name", "api_key",
		"embedding", "chunk_content", "vector_manifest", "secret-value",
	} {
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Errorf("migration report contains forbidden field/value %q: %s", forbidden, data)
		}
	}

	report.Success = false
	if err := WriteMigrationReport(path, report); err != nil {
		t.Fatalf("replace migration report: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("list report directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "migration.json" {
		t.Fatalf("report directory entries = %+v, want only final report", entries)
	}
}

func TestWriteMigrationReportRejectsEmptyPath(t *testing.T) {
	if err := WriteMigrationReport("", MigrationReport{}); err == nil {
		t.Fatal("WriteMigrationReport() error = nil, want empty path rejection")
	}
}

func TestWriteMigrationReportDefaultsVersionAndNormalizesLifecycleTimes(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	started := time.Date(2026, 7, 18, 10, 0, 0, 0, zone)
	committed := started.Add(2 * time.Minute)
	report := MigrationReport{
		StartedAt:       started,
		CompletedAt:     started.Add(3 * time.Minute),
		CopyCommittedAt: &committed,
		Mode:            "execute",
		CompletionState: MigrationCompletionRelationalAudited,
		CopyCommitted:   true,
	}
	path := filepath.Join(t.TempDir(), "migration.json")
	if err := WriteMigrationReport(path, report); err != nil {
		t.Fatalf("WriteMigrationReport() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration report: %v", err)
	}
	var decoded MigrationReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode migration report: %v", err)
	}
	if decoded.Version != MigrationReportVersion {
		t.Fatalf("version = %d, want %d", decoded.Version, MigrationReportVersion)
	}
	for name, got := range map[string]time.Time{
		"generated_at": decoded.GeneratedAt,
		"started_at":   decoded.StartedAt,
		"completed_at": decoded.CompletedAt,
		"copy_committed_at": func() time.Time {
			if decoded.CopyCommittedAt == nil {
				return time.Time{}
			}
			return *decoded.CopyCommittedAt
		}(),
	} {
		if got.IsZero() || got.Location() != time.UTC {
			t.Errorf("%s = %v (%v), want non-zero UTC", name, got, got.Location())
		}
	}
}
