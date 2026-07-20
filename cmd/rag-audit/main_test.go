package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"vid-lens/internal/ragtool"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestParseFlagsRequiresExplicitScope(t *testing.T) {
	if _, err := parseFlags([]string{}); err == nil || !strings.Contains(err.Error(), "user-id") {
		t.Fatalf("parseFlags() error = %v, want user scope validation", err)
	}
	opts, err := parseFlags([]string{"--user-id", "5", "--task-id", "14", "--model", "embed-v1"})
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}
	if opts.userID != 5 || opts.taskID != 14 || opts.embeddingModel != "embed-v1" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestParseFlagsRejectsNegativeScopeAndTimeout(t *testing.T) {
	for _, args := range [][]string{
		{"--user-id", "-1", "--task-id", "14", "--model", "embed-v1"},
		{"--user-id", "5", "--task-id", "0", "--model", "embed-v1"},
		{"--user-id", "5", "--task-id", "14", "--model", "embed-v1", "--timeout", "-1s"},
	} {
		if _, err := parseFlags(args); err == nil {
			t.Fatalf("parseFlags(%v) succeeded, want validation error", args)
		}
	}
}

func TestParseFlagsAcceptsAllModeAndRejectsMixedScope(t *testing.T) {
	opts, err := parseFlags([]string{"--all"})
	if err != nil {
		t.Fatalf("parseFlags(--all) error = %v", err)
	}
	if !opts.all {
		t.Fatalf("opts = %+v, want all mode", opts)
	}
	for _, args := range [][]string{
		{"--all", "--user-id", "5"},
		{"--all", "--task-id", "14"},
		{"--all", "--model", "embed-v1"},
	} {
		if _, err := parseFlags(args); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("parseFlags(%v) error = %v, want mixed-scope rejection", args, err)
		}
	}
}

type fakeRAGAuditSource struct {
	all []repository.ChunkEvidenceManifestEntry
}

func (f fakeRAGAuditSource) ListEvidenceManifest(int64, int64, string) ([]repository.ChunkEvidenceManifestEntry, error) {
	return f.all, nil
}

func (f fakeRAGAuditSource) ListAllEvidenceManifest(context.Context) ([]repository.ChunkEvidenceManifestEntry, error) {
	return f.all, nil
}

type fakeRAGAuditTarget struct {
	all []service.RAGVectorManifestEntry
}

func (f fakeRAGAuditTarget) ListTaskVectorManifest(context.Context, int64, int64, string) ([]service.RAGVectorManifestEntry, error) {
	return f.all, nil
}

func (f fakeRAGAuditTarget) ListAllVectorManifest(context.Context) ([]service.RAGVectorManifestEntry, error) {
	return f.all, nil
}

type scopedOnlyRAGAuditSource struct{}

func (scopedOnlyRAGAuditSource) ListEvidenceManifest(int64, int64, string) ([]repository.ChunkEvidenceManifestEntry, error) {
	return nil, nil
}

type scopedOnlyRAGAuditTarget struct{}

func (scopedOnlyRAGAuditTarget) ListTaskVectorManifest(context.Context, int64, int64, string) ([]service.RAGVectorManifestEntry, error) {
	return nil, nil
}

func TestAuditConfiguredProjectionAllRequiresFullManifestCapabilities(t *testing.T) {
	tests := []struct {
		name   string
		source ragAuditSource
		target ragAuditTarget
		want   string
	}{
		{
			name:   "source",
			source: scopedOnlyRAGAuditSource{},
			target: fakeRAGAuditTarget{},
			want:   "PostgreSQL source does not support an all-scope chunk manifest",
		},
		{
			name:   "target",
			source: fakeRAGAuditSource{},
			target: scopedOnlyRAGAuditTarget{},
			want:   "pgvector target does not support an all-scope vector manifest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auditConfiguredProjection(context.Background(), options{all: true}, "pgvector", tt.source, tt.target)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("auditConfiguredProjection() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAuditConfiguredProjectionAllUsesUnionOfSourceAndTargetScopes(t *testing.T) {
	source := fakeRAGAuditSource{all: []repository.ChunkEvidenceManifestEntry{
		{EvidenceID: "source-only", UserID: 1, TaskID: 10, ChunkID: 100, ChunkIndex: 0, ContentHash: "h1", EmbeddingModel: "m1"},
	}}
	target := fakeRAGAuditTarget{all: []service.RAGVectorManifestEntry{
		{EvidenceID: "target-only", UserID: 2, TaskID: 20, ChunkID: 200, ChunkIndex: 0, ContentHash: "h2", EmbeddingModel: "m2"},
	}}

	summary, err := auditConfiguredProjection(context.Background(), options{all: true}, "pgvector", source, target)
	if err != nil {
		t.Fatalf("auditConfiguredProjection() error = %v", err)
	}
	if summary.Consistent() || len(summary.Scopes) != 2 || summary.SourceCount != 1 || summary.TargetCount != 1 {
		t.Fatalf("summary = %+v, want union-of-scopes drift", summary)
	}
}

func TestAuditConfiguredProjectionAllRequiresPGVector(t *testing.T) {
	_, err := auditConfiguredProjection(context.Background(), options{all: true}, "milvus", fakeRAGAuditSource{}, fakeRAGAuditTarget{})
	if err == nil || !strings.Contains(err.Error(), "pgvector") {
		t.Fatalf("auditConfiguredProjection() error = %v, want pgvector-only all-mode gate", err)
	}
}

func TestWriteAuditSummaryIncludesAggregateAndScopeDetails(t *testing.T) {
	summary := ragtool.RAGProjectionAuditSummary{
		Backend: "pgvector", SourceCount: 2, TargetCount: 1,
		Scopes: []ragtool.RAGProjectionAuditReport{{
			Scope:       ragtool.RAGProjectionScope{UserID: 7, TaskID: 42, EmbeddingModel: "embed-v1", Backend: "pgvector"},
			SourceCount: 2, TargetCount: 1,
			Issues: []ragtool.RAGProjectionIssue{{Message: "projection row is missing"}},
		}},
	}
	var out bytes.Buffer
	writeAuditSummary(&out, summary)
	text := out.String()
	for _, want := range []string{
		"backend=pgvector scopes=1 source=2 target=1 issues=1",
		`scope: user=7 task=42 model="embed-v1" source=2 target=1 issues=1`,
		"- projection row is missing",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("writeAuditSummary() = %q, want %q", text, want)
		}
	}
}
