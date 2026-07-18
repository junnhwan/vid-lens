package dbmigration

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// DryRunResult is the credential-free evidence produced without creating or
// modifying target business tables. Vector projection readiness is audited by
// cmd/rag-audit after the relational copy and cmd/rag-reindex stages.
type DryRunResult struct {
	Source *SourceAudit
	Target *TargetReadiness
}

// ExistingMigrationAudit is a post-copy verification result collected after
// reconnecting to the source and target. It deliberately reuses AuditData and
// AuditSequences so relational correctness has one definition.
type ExistingMigrationAudit struct {
	Data      *DataAudit
	Sequences []SequenceState
}

// DryRun inspects one repeatable-read, read-only source snapshot and a target
// namespace. It never creates, migrates, truncates, or updates target tables.
// Vector state is deliberately outside this relational migration boundary.
func DryRun(
	ctx context.Context,
	source *gorm.DB,
	target *gorm.DB,
) (*DryRunResult, error) {
	ctx = nonNilContext(ctx)
	result := &DryRunResult{}
	if target == nil {
		return result, fmt.Errorf("migration dry-run: target database is nil")
	}

	err := withSourceSnapshot(ctx, source, func(snapshot *gorm.DB) error {
		sourceAudit, err := auditSourceSnapshot(ctx, snapshot)
		result.Source = sourceAudit
		if err != nil {
			return err
		}

		readiness, readinessErr := InspectTargetReadiness(ctx, target)
		result.Target = &readiness
		return readinessErr
	})
	if err != nil {
		return result, fmt.Errorf("migration dry-run: %w", err)
	}
	return result, nil
}

// AuditExistingMigration independently verifies an already copied PostgreSQL
// target. Callers should close and reopen target connections before invoking it.
// The vector projection is verified separately by cmd/rag-audit.
func AuditExistingMigration(
	ctx context.Context,
	source *gorm.DB,
	target *gorm.DB,
) (*ExistingMigrationAudit, error) {
	ctx = nonNilContext(ctx)
	result := &ExistingMigrationAudit{}
	if target == nil {
		return result, fmt.Errorf("existing migration audit: target database is nil")
	}

	err := withSourceSnapshot(ctx, source, func(snapshot *gorm.DB) error {
		if err := CheckSourceSchema(ctx, snapshot); err != nil {
			return err
		}
		dataAudit, dataErr := AuditData(ctx, snapshot, target)
		result.Data = dataAudit
		sequences, sequenceErr := AuditSequences(ctx, target)
		result.Sequences = sequences
		return errors.Join(dataErr, sequenceErr)
	})
	if err != nil {
		return result, fmt.Errorf("existing migration audit: %w", err)
	}
	return result, nil
}

func auditSourceSnapshot(ctx context.Context, snapshot *gorm.DB) (*SourceAudit, error) {
	report := &SourceAudit{Tables: make([]TableDigest, 0, len(Catalog()))}
	if err := CheckSourceSchema(ctx, snapshot); err != nil {
		return report, err
	}
	relationships, err := AuditLogicalRelationships(ctx, snapshot)
	report.Relationships = relationships
	if err != nil {
		return report, err
	}
	if err := relationshipViolationsError(relationships); err != nil {
		return report, err
	}
	for _, spec := range Catalog() {
		digest, err := DigestTable(ctx, snapshot, spec)
		if err != nil {
			return report, fmt.Errorf("source snapshot audit: %w", err)
		}
		report.Tables = append(report.Tables, digest)
	}
	return report, nil
}

func withSourceSnapshot(ctx context.Context, source *gorm.DB, inspect func(*gorm.DB) error) error {
	if inspect == nil {
		return fmt.Errorf("source snapshot inspection callback is nil")
	}
	snapshot, err := beginSourceSnapshot(ctx, source)
	if err != nil {
		return err
	}
	defer func() { _ = snapshot.Rollback().Error }()
	return inspect(snapshot)
}
