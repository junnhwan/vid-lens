package dbmigration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MigrationReportVersion = 3

const (
	MigrationPhaseLoadConfig          = "load_config"
	MigrationPhaseValidateConfig      = "validate_config"
	MigrationPhaseOpenConnections     = "open_connections"
	MigrationPhaseAcquireLock         = "acquire_migration_lock"
	MigrationPhaseReleaseLock         = "release_migration_lock"
	MigrationPhaseRelationalPreflight = "relational_preflight"
	MigrationPhasePrepareTarget       = "prepare_target"
	MigrationPhaseCopyRelationalData  = "copy_relational_data"
	MigrationPhaseCloseBeforeAudit    = "close_before_independent_audit"
	MigrationPhaseReopenConnections   = "reopen_connections"
	MigrationPhaseIndependentAudit    = "independent_relational_audit"
	MigrationPhaseUpgradeSourceSchema = "upgrade_source_schema"
	MigrationPhaseComplete            = "complete"

	MigrationCompletionComplete          = "complete"
	MigrationCompletionFailed            = "failed"
	MigrationCompletionNotCommitted      = "relational_not_committed"
	MigrationCompletionAuditPending      = "relational_committed_audit_pending"
	MigrationCompletionRelationalAudited = "relational_committed_and_audited"
)

// DatabaseEndpoint deliberately excludes database names, usernames, passwords,
// and DSNs. Host and port are enough to identify local migration evidence
// without turning the report into a credential artifact.
type DatabaseEndpoint struct {
	Dialect string `json:"dialect"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

// SourceAudit records read-only source evidence collected from one consistent
// snapshot before any target write is allowed.
type SourceAudit struct {
	Tables        []TableDigest     `json:"tables"`
	Relationships RelationshipAudit `json:"relationships"`
}

// MigrationReport is intentionally composed only of credential-free summaries.
// Runtime errors remain on stderr because driver errors can contain connection
// or SQL details that do not belong in a durable report.
type MigrationReport struct {
	Version         int              `json:"version"`
	GeneratedAt     time.Time        `json:"generated_at"`
	StartedAt       time.Time        `json:"started_at"`
	CompletedAt     time.Time        `json:"completed_at"`
	Mode            string           `json:"mode"`
	TargetSchema    string           `json:"target_schema"`
	Phase           string           `json:"phase"`
	FailureStage    string           `json:"failure_stage,omitempty"`
	CompletionState string           `json:"completion_state"`
	CopyCommitted   bool             `json:"copy_committed"`
	CopyCommittedAt *time.Time       `json:"copy_committed_at,omitempty"`
	Success         bool             `json:"success"`
	Source          DatabaseEndpoint `json:"source"`
	Target          DatabaseEndpoint `json:"target"`
	SourceAudit     *SourceAudit     `json:"source_audit,omitempty"`
	TargetReadiness *TargetReadiness `json:"target_readiness,omitempty"`
	DataAudit       *DataAudit       `json:"data_audit,omitempty"`
	Sequences       []SequenceState  `json:"sequences,omitempty"`
}

// WriteMigrationReport writes a complete JSON document to a private temporary
// file in the destination directory, syncs it, and atomically renames it into
// place. A failed encode never leaves a partially written final report.
func WriteMigrationReport(path string, report MigrationReport) (err error) {
	if strings.TrimSpace(path) == "" {
		return errors.New("write migration report: path is required")
	}
	if report.Version == 0 {
		report.Version = MigrationReportVersion
	}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = time.Now().UTC()
	} else {
		report.GeneratedAt = report.GeneratedAt.UTC()
	}
	if !report.StartedAt.IsZero() {
		report.StartedAt = report.StartedAt.UTC()
	}
	if !report.CompletedAt.IsZero() {
		report.CompletedAt = report.CompletedAt.UTC()
	}
	if report.CopyCommittedAt != nil {
		committedAt := report.CopyCommittedAt.UTC()
		report.CopyCommittedAt = &committedAt
	}

	cleanPath := filepath.Clean(path)
	directory := filepath.Dir(cleanPath)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("write migration report: create directory: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".mysql-to-postgres-report-*.tmp")
	if err != nil {
		return fmt.Errorf("write migration report: create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("write migration report: protect temporary file: %w", err)
	}

	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write migration report: encode JSON: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("write migration report: sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write migration report: close temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, cleanPath); err != nil {
		return fmt.Errorf("write migration report: publish atomically: %w", err)
	}
	return nil
}
