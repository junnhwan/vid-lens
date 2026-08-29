package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"vid-lens/internal/config"
	appdb "vid-lens/internal/database"
	"vid-lens/internal/dbmigration"
	"vid-lens/internal/model"
)

type databaseConnection struct {
	DB     *gorm.DB
	Close  func() error
	closed bool
}

func (c *databaseConnection) close() error {
	if c == nil || c.closed {
		return nil
	}
	c.closed = true
	if c.Close == nil {
		return nil
	}
	return c.Close()
}

type sourceConnectionOpener func(context.Context, *config.Config) (*databaseConnection, error)
type targetConnectionOpener func(context.Context, *config.Config, string) (*databaseConnection, error)
type migrationLockAcquirer func(context.Context, *gorm.DB) (func(context.Context) error, error)

type migrationApplication struct {
	loadConfig    func(string) (*config.Config, error)
	openSource    sourceConnectionOpener
	openTarget    targetConnectionOpener
	acquireLock   migrationLockAcquirer
	migrateSource func(*gorm.DB) error
	migrateTarget func(*gorm.DB) error
	dryRun        func(context.Context, *gorm.DB, *gorm.DB) (*dbmigration.DryRunResult, error)
	copyData      func(context.Context, *gorm.DB, *gorm.DB, dbmigration.CopyOptions) (*dbmigration.CopyResult, error)
	auditExisting func(context.Context, *gorm.DB, *gorm.DB) (*dbmigration.ExistingMigrationAudit, error)
	writeReport   func(string, dbmigration.MigrationReport) error
	now           func() time.Time
	output        io.Writer
}

func newMigrationApplication(output io.Writer) migrationApplication {
	if output == nil {
		output = io.Discard
	}
	return migrationApplication{
		loadConfig:    config.Load,
		openSource:    openMySQLConnection,
		openTarget:    openPostgresSchemaConnection,
		acquireLock:   dbmigration.TryAcquireMigrationAdvisoryLock,
		migrateSource: model.MigrateLegacy,
		migrateTarget: model.Migrate,
		dryRun:        dbmigration.DryRun,
		copyData:      dbmigration.Copy,
		auditExisting: dbmigration.AuditExistingMigration,
		writeReport:   dbmigration.WriteMigrationReport,
		now:           time.Now,
		output:        output,
	}
}

func (a migrationApplication) run(ctx context.Context, opts options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
	}

	startedAt := a.now().UTC()
	report := dbmigration.MigrationReport{
		Version:      dbmigration.MigrationReportVersion,
		GeneratedAt:  startedAt,
		StartedAt:    startedAt,
		Mode:         string(opts.mode()),
		TargetSchema: opts.targetSchema,
		Phase:        dbmigration.MigrationPhaseLoadConfig,
	}

	cfg, operationErr := a.loadConfig(opts.configPath)
	if cfg != nil {
		report.Source = dbmigration.DatabaseEndpoint{Dialect: "mysql", Host: cfg.LegacyMySQL.Host, Port: cfg.LegacyMySQL.Port}
		report.Target = dbmigration.DatabaseEndpoint{Dialect: "postgres", Host: cfg.Database.Host, Port: cfg.Database.Port}
	}
	if operationErr == nil {
		report.Phase = dbmigration.MigrationPhaseValidateConfig
		operationErr = a.validateConfig(cfg, opts.mode())
	}
	if operationErr == nil {
		switch opts.mode() {
		case modeDryRun:
			operationErr = a.runDryRun(ctx, cfg, opts, &report)
		case modeUpgradeSourceSchema:
			operationErr = a.runSourceUpgrade(ctx, cfg, &report)
		case modeExecute:
			operationErr = a.runExecute(ctx, cfg, opts, &report)
		case modeAudit:
			operationErr = a.runAudit(ctx, cfg, opts, &report)
		default:
			operationErr = fmt.Errorf("unsupported migration mode %q", opts.mode())
		}
	}

	report.CompletedAt = a.now().UTC()
	report.Success = operationErr == nil
	if operationErr != nil {
		report.FailureStage = report.Phase
		if opts.mode() == modeExecute {
			if report.CopyCommitted {
				report.CompletionState = dbmigration.MigrationCompletionAuditPending
			} else {
				report.CompletionState = dbmigration.MigrationCompletionNotCommitted
			}
		} else {
			report.CompletionState = dbmigration.MigrationCompletionFailed
		}
	} else {
		report.Phase = dbmigration.MigrationPhaseComplete
		if opts.mode() == modeExecute {
			report.CompletionState = dbmigration.MigrationCompletionRelationalAudited
		} else {
			report.CompletionState = dbmigration.MigrationCompletionComplete
		}
	}
	reportErr := a.writeReport(opts.reportPath, report)
	if operationErr == nil && reportErr == nil {
		_, _ = fmt.Fprintf(a.output, "migration mode %s completed; report: %s\n", opts.mode(), opts.reportPath)
	}
	return errors.Join(operationErr, reportErr)
}

func (a migrationApplication) validateConfig(cfg *config.Config, mode migrationMode) error {
	if cfg == nil {
		return errors.New("migration config is nil")
	}
	if mode == modeUpgradeSourceSchema {
		return cfg.ValidateMySQL()
	}
	return errors.Join(cfg.ValidateMySQL(), cfg.ValidatePostgres())
}

func (a migrationApplication) runDryRun(ctx context.Context, cfg *config.Config, opts options, report *dbmigration.MigrationReport) (err error) {
	report.Phase = dbmigration.MigrationPhaseOpenConnections
	connections, err := a.openConnections(ctx, cfg, opts.targetSchema)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, connections.close()) }()

	report.Phase = dbmigration.MigrationPhaseRelationalPreflight
	result, err := a.dryRun(ctx, connections.source.DB, connections.target.DB)
	applyDryRunResult(report, result)
	return err
}

func (a migrationApplication) runSourceUpgrade(ctx context.Context, cfg *config.Config, report *dbmigration.MigrationReport) (err error) {
	report.Phase = dbmigration.MigrationPhaseUpgradeSourceSchema
	source, err := a.openSource(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open MySQL source for schema upgrade: %w", err)
	}
	defer func() { err = errors.Join(err, source.close()) }()

	_, _ = fmt.Fprintln(a.output, "WARNING: upgrading MySQL source schema only; rerun dry-run before execute")
	if err := a.migrateSource(source.DB); err != nil {
		return fmt.Errorf("upgrade MySQL source schema: %w", err)
	}
	return nil
}

func (a migrationApplication) runExecute(ctx context.Context, cfg *config.Config, opts options, report *dbmigration.MigrationReport) (err error) {
	report.Phase = dbmigration.MigrationPhaseOpenConnections
	connections, err := a.openConnections(ctx, cfg, opts.targetSchema)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, connections.close()) }()

	report.Phase = dbmigration.MigrationPhaseAcquireLock
	releaseLock, err := a.acquireLock(ctx, connections.target.DB)
	if err != nil {
		return err
	}
	lockHeld := true
	defer func() {
		if lockHeld {
			err = errors.Join(err, releaseLock(ctx))
		}
	}()

	report.Phase = dbmigration.MigrationPhaseRelationalPreflight
	preflight, err := a.dryRun(ctx, connections.source.DB, connections.target.DB)
	applyDryRunResult(report, preflight)
	if err != nil {
		return err
	}
	report.Phase = dbmigration.MigrationPhasePrepareTarget
	if err := ensureTargetSchema(ctx, connections.target.DB, opts.targetSchema); err != nil {
		return err
	}
	if err := a.migrateTarget(connections.target.DB); err != nil {
		return fmt.Errorf("migrate PostgreSQL target schema %q: %w", opts.targetSchema, err)
	}
	report.Phase = dbmigration.MigrationPhaseCopyRelationalData
	if _, err := a.copyData(ctx, connections.source.DB, connections.target.DB, dbmigration.CopyOptions{BatchSize: opts.batchSize}); err != nil {
		return err
	}
	report.CopyCommitted = true
	committedAt := a.now().UTC()
	report.CopyCommittedAt = &committedAt

	report.Phase = dbmigration.MigrationPhaseReleaseLock
	if err := releaseLock(ctx); err != nil {
		lockHeld = false
		return fmt.Errorf("release migration advisory lock: %w", err)
	}
	lockHeld = false

	// Close all first-generation pools before the independent audit. This proves
	// persisted state through new sessions rather than reusing transaction-local
	// or connection-local state from Copy.
	report.Phase = dbmigration.MigrationPhaseCloseBeforeAudit
	if err := connections.close(); err != nil {
		return fmt.Errorf("close migration connections before independent audit: %w", err)
	}
	report.Phase = dbmigration.MigrationPhaseReopenConnections
	connections, err = a.openConnections(ctx, cfg, opts.targetSchema)
	if err != nil {
		return fmt.Errorf("reopen migration connections for independent audit: %w", err)
	}

	report.Phase = dbmigration.MigrationPhaseIndependentAudit
	audit, err := a.auditExisting(ctx, connections.source.DB, connections.target.DB)
	applyExistingAudit(report, audit)
	return err
}

func (a migrationApplication) runAudit(ctx context.Context, cfg *config.Config, opts options, report *dbmigration.MigrationReport) (err error) {
	report.Phase = dbmigration.MigrationPhaseOpenConnections
	connections, err := a.openConnections(ctx, cfg, opts.targetSchema)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, connections.close()) }()

	report.Phase = dbmigration.MigrationPhaseIndependentAudit
	audit, err := a.auditExisting(ctx, connections.source.DB, connections.target.DB)
	applyExistingAudit(report, audit)
	return err
}

type migrationConnections struct {
	source *databaseConnection
	target *databaseConnection
}

func (a migrationApplication) openConnections(ctx context.Context, cfg *config.Config, schema string) (*migrationConnections, error) {
	connections := &migrationConnections{}
	var err error
	connections.source, err = a.openSource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open MySQL migration source: %w", err)
	}
	connections.target, err = a.openTarget(ctx, cfg, schema)
	if err != nil {
		_ = connections.close()
		return nil, fmt.Errorf("open PostgreSQL business target schema %q: %w", schema, err)
	}
	return connections, nil
}

func (c *migrationConnections) close() error {
	if c == nil {
		return nil
	}
	return errors.Join(
		closeNamedConnection("target", c.target),
		closeNamedConnection("source", c.source),
	)
}

func closeNamedConnection(name string, connection *databaseConnection) error {
	if err := connection.close(); err != nil {
		return fmt.Errorf("close %s database: %w", name, err)
	}
	return nil
}

func applyDryRunResult(report *dbmigration.MigrationReport, result *dbmigration.DryRunResult) {
	if report == nil || result == nil {
		return
	}
	report.SourceAudit = result.Source
	report.TargetReadiness = result.Target
}

func applyExistingAudit(report *dbmigration.MigrationReport, result *dbmigration.ExistingMigrationAudit) {
	if report == nil || result == nil {
		return
	}
	report.DataAudit = result.Data
	report.Sequences = result.Sequences
}

func ensureTargetSchema(ctx context.Context, db *gorm.DB, schema string) error {
	if schema == "public" {
		return nil
	}
	var quoted strings.Builder
	db.Dialector.QuoteTo(&quoted, schema)
	if err := db.WithContext(ctx).Exec("CREATE SCHEMA IF NOT EXISTS " + quoted.String()).Error; err != nil {
		return fmt.Errorf("create PostgreSQL target schema %q: %w", schema, err)
	}
	return nil
}

func openMySQLConnection(ctx context.Context, cfg *config.Config) (*databaseConnection, error) {
	db, err := gorm.Open(mysql.Open(cfg.LegacyMySQL.DSN()), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return checkedConnection(ctx, db)
}

func openPostgresSchemaConnection(ctx context.Context, cfg *config.Config, schema string) (*databaseConnection, error) {
	return openPostgresConnection(ctx, cfg, schema)
}

func openPostgresConnection(ctx context.Context, cfg *config.Config, schema string) (*databaseConnection, error) {
	dsn, err := appdb.PostgresDSN(cfg.Database)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL connection settings: %w", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return checkedConnection(ctx, db)
}

func checkedConnection(ctx context.Context, db *gorm.DB) (*databaseConnection, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return &databaseConnection{DB: db, Close: sqlDB.Close}, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := runCommand(ctx, os.Args[1:], newMigrationApplication(os.Stdout).run)
	if errors.Is(err, flag.ErrHelp) {
		printMigrationUsage(os.Stdout)
		return
	}
	if err != nil {
		log.Fatalf("MySQL to PostgreSQL migration failed: %v", err)
	}
}

func printMigrationUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage: mysql-to-postgres [options]

Default mode is read-only dry-run. Writing modes require an explicit flag:
  --execute                 copy all catalog tables after preflight
  --audit                   independently audit an existing target
  --upgrade-source-schema   AutoMigrate the MySQL source only

Safety options:
  --target-schema NAME      PostgreSQL business schema (default public)
  --confirm-target-schema N exact confirmation required to execute against public
  --batch-size N            rows per insert, 1-1000 (default 100)
  --report PATH             credential-free JSON under .logs/
  --timeout DURATION        optional overall deadline`)
}
