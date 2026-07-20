package ragtool

import (
	"strings"
	"testing"

	"vid-lens/internal/service"
)

func TestAuditRAGProjectionAcceptsMatchingManifests(t *testing.T) {
	scope := RAGProjectionScope{UserID: 5, TaskID: 14, EmbeddingModel: "embed-v1", Backend: "pgvector"}
	source := []RAGSourceManifestEntry{{
		EvidenceID: "e1", UserID: 5, TaskID: 14, ChunkID: 21, ChunkIndex: 0,
		ContentHash: "h1", EmbeddingModel: "embed-v1",
	}}
	target := []service.RAGVectorManifestEntry{{
		EvidenceID: "e1", UserID: 5, TaskID: 14, ChunkID: 21, ChunkIndex: 0,
		ContentHash: "h1", EmbeddingModel: "embed-v1",
	}}

	report, err := AuditRAGProjection(scope, source, target)
	if err != nil {
		t.Fatalf("AuditRAGProjection() error = %v", err)
	}
	if !report.Consistent() || len(report.Issues) != 0 {
		t.Fatalf("report = %+v, want consistent report", report)
	}
	if report.SourceCount != 1 || report.TargetCount != 1 {
		t.Fatalf("counts = %d/%d, want 1/1", report.SourceCount, report.TargetCount)
	}
}

func TestAuditRAGProjectionClassifiesDrift(t *testing.T) {
	scope := RAGProjectionScope{UserID: 5, TaskID: 14, EmbeddingModel: "embed-v1", Backend: "pgvector"}
	source := []RAGSourceManifestEntry{
		{EvidenceID: "source-only", UserID: 5, TaskID: 14, ChunkID: 21, ChunkIndex: 0, ContentHash: "h1", EmbeddingModel: "embed-v1"},
		{EvidenceID: "changed", UserID: 5, TaskID: 14, ChunkID: 22, ChunkIndex: 1, ContentHash: "source-hash", EmbeddingModel: "embed-v1"},
	}
	target := []service.RAGVectorManifestEntry{
		{EvidenceID: "changed", UserID: 5, TaskID: 14, ChunkID: 999, ChunkIndex: 1, ContentHash: "vector-hash", EmbeddingModel: "embed-v1"},
		{EvidenceID: "target-only", UserID: 5, TaskID: 14, ChunkID: 23, ChunkIndex: 2, ContentHash: "h3", EmbeddingModel: "embed-v1"},
	}

	report, err := AuditRAGProjection(scope, source, target)
	if err != nil {
		t.Fatalf("AuditRAGProjection() error = %v", err)
	}
	if report.Consistent() {
		t.Fatalf("report = %+v, want drift", report)
	}
	kinds := make(map[RAGProjectionIssueKind]int)
	for _, issue := range report.Issues {
		kinds[issue.Kind]++
	}
	for _, kind := range []RAGProjectionIssueKind{RAGProjectionSourceOnly, RAGProjectionTargetOnly, RAGProjectionMetadataMismatch} {
		if kinds[kind] == 0 {
			t.Fatalf("issues = %+v, missing kind %q", report.Issues, kind)
		}
	}
	joined := strings.Join(report.Messages(), "; ")
	for _, want := range []string{"source-only", "target-only", "chunk_id", "content_hash"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("messages = %q, missing %q", joined, want)
		}
	}
}

func TestAuditRAGProjectionRejectsInvalidAndDuplicateEntries(t *testing.T) {
	scope := RAGProjectionScope{UserID: 5, TaskID: 14, EmbeddingModel: "embed-v1", Backend: "pgvector"}
	source := []RAGSourceManifestEntry{
		{EvidenceID: "dup", UserID: 5, TaskID: 14, ChunkID: 0, ChunkIndex: 0, ContentHash: "h1", EmbeddingModel: "embed-v1"},
		{EvidenceID: "dup", UserID: 5, TaskID: 14, ChunkID: 22, ChunkIndex: 1, ContentHash: "h2", EmbeddingModel: "embed-v1"},
	}
	target := []service.RAGVectorManifestEntry{
		{EvidenceID: "dup", UserID: 99, TaskID: 14, ChunkID: 0, ChunkIndex: 0, ContentHash: "h1", EmbeddingModel: "embed-v1"},
		{EvidenceID: "dup", UserID: 5, TaskID: 14, ChunkID: 22, ChunkIndex: 1, ContentHash: "h2", EmbeddingModel: "embed-v1"},
	}

	report, err := AuditRAGProjection(scope, source, target)
	if err != nil {
		t.Fatalf("AuditRAGProjection() error = %v", err)
	}
	kinds := make(map[RAGProjectionIssueKind]int)
	for _, issue := range report.Issues {
		kinds[issue.Kind]++
	}
	for _, kind := range []RAGProjectionIssueKind{RAGProjectionInvalidSource, RAGProjectionInvalidTarget, RAGProjectionDuplicateSource, RAGProjectionDuplicateTarget} {
		if kinds[kind] == 0 {
			t.Fatalf("issues = %+v, missing kind %q", report.Issues, kind)
		}
	}
}

func TestAuditRAGProjectionValidatesScope(t *testing.T) {
	_, err := AuditRAGProjection(RAGProjectionScope{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("AuditRAGProjection() error = %v, want scope validation", err)
	}
}

func TestAuditRAGProjectionReportsMissingSource(t *testing.T) {
	report, err := AuditRAGProjection(RAGProjectionScope{
		UserID: 5, TaskID: 14, EmbeddingModel: "embed-v1", Backend: "pgvector",
	}, nil, nil)
	if err != nil {
		t.Fatalf("AuditRAGProjection() error = %v", err)
	}
	if report.Consistent() {
		t.Fatalf("report = %+v, want empty source issue", report)
	}
	if len(report.Issues) != 1 || report.Issues[0].Kind != RAGProjectionEmptySource {
		t.Fatalf("issues = %+v, want empty source classification", report.Issues)
	}
}

func TestAuditAllRAGProjectionsCoversSourceAndTargetOnlyScopes(t *testing.T) {
	source := []RAGSourceManifestEntry{
		{EvidenceID: "shared", UserID: 1, TaskID: 10, ChunkID: 100, ChunkIndex: 0, ContentHash: "h1", EmbeddingModel: "m1"},
		{EvidenceID: "source-only", UserID: 2, TaskID: 20, ChunkID: 200, ChunkIndex: 0, ContentHash: "h2", EmbeddingModel: "m2"},
	}
	target := []service.RAGVectorManifestEntry{
		{EvidenceID: "shared", UserID: 1, TaskID: 10, ChunkID: 100, ChunkIndex: 0, ContentHash: "h1", EmbeddingModel: "m1"},
		{EvidenceID: "target-only", UserID: 3, TaskID: 30, ChunkID: 300, ChunkIndex: 0, ContentHash: "h3", EmbeddingModel: "m3"},
	}

	summary, err := AuditAllRAGProjections("pgvector", source, target)
	if err != nil {
		t.Fatalf("AuditAllRAGProjections() error = %v", err)
	}
	if summary.Consistent() || summary.SourceCount != 2 || summary.TargetCount != 2 || len(summary.Scopes) != 3 {
		t.Fatalf("summary = %+v, want three-scope drift", summary)
	}
	if summary.Scopes[0].Scope.UserID != 1 || summary.Scopes[1].Scope.UserID != 2 || summary.Scopes[2].Scope.UserID != 3 {
		t.Fatalf("scope order = %+v, want deterministic user/task/model order", summary.Scopes)
	}
	kinds := map[RAGProjectionIssueKind]int{}
	for _, report := range summary.Scopes {
		for _, issue := range report.Issues {
			kinds[issue.Kind]++
		}
	}
	if kinds[RAGProjectionSourceOnly] == 0 || kinds[RAGProjectionTargetOnly] == 0 || kinds[RAGProjectionEmptySource] == 0 {
		t.Fatalf("issue kinds = %+v, want source-only and target-only scope evidence", kinds)
	}
}

func TestAuditAllRAGProjectionsAcceptsEmptyDatabase(t *testing.T) {
	summary, err := AuditAllRAGProjections("pgvector", nil, nil)
	if err != nil {
		t.Fatalf("AuditAllRAGProjections() error = %v", err)
	}
	if !summary.Consistent() || len(summary.Scopes) != 0 {
		t.Fatalf("summary = %+v, empty database should be consistent", summary)
	}
}
