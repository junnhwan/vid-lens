package dbmigration

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// PreflightError reports deterministic, credential-free validation failures.
// Connection setup remains the caller's responsibility, so DSNs never need to
// become part of this error type.
type PreflightError struct {
	Stage    string
	Problems []string
}

func (e *PreflightError) Error() string {
	if e == nil {
		return ""
	}
	return e.Stage + ": " + strings.Join(e.Problems, "; ")
}

// CheckSourceSchema verifies that the connected source can represent every
// model in Catalog. It is intentionally read-only and does not run AutoMigrate.
func CheckSourceSchema(ctx context.Context, db *gorm.DB) error {
	ctx = nonNilContext(ctx)
	if err := pingDatabase(ctx, db); err != nil {
		return fmt.Errorf("source schema preflight: %w", err)
	}

	scoped := db.WithContext(ctx)
	migrator := scoped.Migrator()
	problems := make([]string, 0)
	for _, spec := range Catalog() {
		if !migrator.HasTable(spec.Model) {
			problems = append(problems, "missing table "+spec.Name)
			continue
		}

		statement := &gorm.Statement{DB: scoped}
		if err := statement.Parse(spec.Model); err != nil {
			return fmt.Errorf("source schema preflight: parse model for %s: %w", spec.Name, err)
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName == "" || field.IgnoreMigration {
				continue
			}
			if !migrator.HasColumn(spec.Model, field.DBName) {
				problems = append(problems, "missing column "+spec.Name+"."+field.DBName)
			}
		}
	}
	if len(problems) > 0 {
		return &PreflightError{Stage: "source schema preflight failed", Problems: problems}
	}
	return nil
}

// CollectExactCounts counts unscoped physical rows for exactly the tables in
// Catalog. Tables outside the migration boundary, including the pgvector
// projection table, are deliberately ignored.
func CollectExactCounts(ctx context.Context, db *gorm.DB) (map[string]int64, error) {
	ctx = nonNilContext(ctx)
	if err := pingDatabase(ctx, db); err != nil {
		return nil, fmt.Errorf("collect exact counts: %w", err)
	}

	counts := make(map[string]int64, len(Catalog()))
	for _, spec := range Catalog() {
		var count int64
		if err := db.WithContext(ctx).Table(spec.Name).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("collect exact counts for %s: %w", spec.Name, err)
		}
		counts[spec.Name] = count
	}
	return counts, nil
}

// CheckTargetEmpty refuses to merge a migration into existing business rows.
// This is safer than silently truncating or attempting an implicit upsert.
func CheckTargetEmpty(ctx context.Context, db *gorm.DB) error {
	counts, err := CollectExactCounts(ctx, db)
	if err != nil {
		return fmt.Errorf("target empty preflight: %w", err)
	}

	problems := make([]string, 0)
	for _, spec := range Catalog() {
		if count := counts[spec.Name]; count > 0 {
			problems = append(problems, fmt.Sprintf("%s=%d", spec.Name, count))
		}
	}
	if len(problems) > 0 {
		return &PreflightError{Stage: "target database is not empty", Problems: problems}
	}
	return nil
}

// TargetState describes whether the PostgreSQL business-table namespace is safe
// for an all-or-nothing migration. The pgvector projection is outside Catalog
// and therefore does not affect this state.
type TargetState string

const (
	TargetStateAbsent   TargetState = "absent"
	TargetStateEmpty    TargetState = "empty"
	TargetStateMixed    TargetState = "mixed"
	TargetStateOccupied TargetState = "occupied"
	TargetStateDrifted  TargetState = "drifted"
)

type OccupiedTable struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
}

type TargetReadiness struct {
	State          TargetState     `json:"state"`
	MissingTables  []string        `json:"missing_tables,omitempty"`
	EmptyTables    []string        `json:"empty_tables,omitempty"`
	OccupiedTables []OccupiedTable `json:"occupied_tables,omitempty"`
}

func (r TargetReadiness) Ready() bool {
	return r.State == TargetStateAbsent || r.State == TargetStateEmpty
}

// InspectTargetReadiness is a read-only check for dry-run and execute gates. A
// target is accepted only when every catalog table is absent, or every table is
// present with the expected columns and zero physical rows. Mixed namespaces,
// column drift, and pre-existing business data require explicit operator action.
func InspectTargetReadiness(ctx context.Context, db *gorm.DB) (TargetReadiness, error) {
	ctx = nonNilContext(ctx)
	readiness := TargetReadiness{
		MissingTables:  make([]string, 0),
		EmptyTables:    make([]string, 0),
		OccupiedTables: make([]OccupiedTable, 0),
	}
	if err := pingDatabase(ctx, db); err != nil {
		return readiness, fmt.Errorf("target readiness preflight: %w", err)
	}

	scoped := db.WithContext(ctx)
	migrator := scoped.Migrator()
	columnProblems := make([]string, 0)
	for _, spec := range Catalog() {
		if !migrator.HasTable(spec.Model) {
			readiness.MissingTables = append(readiness.MissingTables, spec.Name)
			continue
		}

		statement := &gorm.Statement{DB: scoped}
		if err := statement.Parse(spec.Model); err != nil {
			return readiness, fmt.Errorf("target readiness preflight: parse model for %s: %w", spec.Name, err)
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName == "" || field.IgnoreMigration {
				continue
			}
			if !migrator.HasColumn(spec.Model, field.DBName) {
				columnProblems = append(columnProblems, "missing column "+spec.Name+"."+field.DBName)
			}
		}

		var count int64
		if err := scoped.Table(spec.Name).Count(&count).Error; err != nil {
			return readiness, fmt.Errorf("target readiness preflight count %s: %w", spec.Name, err)
		}
		if count == 0 {
			readiness.EmptyTables = append(readiness.EmptyTables, spec.Name)
		} else {
			readiness.OccupiedTables = append(readiness.OccupiedTables, OccupiedTable{Name: spec.Name, RowCount: count})
		}
	}

	problems := make([]string, 0)
	switch {
	case len(readiness.OccupiedTables) > 0:
		readiness.State = TargetStateOccupied
		for _, table := range readiness.OccupiedTables {
			problems = append(problems, fmt.Sprintf("%s=%d", table.Name, table.RowCount))
		}
	case len(columnProblems) > 0:
		readiness.State = TargetStateDrifted
		problems = append(problems, columnProblems...)
	case len(readiness.MissingTables) == len(Catalog()):
		readiness.State = TargetStateAbsent
	case len(readiness.EmptyTables) == len(Catalog()):
		readiness.State = TargetStateEmpty
	default:
		readiness.State = TargetStateMixed
		problems = append(problems, fmt.Sprintf(
			"mixed business schema: missing=%d existing_empty=%d",
			len(readiness.MissingTables), len(readiness.EmptyTables),
		))
	}
	if len(problems) > 0 {
		return readiness, &PreflightError{Stage: "target readiness preflight failed", Problems: problems}
	}
	return readiness, nil
}

type relationshipPresence uint8

const (
	relationshipRequired relationshipPresence = iota
	relationshipWhenNotNull
	relationshipWhenNonZero
	relationshipWhenNonEmpty
)

type relationshipDisposition uint8

const (
	// The zero value is intentionally the blocking disposition so specs that do
	// not opt into a retention exception remain strict by default.
	relationshipHistorical relationshipDisposition = 1 + iota
	// Historical references deliberately outlive the referenced row. They are
	// visible in migration evidence but do not make an otherwise valid snapshot
	// unsafe to copy.
	// Job-scoped audit records may outlive task_jobs only after their owning task
	// has been soft-deleted. The same missing job on an active task remains a
	// blocking integrity violation.
	relationshipHistoricalWhenTaskDeleted
)

type relationshipSpec struct {
	childTable      string
	childColumn     string
	parentTable     string
	parentKey       string
	presence        relationshipPresence
	disposition     relationshipDisposition
	retentionReason string
}

// RelationshipFinding contains counts and schema labels only. It deliberately
// excludes row IDs and business content so reports remain safe to retain.
type RelationshipFinding struct {
	Relationship string `json:"relationship"`
	OrphanRows   int64  `json:"orphan_rows"`
	Reason       string `json:"reason,omitempty"`
}

// RelationshipAudit separates data that blocks a migration from expected
// historical references created by documented lifecycle differences.
type RelationshipAudit struct {
	Valid      bool                  `json:"valid"`
	Violations []RelationshipFinding `json:"violations"`
	Warnings   []RelationshipFinding `json:"warnings"`
}

// logicalRelationships are application-level references. The models do not
// consistently declare physical foreign keys, so migration safety cannot rely
// on database constraints alone. Any non-blocking relationship must state why
// its child is intentionally retained longer than its parent.
var logicalRelationships = []relationshipSpec{
	{childTable: "video_assets", childColumn: "delete_owner_job_id", parentTable: "task_cleanup_jobs", parentKey: "id", presence: relationshipWhenNotNull},
	{childTable: "video_tasks", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "video_tasks", childColumn: "asset_id", parentTable: "video_assets", parentKey: "id", presence: relationshipWhenNotNull},
	{childTable: "task_jobs", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipRequired},
	{childTable: "task_jobs", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "task_jobs", childColumn: "retry_budget_id", parentTable: "ai_retry_budgets", parentKey: "budget_id", presence: relationshipWhenNonEmpty},
	{childTable: "task_cleanup_jobs", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipRequired},
	{childTable: "task_cleanup_jobs", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "task_cleanup_jobs", childColumn: "asset_id", parentTable: "video_assets", parentKey: "id", presence: relationshipWhenNotNull},
	{childTable: "video_transcriptions", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipRequired},
	{childTable: "video_transcription_chunks", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipRequired},
	{childTable: "ai_summaries", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipRequired},
	{childTable: "user_ai_profiles", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "video_chunks", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "video_chunks", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipRequired},
	{childTable: "video_rag_indexes", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "video_rag_indexes", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipRequired},
	{childTable: "chat_sessions", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "chat_sessions", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipRequired},
	{childTable: "chat_messages", childColumn: "session_id", parentTable: "chat_sessions", parentKey: "id", presence: relationshipRequired},
	{childTable: "chat_messages", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "ai_call_logs", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "ai_call_logs", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipWhenNonZero},
	{
		childTable: "ai_call_logs", childColumn: "job_id", parentTable: "task_jobs", parentKey: "id", presence: relationshipWhenNonZero,
		disposition: relationshipHistoricalWhenTaskDeleted, retentionReason: "AI call audit logs outlive task job cleanup after task deletion",
	},
	{
		childTable: "ai_call_logs", childColumn: "session_id", parentTable: "chat_sessions", parentKey: "id", presence: relationshipWhenNonZero,
		disposition: relationshipHistorical, retentionReason: "AI call audit logs outlive user-deleted chat sessions",
	},
	{childTable: "ai_retry_budgets", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipWhenNonZero},
	{
		childTable: "ai_retry_budgets", childColumn: "job_id", parentTable: "task_jobs", parentKey: "id", presence: relationshipWhenNonZero,
		disposition: relationshipHistoricalWhenTaskDeleted, retentionReason: "durable retry history outlives task job cleanup after task deletion",
	},
	{childTable: "ai_retry_attempts", childColumn: "budget_id", parentTable: "ai_retry_budgets", parentKey: "budget_id", presence: relationshipRequired},
	{childTable: "ai_usage_ledgers", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "ai_usage_ledgers", childColumn: "task_id", parentTable: "video_tasks", parentKey: "id", presence: relationshipWhenNonZero},
	{
		childTable: "ai_usage_ledgers", childColumn: "job_id", parentTable: "task_jobs", parentKey: "id", presence: relationshipWhenNonZero,
		disposition: relationshipHistoricalWhenTaskDeleted, retentionReason: "auditable usage accounting outlives task job cleanup after task deletion",
	},
	{childTable: "quota_compensations", childColumn: "ledger_id", parentTable: "ai_usage_ledgers", parentKey: "id", presence: relationshipRequired},
	{childTable: "quota_compensations", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
	{childTable: "user_usage_daily", childColumn: "user_id", parentTable: "users", parentKey: "id", presence: relationshipRequired},
}

// AuditLogicalRelationships reports all blocking and historical orphan
// categories. Operational query failures are returned separately from findings.
func AuditLogicalRelationships(ctx context.Context, db *gorm.DB) (RelationshipAudit, error) {
	ctx = nonNilContext(ctx)
	audit := RelationshipAudit{
		Valid:      true,
		Violations: make([]RelationshipFinding, 0),
		Warnings:   make([]RelationshipFinding, 0),
	}
	if err := pingDatabase(ctx, db); err != nil {
		return audit, fmt.Errorf("logical relationship audit: %w", err)
	}

	for _, relationship := range logicalRelationships {
		orphanCount, err := countRelationshipOrphans(ctx, db, relationship)
		if err != nil {
			return audit, fmt.Errorf("logical relationship audit for %s: %w", relationship.label(), err)
		}
		if orphanCount == 0 {
			continue
		}

		warningCount := int64(0)
		switch relationship.disposition {
		case relationshipHistorical:
			warningCount = orphanCount
		case relationshipHistoricalWhenTaskDeleted:
			warningCount, err = countDeletedTaskHistoricalReferences(ctx, db, relationship)
			if err != nil {
				return audit, fmt.Errorf("logical relationship audit historical classification for %s: %w", relationship.label(), err)
			}
		}
		if warningCount > orphanCount {
			return audit, fmt.Errorf("logical relationship audit for %s produced invalid warning count", relationship.label())
		}
		if warningCount > 0 {
			audit.Warnings = append(audit.Warnings, RelationshipFinding{
				Relationship: relationship.label(), OrphanRows: warningCount, Reason: relationship.retentionReason,
			})
		}
		if violationCount := orphanCount - warningCount; violationCount > 0 {
			audit.Violations = append(audit.Violations, RelationshipFinding{
				Relationship: relationship.label(), OrphanRows: violationCount,
			})
		}
	}

	sortRelationshipFindings(audit.Violations)
	sortRelationshipFindings(audit.Warnings)
	audit.Valid = len(audit.Violations) == 0
	return audit, nil
}

// CheckLogicalRelationships is the migration gate. Historical warnings remain
// available through AuditLogicalRelationships but do not block copying.
func CheckLogicalRelationships(ctx context.Context, db *gorm.DB) error {
	audit, err := AuditLogicalRelationships(ctx, db)
	if err != nil {
		return err
	}
	return relationshipViolationsError(audit)
}

func relationshipViolationsError(audit RelationshipAudit) error {
	if audit.Valid {
		return nil
	}
	problems := make([]string, 0, len(audit.Violations))
	for _, violation := range audit.Violations {
		problems = append(problems, fmt.Sprintf("%s=%d", violation.Relationship, violation.OrphanRows))
	}
	return &PreflightError{Stage: "logical relationship preflight failed", Problems: problems}
}

func countRelationshipOrphans(ctx context.Context, db *gorm.DB, relationship relationshipSpec) (int64, error) {
	childTable := quoteIdentifier(db, relationship.childTable)
	childColumn := quoteIdentifier(db, relationship.childColumn)
	parentTable := quoteIdentifier(db, relationship.parentTable)
	parentKey := quoteIdentifier(db, relationship.parentKey)
	childReference := "c." + childColumn
	presencePredicate := relationship.presencePredicate(childReference)

	query := fmt.Sprintf(
		"SELECT COUNT(*) FROM %s AS c LEFT JOIN %s AS p ON %s = p.%s WHERE (%s) AND p.%s IS NULL",
		childTable, parentTable, childReference, parentKey, presencePredicate, parentKey,
	)
	var count int64
	if err := db.WithContext(ctx).Raw(query).Scan(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// countDeletedTaskHistoricalReferences narrows job-reference warnings to rows
// whose owning video task has already been soft-deleted. This prevents a missing
// task job on an active task from being mislabeled as harmless history.
func countDeletedTaskHistoricalReferences(ctx context.Context, db *gorm.DB, relationship relationshipSpec) (int64, error) {
	childTable := quoteIdentifier(db, relationship.childTable)
	childColumn := quoteIdentifier(db, relationship.childColumn)
	parentTable := quoteIdentifier(db, relationship.parentTable)
	parentKey := quoteIdentifier(db, relationship.parentKey)
	taskTable := quoteIdentifier(db, "video_tasks")
	taskID := quoteIdentifier(db, "task_id")
	id := quoteIdentifier(db, "id")
	deletedAt := quoteIdentifier(db, "deleted_at")
	childReference := "c." + childColumn
	presencePredicate := relationship.presencePredicate(childReference)

	query := fmt.Sprintf(
		"SELECT COUNT(*) FROM %s AS c LEFT JOIN %s AS p ON %s = p.%s "+
			"INNER JOIN %s AS task_owner ON c.%s = task_owner.%s "+
			"WHERE (%s) AND p.%s IS NULL AND task_owner.%s IS NOT NULL",
		childTable, parentTable, childReference, parentKey,
		taskTable, taskID, id,
		presencePredicate, parentKey, deletedAt,
	)
	var count int64
	if err := db.WithContext(ctx).Raw(query).Scan(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r relationshipSpec) presencePredicate(childReference string) string {
	switch r.presence {
	case relationshipWhenNotNull:
		return childReference + " IS NOT NULL"
	case relationshipWhenNonZero:
		return childReference + " <> 0"
	case relationshipWhenNonEmpty:
		return childReference + " <> ''"
	default:
		return "1 = 1"
	}
}

func (r relationshipSpec) label() string {
	return r.childTable + "." + r.childColumn + " -> " + r.parentTable + "." + r.parentKey
}

func sortRelationshipFindings(findings []RelationshipFinding) {
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Relationship < findings[j].Relationship
	})
}

func quoteIdentifier(db *gorm.DB, identifier string) string {
	var builder strings.Builder
	db.Dialector.QuoteTo(&builder, identifier)
	return builder.String()
}

func pingDatabase(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get connection pool: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
