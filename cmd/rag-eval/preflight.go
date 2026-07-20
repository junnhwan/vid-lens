package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/ragtool"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/vector"
)

// The preflight interfaces deliberately expose only read operations. This
// keeps the audit independent from GORM and makes it usable before any
// embedding or LLM call is made.
type evalPreflightTaskSource interface {
	FindByID(id int64) (*model.VideoTask, error)
}

type evalPreflightChunkSource interface {
	ListEmbeddingModelsByTask(userID, taskID int64) ([]string, error)
	ListEvidenceManifest(userID, taskID int64, embeddingModel string) ([]repository.ChunkEvidenceManifestEntry, error)
}

type evalPreflightProfileSource interface {
	GetDefaultAIProfile(userID int64) (*ai.Profile, error)
}

type evalPreflightVectorSource interface {
	ListTaskVectorManifest(context.Context, int64, int64, string) ([]service.RAGVectorManifestEntry, error)
}

type evalPreflightSources struct {
	tasks    evalPreflightTaskSource
	chunks   evalPreflightChunkSource
	profiles evalPreflightProfileSource
	vectors  evalPreflightVectorSource
}

type evalPreflightTaskReport struct {
	TaskID         int64
	UserID         int64
	CaseCount      int
	ChunkCount     int
	VectorCount    int
	EmbeddingModel string
	Issues         []string
}

type evalPreflightReport struct {
	Backend      string
	CaseCount    int
	ReadyCases   int
	InvalidCases int
	Tasks        []evalPreflightTaskReport
}

func (r evalPreflightReport) Valid() bool {
	return r.InvalidCases == 0 && len(r.Tasks) > 0
}

func (r evalPreflightReport) Error() string {
	if r.Valid() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "RAG eval preflight failed: backend=%s cases=%d ready=%d invalid=%d\n", r.Backend, r.CaseCount, r.ReadyCases, r.InvalidCases)
	for _, task := range r.Tasks {
		if len(task.Issues) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- task %d cases=%d: %s\n", task.TaskID, task.CaseCount, strings.Join(task.Issues, "; "))
	}
	return strings.TrimRight(b.String(), "\n")
}

func preflightCases(ctx context.Context, cases []evalCase, sources evalPreflightSources, backend string) (evalPreflightReport, error) {
	if sources.tasks == nil || sources.chunks == nil || sources.profiles == nil || sources.vectors == nil {
		return evalPreflightReport{}, errors.New("RAG eval preflight requires task, chunk, profile, and vector sources")
	}
	report := evalPreflightReport{Backend: vector.NormalizeBackendName(backend), CaseCount: len(cases)}
	caseCounts := make(map[int64]int)
	for _, c := range cases {
		caseCounts[c.TaskID]++
	}
	taskIDs := make([]int64, 0, len(caseCounts))
	for taskID := range caseCounts {
		taskIDs = append(taskIDs, taskID)
	}
	sort.Slice(taskIDs, func(i, j int) bool { return taskIDs[i] < taskIDs[j] })

	for _, taskID := range taskIDs {
		taskReport := evalPreflightTaskReport{TaskID: taskID, CaseCount: caseCounts[taskID]}
		task, err := sources.tasks.FindByID(taskID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				taskReport.Issues = append(taskReport.Issues, "task not found or has been soft-deleted")
			} else {
				return report, fmt.Errorf("preflight find task %d: %w", taskID, err)
			}
			report.Tasks = append(report.Tasks, taskReport)
			report.InvalidCases += taskReport.CaseCount
			continue
		}
		taskReport.UserID = task.UserID
		profile, err := sources.profiles.GetDefaultAIProfile(task.UserID)
		if err != nil {
			return report, fmt.Errorf("preflight load profile for task %d user %d: %w", taskID, task.UserID, err)
		}
		if profile == nil {
			return report, fmt.Errorf("preflight load profile for task %d user %d: source returned nil profile", taskID, task.UserID)
		}
		modelName := strings.TrimSpace(profile.EmbeddingModel)
		taskReport.EmbeddingModel = modelName
		models, err := sources.chunks.ListEmbeddingModelsByTask(task.UserID, taskID)
		if err != nil {
			return report, fmt.Errorf("preflight list embedding models for task %d: %w", taskID, err)
		}
		if modelName == "" {
			taskReport.Issues = append(taskReport.Issues, "default AI profile has empty embedding model")
		} else if !containsString(models, modelName) {
			taskReport.Issues = append(taskReport.Issues, fmt.Sprintf("embedding model %q is not present in relational chunks (available: %s)", modelName, strings.Join(models, ", ")))
		}
		if len(taskReport.Issues) == 0 {
			sourceManifest, err := sources.chunks.ListEvidenceManifest(task.UserID, taskID, modelName)
			if err != nil {
				return report, fmt.Errorf("preflight list relational chunks for task %d: %w", taskID, err)
			}
			taskReport.ChunkCount = len(sourceManifest)
			vectorManifest, err := sources.vectors.ListTaskVectorManifest(ctx, task.UserID, taskID, modelName)
			if err != nil {
				return report, fmt.Errorf("preflight list %s vectors for task %d: %w", report.Backend, taskID, err)
			}
			taskReport.VectorCount = len(vectorManifest)
			taskReport.Issues = append(taskReport.Issues, compareEvalManifests(sourceManifest, vectorManifest, report.Backend, task.UserID, taskID, modelName)...)
		}
		report.Tasks = append(report.Tasks, taskReport)
		if len(taskReport.Issues) == 0 {
			report.ReadyCases += taskReport.CaseCount
		} else {
			report.InvalidCases += taskReport.CaseCount
		}
	}
	return report, nil
}

func compareEvalManifests(source []repository.ChunkEvidenceManifestEntry, target []service.RAGVectorManifestEntry, backend string, userID, taskID int64, embeddingModel string) []string {
	sourceEntries := make([]ragtool.RAGSourceManifestEntry, 0, len(source))
	for _, entry := range source {
		sourceEntries = append(sourceEntries, ragtool.RAGSourceManifestEntry{
			EvidenceID: entry.EvidenceID, UserID: entry.UserID, TaskID: entry.TaskID,
			ChunkID: entry.ChunkID, ChunkIndex: entry.ChunkIndex, ContentHash: entry.ContentHash,
			EmbeddingModel: entry.EmbeddingModel,
		})
	}
	report, err := ragtool.AuditRAGProjection(ragtool.RAGProjectionScope{
		UserID: userID, TaskID: taskID, EmbeddingModel: embeddingModel, Backend: backend,
	}, sourceEntries, target)
	if err != nil {
		return []string{err.Error()}
	}
	return report.Messages()
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}
