package dbmigration

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"

	"gorm.io/gorm"
)

// TableComparison keeps source and target evidence side by side. Matches is a
// convenience for report consumers; the two full digests remain available for
// diagnosis without reconnecting to either database.
type TableComparison struct {
	Table            string             `json:"table"`
	Source           TableDigest        `json:"source"`
	Target           TableDigest        `json:"target"`
	Matches          bool               `json:"matches"`
	ColumnMismatches []ColumnComparison `json:"column_mismatches,omitempty"`
}

// ColumnComparison localizes a table-level mismatch without exposing cell
// values or primary keys. Only mismatching columns are retained in reports.
type ColumnComparison struct {
	Column  string       `json:"column"`
	Source  ColumnDigest `json:"source"`
	Target  ColumnDigest `json:"target"`
	Matches bool         `json:"matches"`
}

// DataAudit is reusable both inside Copy's target transaction and after a fresh
// reconnect. Keeping it outside the copier prevents the CLI from developing a
// second, subtly different definition of migration correctness.
type DataAudit struct {
	Tables              []TableComparison `json:"tables"`
	SourceRelationships RelationshipAudit `json:"source_relationships"`
	TargetRelationships RelationshipAudit `json:"target_relationships"`
}

// AuditData compares every catalog table and validates logical relationships on
// both sides. Content mismatches are aggregated so one run reports all affected
// tables rather than forcing a repair/retry loop table by table.
func AuditData(ctx context.Context, source, target *gorm.DB) (*DataAudit, error) {
	ctx = nonNilContext(ctx)
	if source == nil {
		return nil, fmt.Errorf("migration data audit: source database is nil")
	}
	if target == nil {
		return nil, fmt.Errorf("migration data audit: target database is nil")
	}

	report := &DataAudit{Tables: make([]TableComparison, 0, len(Catalog()))}
	problems := make([]string, 0)
	for _, spec := range Catalog() {
		sourceDigest, err := DigestTable(ctx, source, spec)
		if err != nil {
			return report, fmt.Errorf("migration data audit source: %w", err)
		}
		targetDigest, err := DigestTable(ctx, target, spec)
		if err != nil {
			return report, fmt.Errorf("migration data audit target: %w", err)
		}

		comparison := TableComparison{
			Table:  spec.Name,
			Source: sourceDigest,
			Target: targetDigest,
		}
		comparison.Matches = sourceDigest.RowCount == targetDigest.RowCount && sourceDigest.SHA256 == targetDigest.SHA256
		report.Tables = append(report.Tables, comparison)
		if sourceDigest.RowCount != targetDigest.RowCount {
			problems = append(problems, fmt.Sprintf(
				"%s row count: source=%d target=%d",
				spec.Name, sourceDigest.RowCount, targetDigest.RowCount,
			))
		}
		if sourceDigest.SHA256 != targetDigest.SHA256 {
			problems = append(problems, fmt.Sprintf(
				"%s digest: source=%s target=%s",
				spec.Name, sourceDigest.SHA256, targetDigest.SHA256,
			))
			// When row counts match, column-level hashes identify a cross-dialect
			// normalization or copy issue without reporting any business values.
			if sourceDigest.RowCount == targetDigest.RowCount {
				columnMismatches, columnErr := auditColumnMismatches(ctx, source, target, spec)
				if columnErr != nil {
					return report, columnErr
				}
				comparison.ColumnMismatches = columnMismatches
				report.Tables[len(report.Tables)-1] = comparison
				for _, column := range columnMismatches {
					problems = append(problems, fmt.Sprintf(
						"%s.%s digest: source=%s target=%s",
						spec.Name, column.Column, column.Source.SHA256, column.Target.SHA256,
					))
				}
			}
		}
	}

	sourceRelationships, sourceRelationshipErr := AuditLogicalRelationships(ctx, source)
	report.SourceRelationships = sourceRelationships
	if sourceRelationshipErr != nil {
		problems = append(problems, "source relationships: "+sourceRelationshipErr.Error())
	} else if violationErr := relationshipViolationsError(sourceRelationships); violationErr != nil {
		problems = append(problems, "source relationships: "+violationErr.Error())
	}
	targetRelationships, targetRelationshipErr := AuditLogicalRelationships(ctx, target)
	report.TargetRelationships = targetRelationships
	if targetRelationshipErr != nil {
		problems = append(problems, "target relationships: "+targetRelationshipErr.Error())
	} else if violationErr := relationshipViolationsError(targetRelationships); violationErr != nil {
		problems = append(problems, "target relationships: "+violationErr.Error())
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return report, &PreflightError{Stage: "migration data audit failed", Problems: problems}
	}
	return report, nil
}

func auditColumnMismatches(ctx context.Context, source, target *gorm.DB, spec TableSpec) ([]ColumnComparison, error) {
	sourceColumns, err := DigestTableColumns(ctx, source, spec)
	if err != nil {
		return nil, fmt.Errorf("migration data audit source column diagnostics: %w", err)
	}
	targetColumns, err := DigestTableColumns(ctx, target, spec)
	if err != nil {
		return nil, fmt.Errorf("migration data audit target column diagnostics: %w", err)
	}
	if len(sourceColumns) != len(targetColumns) {
		return nil, fmt.Errorf(
			"migration data audit table %q column count: source=%d target=%d",
			spec.Name, len(sourceColumns), len(targetColumns),
		)
	}

	mismatches := make([]ColumnComparison, 0)
	for i := range sourceColumns {
		if sourceColumns[i].Column != targetColumns[i].Column {
			return nil, fmt.Errorf(
				"migration data audit table %q column order at %d: source=%q target=%q",
				spec.Name, i, sourceColumns[i].Column, targetColumns[i].Column,
			)
		}
		comparison := ColumnComparison{
			Column: sourceColumns[i].Column,
			Source: sourceColumns[i],
			Target: targetColumns[i],
		}
		comparison.Matches = comparison.Source.RowCount == comparison.Target.RowCount && comparison.Source.SHA256 == comparison.Target.SHA256
		if !comparison.Matches {
			mismatches = append(mismatches, comparison)
		}
	}
	return mismatches, nil
}

// AuditSequences verifies the value that nextval would return, not only the
// stored last_value. PostgreSQL sequences with is_called=false return
// last_value itself, which can otherwise silently reuse an explicitly copied ID.
func AuditSequences(ctx context.Context, target *gorm.DB) ([]SequenceState, error) {
	ctx = nonNilContext(ctx)
	if target == nil {
		return nil, fmt.Errorf("sequence audit: target database is nil")
	}
	if target.Dialector.Name() != "postgres" {
		return nil, fmt.Errorf("sequence audit: target dialect %q is not PostgreSQL", target.Dialector.Name())
	}

	states := make([]SequenceState, 0)
	problems := make([]string, 0)
	for _, spec := range Catalog() {
		if !spec.AutoIncrement {
			continue
		}

		var maximum sql.NullInt64
		maxQuery := fmt.Sprintf(
			"SELECT MAX(%s) FROM %s",
			quoteIdentifier(target, spec.PrimaryKey),
			quoteIdentifier(target, spec.Name),
		)
		if err := target.WithContext(ctx).Raw(maxQuery).Scan(&maximum).Error; err != nil {
			return states, fmt.Errorf("sequence audit %q read table %q maximum ID: %w", spec.SequenceName, spec.Name, err)
		}

		var persisted struct {
			LastValue int64 `gorm:"column:last_value"`
			IsCalled  bool  `gorm:"column:is_called"`
		}
		sequenceQuery := fmt.Sprintf(
			"SELECT last_value, is_called FROM %s",
			quoteIdentifier(target, spec.SequenceName),
		)
		if err := target.WithContext(ctx).Raw(sequenceQuery).Scan(&persisted).Error; err != nil {
			return states, fmt.Errorf("sequence audit %q read state for table %q: %w", spec.SequenceName, spec.Name, err)
		}

		state := SequenceState{
			Table:    spec.Name,
			Sequence: spec.SequenceName,
			Value:    persisted.LastValue,
			IsCalled: persisted.IsCalled,
		}
		if maximum.Valid {
			maxID := maximum.Int64
			state.MaxID = &maxID
			nextValue := persisted.LastValue
			if persisted.IsCalled {
				if persisted.LastValue == math.MaxInt64 {
					problems = append(problems, fmt.Sprintf("%s next value overflows int64 at %d", spec.SequenceName, persisted.LastValue))
					states = append(states, state)
					continue
				}
				nextValue++
			}
			if nextValue <= maxID {
				problems = append(problems, fmt.Sprintf(
					"%s next value %d would not exceed %s.%s maximum ID %d",
					spec.SequenceName, nextValue, spec.Name, spec.PrimaryKey, maxID,
				))
			}
		}
		states = append(states, state)
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return states, &PreflightError{Stage: "sequence audit failed", Problems: problems}
	}
	return states, nil
}
