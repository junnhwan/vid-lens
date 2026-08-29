package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type migrationMode string

const (
	modeDryRun              migrationMode = "dry-run"
	modeExecute             migrationMode = "execute"
	modeAudit               migrationMode = "audit"
	modeUpgradeSourceSchema migrationMode = "upgrade-source-schema"
)

var targetSchemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

type options struct {
	configPath          string
	targetSchema        string
	confirmTargetSchema string
	reportPath          string
	batchSize           int
	timeout             time.Duration
	execute             bool
	audit               bool
	upgradeSourceSchema bool
}

func (o options) mode() migrationMode {
	switch {
	case o.execute:
		return modeExecute
	case o.audit:
		return modeAudit
	case o.upgradeSourceSchema:
		return modeUpgradeSourceSchema
	default:
		return modeDryRun
	}
}

func parseOptions(args []string) (options, error) {
	flags := flag.NewFlagSet("mysql-to-postgres", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var opts options
	flags.StringVar(&opts.configPath, "config", "config.yaml", "application config file")
	flags.StringVar(&opts.targetSchema, "target-schema", "public", "PostgreSQL schema containing migration target tables")
	flags.StringVar(&opts.confirmTargetSchema, "confirm-target-schema", "", "exact target schema confirmation required when executing against public")
	flags.StringVar(&opts.reportPath, "report", filepath.Join(".logs", "mysql-to-postgres-report.json"), "credential-free JSON report path under .logs")
	flags.IntVar(&opts.batchSize, "batch-size", 100, "rows per target insert statement (1-1000)")
	flags.DurationVar(&opts.timeout, "timeout", 0, "optional overall timeout; zero means no deadline")
	flags.BoolVar(&opts.execute, "execute", false, "copy all catalog tables in one target transaction")
	flags.BoolVar(&opts.audit, "audit", false, "independently compare an already migrated target")
	flags.BoolVar(&opts.upgradeSourceSchema, "upgrade-source-schema", false, "run model migration on MySQL only, then exit")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}

	opts.configPath = strings.TrimSpace(opts.configPath)
	opts.targetSchema = strings.TrimSpace(opts.targetSchema)
	opts.confirmTargetSchema = strings.TrimSpace(opts.confirmTargetSchema)
	opts.reportPath = strings.TrimSpace(opts.reportPath)
	if opts.configPath == "" {
		return options{}, errors.New("config path is required")
	}
	if opts.batchSize < 1 || opts.batchSize > 1000 {
		return options{}, errors.New("batch-size must be between 1 and 1000")
	}
	if opts.timeout < 0 {
		return options{}, errors.New("timeout cannot be negative")
	}
	if !targetSchemaPattern.MatchString(opts.targetSchema) || strings.HasPrefix(opts.targetSchema, "pg_") || opts.targetSchema == "information_schema" {
		return options{}, errors.New("target-schema must be a safe lowercase PostgreSQL identifier outside system schemas")
	}
	if err := validateReportPath(opts.reportPath); err != nil {
		return options{}, err
	}
	opts.reportPath = filepath.Clean(opts.reportPath)

	selectedModes := 0
	for _, selected := range []bool{opts.execute, opts.audit, opts.upgradeSourceSchema} {
		if selected {
			selectedModes++
		}
	}
	if selectedModes > 1 {
		return options{}, errors.New("--execute, --audit, and --upgrade-source-schema are mutually exclusive")
	}
	if opts.confirmTargetSchema != "" && !opts.execute {
		return options{}, errors.New("--confirm-target-schema is only valid with --execute")
	}
	if opts.execute && opts.confirmTargetSchema != "" && opts.confirmTargetSchema != opts.targetSchema {
		return options{}, errors.New("--confirm-target-schema must exactly match --target-schema")
	}
	if opts.execute && opts.targetSchema == "public" && opts.confirmTargetSchema != "public" {
		return options{}, errors.New("executing against public requires --confirm-target-schema=public")
	}
	return opts, nil
}

func validateReportPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("report path is required")
	}
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return errors.New("report path must use a .json extension")
	}
	logsDirectory, err := filepath.Abs(".logs")
	if err != nil {
		return fmt.Errorf("resolve .logs directory: %w", err)
	}
	candidate, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve report path: %w", err)
	}
	relative, err := filepath.Rel(logsDirectory, candidate)
	if err != nil {
		return fmt.Errorf("validate report path under .logs: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("report path must stay inside .logs")
	}
	return nil
}

type migrationRunner func(context.Context, options) error

// runCommand parses flags before invoking any runtime dependency. This keeps
// --help and invalid arguments side-effect free and directly testable.
func runCommand(ctx context.Context, args []string, run migrationRunner) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if run == nil {
		return errors.New("migration runner is nil")
	}
	return run(ctx, opts)
}
