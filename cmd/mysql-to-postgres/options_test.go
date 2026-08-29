package main

import (
	"context"
	"errors"
	"flag"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOptionsDefaultsToDryRun(t *testing.T) {
	opts, err := parseOptions(nil)
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.mode() != modeDryRun {
		t.Fatalf("mode = %q, want %q", opts.mode(), modeDryRun)
	}
	if opts.configPath != "config.yaml" {
		t.Fatalf("config path = %q, want config.yaml", opts.configPath)
	}
	if opts.batchSize != 100 {
		t.Fatalf("batch size = %d, want 100", opts.batchSize)
	}
	if opts.targetSchema != "public" {
		t.Fatalf("target schema = %q, want public", opts.targetSchema)
	}
	if !pathIsInsideLogsForTest(t, opts.reportPath) {
		t.Fatalf("report path = %q, want path inside .logs", opts.reportPath)
	}
}

func TestParseOptionsRequiresExactPublicSchemaConfirmationForExecute(t *testing.T) {
	if _, err := parseOptions([]string{"--execute"}); err == nil || !strings.Contains(err.Error(), "confirm-target-schema") {
		t.Fatalf("parseOptions(--execute) error = %v, want public schema confirmation", err)
	}
	if _, err := parseOptions([]string{"--execute", "--confirm-target-schema", "rehearsal"}); err == nil || !strings.Contains(err.Error(), "match") {
		t.Fatalf("parseOptions(mismatched confirmation) error = %v, want exact match", err)
	}
	opts, err := parseOptions([]string{"--execute", "--confirm-target-schema", "public"})
	if err != nil {
		t.Fatalf("parseOptions(confirmed public execute) error = %v", err)
	}
	if opts.confirmTargetSchema != "public" {
		t.Fatalf("confirm target schema = %q, want public", opts.confirmTargetSchema)
	}
	if _, err := parseOptions([]string{"--execute", "--target-schema", "rehearsal"}); err != nil {
		t.Fatalf("parseOptions(rehearsal execute) error = %v, confirmation should only gate public", err)
	}
}

func TestParseOptionsRejectsConflictingModes(t *testing.T) {
	cases := [][]string{
		{"--execute", "--upgrade-source-schema"},
		{"--execute", "--audit"},
		{"--audit", "--upgrade-source-schema"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if _, err := parseOptions(args); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("parseOptions(%v) error = %v, want mutually exclusive", args, err)
			}
		})
	}
}

func TestParseOptionsRejectsInvalidBatchSchemaAndReportPath(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "zero batch", args: []string{"--batch-size", "0"}, want: "batch-size"},
		{name: "large batch", args: []string{"--batch-size", "1001"}, want: "batch-size"},
		{name: "empty schema", args: []string{"--target-schema", " "}, want: "target-schema"},
		{name: "quoted schema", args: []string{"--target-schema", `public";DROP SCHEMA public`}, want: "target-schema"},
		{name: "system schema", args: []string{"--target-schema", "pg_catalog"}, want: "target-schema"},
		{name: "outside report", args: []string{"--report", filepath.Join("..", "migration.json")}, want: ".logs"},
		{name: "non-json report", args: []string{"--report", filepath.Join(".logs", "migration.txt")}, want: ".json"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseOptions(test.args); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseOptions(%v) error = %v, want %q", test.args, err, test.want)
			}
		})
	}
}

func TestParseOptionsRejectsUnknownFlagsAndPositionalArguments(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"unexpected"}} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("parseOptions(%v) error = nil, want rejection", args)
		}
	}
}

func TestRunCommandHelpDoesNotInvokeMigration(t *testing.T) {
	invoked := false
	err := runCommand(context.Background(), []string{"--help"}, func(context.Context, options) error {
		invoked = true
		return errors.New("must not run")
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runCommand(--help) error = %v, want flag.ErrHelp", err)
	}
	if invoked {
		t.Fatal("--help invoked migration runtime")
	}
}

func pathIsInsideLogsForTest(t *testing.T, path string) bool {
	t.Helper()
	logs, err := filepath.Abs(".logs")
	if err != nil {
		t.Fatalf("resolve .logs: %v", err)
	}
	candidate, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve report path: %v", err)
	}
	relative, err := filepath.Rel(logs, candidate)
	if err != nil {
		t.Fatalf("relativize report path: %v", err)
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
