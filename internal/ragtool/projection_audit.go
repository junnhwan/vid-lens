package ragtool

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"vid-lens/internal/service"
)

// RAGProjectionScope identifies one source-of-truth chunk set and its vector
// projection. A vector row outside this scope must not be silently treated as
// part of the projection.
type RAGProjectionScope struct {
	UserID         int64
	TaskID         int64
	EmbeddingModel string
	Backend        string
}

// RAGSourceManifestEntry is the backend-neutral view of a relational video_chunks
// row used when auditing a vector projection. The database ID is included on
// purpose: vector rows must point back to a real positive chunk ID.
type RAGSourceManifestEntry struct {
	EvidenceID     string
	UserID         int64
	TaskID         int64
	ChunkID        int64
	ChunkIndex     int
	ContentHash    string
	EmbeddingModel string
}

type RAGProjectionIssueKind string

const (
	RAGProjectionEmptySource      RAGProjectionIssueKind = "empty_source"
	RAGProjectionInvalidSource    RAGProjectionIssueKind = "invalid_source"
	RAGProjectionInvalidTarget    RAGProjectionIssueKind = "invalid_target"
	RAGProjectionDuplicateSource  RAGProjectionIssueKind = "duplicate_source"
	RAGProjectionDuplicateTarget  RAGProjectionIssueKind = "duplicate_target"
	RAGProjectionSourceOnly       RAGProjectionIssueKind = "source_only"
	RAGProjectionTargetOnly       RAGProjectionIssueKind = "target_only"
	RAGProjectionMetadataMismatch RAGProjectionIssueKind = "metadata_mismatch"
)

type RAGProjectionIssue struct {
	Kind       RAGProjectionIssueKind
	EvidenceID string
	ChunkID    int64
	Message    string
}

type RAGProjectionAuditReport struct {
	Scope       RAGProjectionScope
	SourceCount int
	TargetCount int
	Issues      []RAGProjectionIssue
}

func (r RAGProjectionAuditReport) Consistent() bool {
	return len(r.Issues) == 0
}

func (r RAGProjectionAuditReport) Messages() []string {
	messages := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		messages = append(messages, issue.Message)
	}
	return messages
}

// RAGProjectionAuditSummary is the deterministic result of auditing every
// task/model projection visible in either PostgreSQL source chunks or the
// vector target. Including target-only scopes prevents orphan projections from
// escaping a migration gate.
type RAGProjectionAuditSummary struct {
	Backend     string
	SourceCount int
	TargetCount int
	Scopes      []RAGProjectionAuditReport
}

func (s RAGProjectionAuditSummary) Consistent() bool {
	for _, report := range s.Scopes {
		if !report.Consistent() {
			return false
		}
	}
	return true
}

func (s RAGProjectionAuditSummary) IssueCount() int {
	count := 0
	for _, report := range s.Scopes {
		count += len(report.Issues)
	}
	return count
}

type ragProjectionScopeKey struct {
	userID         int64
	taskID         int64
	embeddingModel string
}

// AuditAllRAGProjections groups the union of source and target scopes and then
// delegates each comparison to AuditRAGProjection. Empty databases are valid:
// an explicit single-scope audit still reports an empty source, while --all has
// no requested scope to prove when both manifests are empty.
func AuditAllRAGProjections(backend string, source []RAGSourceManifestEntry, target []service.RAGVectorManifestEntry) (RAGProjectionAuditSummary, error) {
	backend = strings.TrimSpace(backend)
	summary := RAGProjectionAuditSummary{
		Backend: backend, SourceCount: len(source), TargetCount: len(target),
	}
	sourceByScope := make(map[ragProjectionScopeKey][]RAGSourceManifestEntry)
	targetByScope := make(map[ragProjectionScopeKey][]service.RAGVectorManifestEntry)
	scopes := make(map[ragProjectionScopeKey]struct{})
	for _, entry := range source {
		key := ragProjectionScopeKey{userID: entry.UserID, taskID: entry.TaskID, embeddingModel: strings.TrimSpace(entry.EmbeddingModel)}
		sourceByScope[key] = append(sourceByScope[key], entry)
		scopes[key] = struct{}{}
	}
	for _, entry := range target {
		key := ragProjectionScopeKey{userID: entry.UserID, taskID: entry.TaskID, embeddingModel: strings.TrimSpace(entry.EmbeddingModel)}
		targetByScope[key] = append(targetByScope[key], entry)
		scopes[key] = struct{}{}
	}

	ordered := make([]ragProjectionScopeKey, 0, len(scopes))
	for key := range scopes {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].userID != ordered[j].userID {
			return ordered[i].userID < ordered[j].userID
		}
		if ordered[i].taskID != ordered[j].taskID {
			return ordered[i].taskID < ordered[j].taskID
		}
		return ordered[i].embeddingModel < ordered[j].embeddingModel
	})

	summary.Scopes = make([]RAGProjectionAuditReport, 0, len(ordered))
	for _, key := range ordered {
		report, err := AuditRAGProjection(RAGProjectionScope{
			UserID: key.userID, TaskID: key.taskID, EmbeddingModel: key.embeddingModel, Backend: backend,
		}, sourceByScope[key], targetByScope[key])
		if err != nil {
			return summary, fmt.Errorf("audit RAG projection scope user=%d task=%d model=%q: %w", key.userID, key.taskID, key.embeddingModel, err)
		}
		summary.Scopes = append(summary.Scopes, report)
	}
	return summary, nil
}

// AuditRAGProjection compares the relational chunk manifest with a vector-store
// manifest without reading embeddings. PostgreSQL relational chunks remain the source of truth; the
// target is treated as a rebuildable projection. The check is deliberately
// read-only and returns classified issues so maintenance commands can report
// drift without deleting or rewriting data automatically.
func AuditRAGProjection(scope RAGProjectionScope, source []RAGSourceManifestEntry, target []service.RAGVectorManifestEntry) (RAGProjectionAuditReport, error) {
	if scope.UserID <= 0 || scope.TaskID <= 0 || strings.TrimSpace(scope.EmbeddingModel) == "" {
		return RAGProjectionAuditReport{}, errors.New("RAG projection scope requires positive user and task IDs plus an embedding model")
	}
	scope.EmbeddingModel = strings.TrimSpace(scope.EmbeddingModel)
	report := RAGProjectionAuditReport{Scope: scope, SourceCount: len(source), TargetCount: len(target)}

	if len(source) == 0 {
		report.Issues = append(report.Issues, RAGProjectionIssue{
			Kind:    RAGProjectionEmptySource,
			Message: "PostgreSQL has no chunks for the selected embedding model",
		})
	}

	sourceByEvidence := make(map[string]RAGSourceManifestEntry, len(source))
	for _, entry := range source {
		if err := validateSourceManifestEntry(scope, entry); err != nil {
			report.Issues = append(report.Issues, RAGProjectionIssue{
				Kind: RAGProjectionInvalidSource, EvidenceID: entry.EvidenceID, ChunkID: entry.ChunkID,
				Message: fmt.Sprintf("invalid relational source evidence %q: %v", entry.EvidenceID, err),
			})
		}
		if strings.TrimSpace(entry.EvidenceID) == "" {
			continue
		}
		if _, exists := sourceByEvidence[entry.EvidenceID]; exists {
			report.Issues = append(report.Issues, RAGProjectionIssue{
				Kind: RAGProjectionDuplicateSource, EvidenceID: entry.EvidenceID, ChunkID: entry.ChunkID,
				Message: fmt.Sprintf("duplicate relational source evidence %q", entry.EvidenceID),
			})
			continue
		}
		sourceByEvidence[entry.EvidenceID] = entry
	}

	targetByEvidence := make(map[string]service.RAGVectorManifestEntry, len(target))
	for _, entry := range target {
		if err := validateTargetManifestEntry(scope, entry); err != nil {
			report.Issues = append(report.Issues, RAGProjectionIssue{
				Kind: RAGProjectionInvalidTarget, EvidenceID: entry.EvidenceID, ChunkID: entry.ChunkID,
				Message: fmt.Sprintf("invalid %s target evidence %q: %v", displayBackend(scope.Backend), entry.EvidenceID, err),
			})
		}
		if strings.TrimSpace(entry.EvidenceID) == "" {
			continue
		}
		if _, exists := targetByEvidence[entry.EvidenceID]; exists {
			report.Issues = append(report.Issues, RAGProjectionIssue{
				Kind: RAGProjectionDuplicateTarget, EvidenceID: entry.EvidenceID, ChunkID: entry.ChunkID,
				Message: fmt.Sprintf("duplicate %s target evidence %q", displayBackend(scope.Backend), entry.EvidenceID),
			})
			continue
		}
		targetByEvidence[entry.EvidenceID] = entry
	}

	for evidenceID, sourceEntry := range sourceByEvidence {
		targetEntry, ok := targetByEvidence[evidenceID]
		if !ok {
			report.Issues = append(report.Issues, RAGProjectionIssue{
				Kind: RAGProjectionSourceOnly, EvidenceID: evidenceID, ChunkID: sourceEntry.ChunkID,
				Message: fmt.Sprintf("relational source evidence %q has no %s projection", evidenceID, displayBackend(scope.Backend)),
			})
			continue
		}
		for _, mismatch := range manifestMismatches(sourceEntry, targetEntry) {
			report.Issues = append(report.Issues, RAGProjectionIssue{
				Kind: RAGProjectionMetadataMismatch, EvidenceID: evidenceID, ChunkID: sourceEntry.ChunkID,
				Message: fmt.Sprintf("metadata mismatch for evidence %q: %s", evidenceID, mismatch),
			})
		}
	}
	for evidenceID, targetEntry := range targetByEvidence {
		if _, ok := sourceByEvidence[evidenceID]; ok {
			continue
		}
		report.Issues = append(report.Issues, RAGProjectionIssue{
			Kind: RAGProjectionTargetOnly, EvidenceID: evidenceID, ChunkID: targetEntry.ChunkID,
			Message: fmt.Sprintf("%s projection evidence %q has no relational source", displayBackend(scope.Backend), evidenceID),
		})
	}

	sort.Slice(report.Issues, func(i, j int) bool {
		if report.Issues[i].Kind != report.Issues[j].Kind {
			return report.Issues[i].Kind < report.Issues[j].Kind
		}
		if report.Issues[i].EvidenceID != report.Issues[j].EvidenceID {
			return report.Issues[i].EvidenceID < report.Issues[j].EvidenceID
		}
		return report.Issues[i].Message < report.Issues[j].Message
	})
	return report, nil
}

func validateSourceManifestEntry(scope RAGProjectionScope, entry RAGSourceManifestEntry) error {
	if strings.TrimSpace(entry.EvidenceID) == "" {
		return errors.New("vector/evidence ID is required")
	}
	var scopeIssues []string
	if entry.UserID != scope.UserID {
		scopeIssues = append(scopeIssues, fmt.Sprintf("unexpected user_id %d (want %d)", entry.UserID, scope.UserID))
	}
	if entry.TaskID != scope.TaskID {
		scopeIssues = append(scopeIssues, fmt.Sprintf("unexpected task_id %d (want %d)", entry.TaskID, scope.TaskID))
	}
	if strings.TrimSpace(entry.EmbeddingModel) != scope.EmbeddingModel {
		scopeIssues = append(scopeIssues, fmt.Sprintf("unexpected embedding model %q (want %q)", entry.EmbeddingModel, scope.EmbeddingModel))
	}
	if len(scopeIssues) > 0 {
		return errors.New(strings.Join(scopeIssues, "; "))
	}
	if entry.ChunkID <= 0 {
		return errors.New("chunk_id must be positive")
	}
	if entry.ChunkIndex < 0 {
		return errors.New("chunk_index cannot be negative")
	}
	if strings.TrimSpace(entry.ContentHash) == "" {
		return errors.New("content_hash is required")
	}
	return nil
}

func validateTargetManifestEntry(scope RAGProjectionScope, entry service.RAGVectorManifestEntry) error {
	return validateSourceManifestEntry(scope, RAGSourceManifestEntry(entry))
}

func manifestMismatches(source RAGSourceManifestEntry, target service.RAGVectorManifestEntry) []string {
	mismatches := make([]string, 0, 6)
	if source.UserID != target.UserID {
		mismatches = append(mismatches, fmt.Sprintf("user_id source=%d vector=%d", source.UserID, target.UserID))
	}
	if source.TaskID != target.TaskID {
		mismatches = append(mismatches, fmt.Sprintf("task_id source=%d vector=%d", source.TaskID, target.TaskID))
	}
	if source.ChunkID != target.ChunkID {
		mismatches = append(mismatches, fmt.Sprintf("chunk_id source=%d vector=%d", source.ChunkID, target.ChunkID))
	}
	if source.ChunkIndex != target.ChunkIndex {
		mismatches = append(mismatches, fmt.Sprintf("chunk_index source=%d vector=%d", source.ChunkIndex, target.ChunkIndex))
	}
	if source.ContentHash != target.ContentHash {
		mismatches = append(mismatches, fmt.Sprintf("content_hash source=%s vector=%s", source.ContentHash, target.ContentHash))
	}
	if strings.TrimSpace(source.EmbeddingModel) != strings.TrimSpace(target.EmbeddingModel) {
		mismatches = append(mismatches, fmt.Sprintf("embedding_model source=%q vector=%q", source.EmbeddingModel, target.EmbeddingModel))
	}
	return mismatches
}

func displayBackend(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "vector-store"
	}
	return name
}
