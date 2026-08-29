package dbmigration

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// CopyOptions controls bounded statement size. Copy deliberately has no
// overwrite or resume option: a migration either commits all catalog tables or
// leaves the target empty.
type CopyOptions struct {
	BatchSize int
}

// CopyResult contains the same credential-free table summaries that were
// compared before commit, plus PostgreSQL sequence state established by the
// migration transaction.
type CopyResult struct {
	Tables    []TableComparison `json:"tables"`
	Sequences []SequenceState   `json:"sequences"`
}

// SequenceState records the post-copy state selected for one auto-increment
// table. Empty tables use Value=1 and IsCalled=false, so their next generated ID
// is 1; non-empty tables use the copied maximum ID and IsCalled=true.
type SequenceState struct {
	Table    string `json:"table"`
	Sequence string `json:"sequence"`
	MaxID    *int64 `json:"max_id,omitempty"`
	Value    int64  `json:"value"`
	IsCalled bool   `json:"is_called"`
}

// Copy performs the complete business-table migration in one source snapshot
// and one target transaction. It never creates, truncates, or silently repairs
// schemas; callers must pass preflighted databases whose catalog tables exist.
func Copy(ctx context.Context, source, target *gorm.DB, options CopyOptions) (*CopyResult, error) {
	ctx = nonNilContext(ctx)
	if options.BatchSize <= 0 {
		return nil, fmt.Errorf("copy migration: batch size must be greater than zero")
	}
	if source == nil {
		return nil, fmt.Errorf("copy migration: source database is nil")
	}
	if target == nil {
		return nil, fmt.Errorf("copy migration: target database is nil")
	}

	sourceSnapshot, err := beginSourceSnapshot(ctx, source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sourceSnapshot.Rollback().Error }()

	if err := CheckSourceSchema(ctx, sourceSnapshot); err != nil {
		return nil, err
	}
	if err := CheckLogicalRelationships(ctx, sourceSnapshot); err != nil {
		return nil, err
	}

	targetTransaction := target.WithContext(ctx).Begin()
	if targetTransaction.Error != nil {
		return nil, fmt.Errorf("begin target write transaction: %w", targetTransaction.Error)
	}
	targetCommitted := false
	defer func() {
		if !targetCommitted {
			_ = targetTransaction.Rollback().Error
		}
	}()

	if err := CheckTargetEmpty(ctx, targetTransaction); err != nil {
		return nil, err
	}
	for _, spec := range Catalog() {
		if err := copyTableInBatches(ctx, sourceSnapshot, targetTransaction, spec, options.BatchSize); err != nil {
			return nil, err
		}
	}

	dataAudit, err := AuditData(ctx, sourceSnapshot, targetTransaction)
	if err != nil {
		return nil, err
	}
	result := &CopyResult{Tables: dataAudit.Tables}

	if _, err = resetTargetSequences(ctx, targetTransaction); err != nil {
		return nil, err
	}
	if targetTransaction.Dialector.Name() == "postgres" {
		result.Sequences, err = AuditSequences(ctx, targetTransaction)
		if err != nil {
			return nil, err
		}
	}
	if err := targetTransaction.Commit().Error; err != nil {
		return nil, fmt.Errorf("commit target migration transaction: %w", err)
	}
	targetCommitted = true
	return result, nil
}

// beginSourceSnapshot is intentionally kept in this package so normal runtime
// code cannot reuse the one-off migration boundary. MySQL and PostgreSQL both
// honor database/sql's repeatable-read, read-only transaction options. SQLite
// is accepted only as the fast unit-test dialect and cannot prove read-only
// enforcement; the PostgreSQL integration test covers that contract for a real
// transactional database.
func beginSourceSnapshot(ctx context.Context, source *gorm.DB) (*gorm.DB, error) {
	if source == nil {
		return nil, fmt.Errorf("begin source snapshot: database is nil")
	}
	var transaction *gorm.DB
	if source.Dialector.Name() == "sqlite" {
		transaction = source.WithContext(nonNilContext(ctx)).Begin()
	} else {
		transaction = source.WithContext(nonNilContext(ctx)).Begin(&sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
			ReadOnly:  true,
		})
	}
	if transaction.Error != nil {
		return nil, fmt.Errorf("begin repeatable-read, read-only source snapshot: %w", transaction.Error)
	}
	return transaction, nil
}

func copyTableInBatches(ctx context.Context, source, target *gorm.DB, spec TableSpec, batchSize int) error {
	columns, err := digestColumns(source, spec)
	if err != nil {
		return err
	}
	sourceColumnNames := make([]string, len(columns))
	targetColumnNames := make([]string, len(columns))
	for i, column := range columns {
		sourceColumnNames[i] = quoteIdentifier(source, column.name)
		targetColumnNames[i] = quoteIdentifier(target, column.name)
	}

	for offset := 0; ; offset += batchSize {
		selectQuery := fmt.Sprintf(
			"SELECT %s FROM %s ORDER BY %s ASC LIMIT %d OFFSET %d",
			strings.Join(sourceColumnNames, ", "),
			quoteIdentifier(source, spec.Name),
			quoteIdentifier(source, spec.PrimaryKey),
			batchSize,
			offset,
		)
		rows, err := source.WithContext(ctx).Raw(selectQuery).Rows()
		if err != nil {
			return fmt.Errorf("copy table %q read batch at offset %d: %w", spec.Name, offset, err)
		}

		batch := make([][]any, 0, batchSize)
		for rows.Next() {
			rawValues := make([]any, len(columns))
			destinations := make([]any, len(columns))
			for i := range rawValues {
				destinations[i] = &rawValues[i]
			}
			if err := rows.Scan(destinations...); err != nil {
				_ = rows.Close()
				return fmt.Errorf("copy table %q scan batch row at offset %d: %w", spec.Name, offset+len(batch), err)
			}

			prepared := make([]any, len(columns))
			for i, column := range columns {
				prepared[i], err = prepareMigrationValue(rawValues[i], column.kind)
				if err != nil {
					_ = rows.Close()
					return fmt.Errorf(
						"copy table %q prepare row at offset %d column %q: %w",
						spec.Name, offset+len(batch), column.name, err,
					)
				}
			}
			batch = append(batch, prepared)
		}
		iterationErr := rows.Err()
		closeErr := rows.Close()
		if iterationErr != nil {
			return fmt.Errorf("copy table %q iterate batch at offset %d: %w", spec.Name, offset, iterationErr)
		}
		if closeErr != nil {
			return fmt.Errorf("copy table %q close batch at offset %d: %w", spec.Name, offset, closeErr)
		}
		if len(batch) == 0 {
			return nil
		}

		rowPlaceholders := "(" + strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",") + ")"
		placeholders := make([]string, len(batch))
		arguments := make([]any, 0, len(batch)*len(columns))
		for i, row := range batch {
			placeholders[i] = rowPlaceholders
			arguments = append(arguments, row...)
		}
		insertQuery := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES %s",
			quoteIdentifier(target, spec.Name),
			strings.Join(targetColumnNames, ", "),
			strings.Join(placeholders, ", "),
		)
		if err := target.WithContext(ctx).Exec(insertQuery, arguments...).Error; err != nil {
			return fmt.Errorf("copy table %q write batch at offset %d: %w", spec.Name, offset, err)
		}
	}
}

func prepareMigrationValue(value any, kind digestValueKind) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch kind {
	case digestValueString:
		return digestText(value)
	case digestValueBytes:
		return digestBytes(value)
	case digestValueBool:
		text, err := digestBool(value)
		if err != nil {
			return nil, err
		}
		return strconv.ParseBool(text)
	case digestValueInteger:
		text, err := digestInteger(value)
		if err != nil {
			return nil, err
		}
		integer, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("integer outside signed 64-bit range: %w", err)
		}
		return integer, nil
	case digestValueDecimal:
		return digestDecimal(value)
	case digestValueTime:
		switch typed := value.(type) {
		case time.Time:
			return typed.Round(0), nil
		case []byte:
			return parseDigestTime(string(typed))
		case string:
			return parseDigestTime(typed)
		default:
			return nil, fmt.Errorf("expected time value, got %T", value)
		}
	case digestValueJSON:
		return digestText(value)
	default:
		return nil, fmt.Errorf("unsupported migration value kind %d", kind)
	}
}

func resetTargetSequences(ctx context.Context, target *gorm.DB) ([]SequenceState, error) {
	if target.Dialector.Name() != "postgres" {
		return nil, nil
	}

	states := make([]SequenceState, 0)
	for _, spec := range Catalog() {
		if !spec.AutoIncrement {
			continue
		}
		var maximum sql.NullInt64
		query := fmt.Sprintf(
			"SELECT MAX(%s) FROM %s",
			quoteIdentifier(target, spec.PrimaryKey),
			quoteIdentifier(target, spec.Name),
		)
		if err := target.WithContext(ctx).Raw(query).Scan(&maximum).Error; err != nil {
			return nil, fmt.Errorf("reset sequence %q for table %q read maximum ID: %w", spec.SequenceName, spec.Name, err)
		}

		state := SequenceState{
			Table:    spec.Name,
			Sequence: spec.SequenceName,
			Value:    1,
			IsCalled: false,
		}
		if maximum.Valid {
			maxID := maximum.Int64
			state.MaxID = &maxID
			state.Value = maxID
			state.IsCalled = true
		}
		var appliedValue int64
		if err := target.WithContext(ctx).
			Raw("SELECT setval(CAST(? AS regclass), ?, ?)", spec.SequenceName, state.Value, state.IsCalled).
			Scan(&appliedValue).Error; err != nil {
			return nil, fmt.Errorf("reset sequence %q for table %q: %w", spec.SequenceName, spec.Name, err)
		}
		if appliedValue != state.Value {
			return nil, fmt.Errorf("reset sequence %q for table %q: applied value %d, want %d", spec.SequenceName, spec.Name, appliedValue, state.Value)
		}
		states = append(states, state)
	}
	return states, nil
}
