package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"sort"
	"strconv"
	"strings"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type ArtifactService struct {
	repos    *repository.Repositories
	profiles *AIProfileService
	factory  *ai.Factory
}

func NewArtifactService(repos *repository.Repositories, profiles *AIProfileService, factory *ai.Factory) *ArtifactService {
	return &ArtifactService{repos, profiles, factory}
}
func artifactSource(ctx context.Context, repos *repository.Repositories, owner, id int64) (string, []model.SourceSnapshotItem, error) {
	task, err := repos.Task.FindByID(id)
	if err != nil {
		return "", nil, err
	}
	if task.UserID != owner {
		return "", nil, artifact.Err("not_found", 404)
	}
	if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
		return "", nil, artifact.Err("source_not_ready", 422)
	}
	rows, err := repos.TranscriptionChunk.ListByTaskID(id)
	if err != nil {
		return "", nil, err
	}
	for _, row := range rows {
		if row.Status != model.TranscriptionChunkStatusCompleted {
			return "", nil, artifact.Err("source_not_ready", 422)
		}
	}
	if len(rows) == 0 {
		t, e := repos.Transcription.FindByTaskID(id)
		if e != nil {
			return "", nil, e
		}
		if t != nil && strings.TrimSpace(t.Content) != "" {
			rows = []model.VideoTranscriptionChunk{{ID: t.ID, TaskID: id, SegmentKey: fmt.Sprintf("transcription:%d", t.ID), Status: model.TranscriptionChunkStatusCompleted, Content: t.Content}}
		}
	}
	frames, err := repos.VisualFrame.ListByTaskID(id)
	if err != nil {
		return "", nil, err
	}
	timeline := BuildVideoTimeline(id, rows, frames)
	if len(timeline.Atoms) == 0 {
		return "", nil, artifact.Err("source_not_ready", 422)
	}
	if len(timeline.Atoms) > 1000 {
		return "", nil, artifact.Err("source_limit_exceeded", 422)
	}
	items := make([]model.SourceSnapshotItem, 0, len(timeline.Atoms))
	size := 0
	for _, a := range timeline.Atoms {
		size += len(a.Content)
		item := model.SourceSnapshotItem{SourceIdentity: a.ID, Modality: a.Modality, Content: a.Content, ContentHash: artifact.Hash(a.Content), TimeRangeStatus: a.TimeRangeStatus}
		if item.TimeRangeStatus == model.ChunkTimeRangeExact {
			item.TimeRangeStatus = "precise"
		}
		if a.TimeRangeStatus != "unknown" {
			start, end := a.StartMS, a.EndMS
			item.StartMS = &start
			item.EndMS = &end
		}
		items = append(items, item)
	}
	if size > 2*1024*1024 {
		return "", nil, artifact.Err("source_limit_exceeded", 422)
	}
	return artifact.Hash(artifact.JSON(items)), items, nil
}

type ArtifactSource struct {
	ManifestID string                     `json:"manifest_id"`
	SourceID   int64                      `json:"source_id"`
	Title      string                     `json:"title"`
	Evidence   []model.SourceSnapshotItem `json:"evidence"`
}

func (s *ArtifactService) Source(ctx context.Context, owner, id int64) (*ArtifactSource, error) {
	m, items, err := s.repos.Artifact.Freeze(ctx, owner, id, artifactSource)
	if err != nil {
		return nil, err
	}
	return &ArtifactSource{m.ID, m.SourceID, m.Title, items}, nil
}
func (s *ArtifactService) Evidence(ctx context.Context, owner int64, m, id string) (*model.SourceSnapshotItem, error) {
	return s.repos.Artifact.Evidence(ctx, owner, m, id)
}

type ArtifactVersionView struct {
	model.ArtifactVersion
	Body         artifact.Body `json:"body"`
	SourceStatus string        `json:"source_status"`
}
type ArtifactDetail struct {
	model.Artifact
	Version   *ArtifactVersionView `json:"version"`
	LatestRun *ArtifactRunView     `json:"latest_run"`
}
type ArtifactListItem struct {
	model.Artifact
	LatestRun *ArtifactRunView `json:"latest_run"`
}

func (s *ArtifactService) latestRuns(ctx context.Context, owner int64, ids []string) (map[string]*ArtifactRunView, error) {
	result := make(map[string]*ArtifactRunView, len(ids))
	runIDs, err := s.repos.Artifact.LatestRunIDs(ctx, owner, ids)
	if err != nil {
		return nil, err
	}
	for artifactID, runID := range runIDs {
		run, err := s.Run(ctx, owner, runID)
		if err != nil {
			return nil, err
		}
		result[artifactID] = run
	}
	return result, nil
}

func (s *ArtifactService) versionView(ctx context.Context, owner int64, v *model.ArtifactVersion) (*ArtifactVersionView, error) {
	if v == nil {
		return nil, nil
	}
	out := &ArtifactVersionView{ArtifactVersion: *v}
	err := json.Unmarshal([]byte(v.BodyJSON), &out.Body)
	if err != nil {
		return nil, err
	}
	m, _, err := s.repos.Artifact.Snapshot(ctx, owner, v.ManifestID)
	if err != nil {
		return nil, err
	}
	hash, _, sourceErr := artifactSource(ctx, s.repos, owner, m.SourceID)
	out.SourceStatus = "current"
	if sourceErr != nil || hash != m.ContentHash {
		out.SourceStatus = "outdated"
	}
	return out, err
}
func (s *ArtifactService) Get(ctx context.Context, owner int64, id string) (*ArtifactDetail, error) {
	a, v, err := s.repos.Artifact.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	runs, err := s.latestRuns(ctx, owner, []string{id})
	if err != nil {
		return nil, err
	}
	view, err := s.versionView(ctx, owner, v)
	return &ArtifactDetail{Artifact: *a, Version: view, LatestRun: runs[id]}, err
}
func (s *ArtifactService) Version(ctx context.Context, owner int64, id, vid string) (*ArtifactVersionView, error) {
	v, err := s.repos.Artifact.Version(ctx, owner, id, vid)
	if err != nil {
		return nil, err
	}
	return s.versionView(ctx, owner, v)
}
func (s *ArtifactService) Versions(ctx context.Context, owner int64, id string) ([]model.ArtifactVersion, error) {
	return s.repos.Artifact.Versions(ctx, owner, id)
}
func (s *ArtifactService) List(ctx context.Context, owner, source int64, page, size int) ([]ArtifactListItem, int64, error) {
	rows, total, err := s.repos.Artifact.List(ctx, owner, source, page, size)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	runs, err := s.latestRuns(ctx, owner, ids)
	if err != nil {
		return nil, 0, err
	}
	items := make([]ArtifactListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ArtifactListItem{Artifact: row, LatestRun: runs[row.ID]})
	}
	return items, total, nil
}
func (s *ArtifactService) Create(ctx context.Context, owner int64, ids []int64, body artifact.Body) (*ArtifactDetail, error) {
	if len(ids) != 1 || ids[0] <= 0 {
		return nil, artifact.Err("invalid_request", 400)
	}
	source, err := s.Source(ctx, owner, ids[0])
	if err != nil {
		return nil, err
	}
	a, err := s.repos.Artifact.Create(ctx, owner, source.ManifestID, body)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, owner, a.ID)
}
func (s *ArtifactService) Save(ctx context.Context, owner int64, id string, expected int64, body *artifact.Body, adopt string) (*ArtifactDetail, error) {
	if expected < 1 {
		return nil, artifact.Err("invalid_request", 400)
	}
	if err := s.repos.Artifact.Save(ctx, owner, id, expected, body, adopt); err != nil {
		return nil, err
	}
	return s.Get(ctx, owner, id)
}
func profileFingerprint(p *ai.Profile) string {
	return artifact.Hash(artifact.JSON([]any{p.ID, p.LLMProvider, p.LLMBaseURL, p.LLMModel, p.LLMContextTokens}))
}
func (s *ArtifactService) Submit(ctx context.Context, owner int64, key string, input artifact.GenerationRequest, parent *string) (*ArtifactRunView, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if err := artifact.ValidateKey(key); err != nil {
		return nil, err
	}
	hash := artifact.Hash(artifact.JSON(struct {
		Input  artifact.GenerationRequest
		Parent *string
	}{input, parent}))
	if parent != nil {
		hash = artifact.Hash("retry:" + *parent)
	}
	prior, err := s.repos.Artifact.ByKey(ctx, owner, key, hash)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return s.Run(ctx, owner, prior.RunID)
	}
	source, err := s.Source(ctx, owner, input.SourceIDs[0])
	if err != nil {
		return nil, err
	}
	resolved, err := s.profiles.GetDefaultConversationProfile(owner)
	if err != nil {
		return nil, artifact.Err("profile_required", 422)
	}
	p := resolved.Profile
	if strings.TrimSpace(p.LLMAPIKey) == "" || strings.TrimSpace(p.LLMModel) == "" {
		return nil, artifact.Err("profile_required", 422)
	}
	budget := resolved.EffectiveAgentBudget.Values
	now := time.Now().UTC()
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		goal = "生成有来源的中文学习笔记"
	}
	run := &model.AgentRun{ID: uuid.NewString(), UserID: owner, SubjectKind: "generation_request", ExecutionKind: "artifact", RecipeVersion: artifact.Recipe, ScopeType: "video", TaskID: input.SourceIDs[0], Goal: goal, Mode: "artifact", Status: "pending", Stage: "queued", ProfileSnapshot: artifact.JSON(map[string]any{"profile_id": p.ID, "model": p.LLMModel, "fingerprint": profileFingerprint(p)}), PolicySnapshot: artifact.JSON(map[string]any{"recipe": artifact.Recipe, "schema_version": 1}), BudgetSnapshot: artifact.JSON(resolved.EffectiveAgentBudget), MaxSteps: budget.MaxToolCalls, MaxLLMCalls: budget.MaxToolCalls, MaxAttemptsPerStep: 2, MaxPromptTokens: int64(budget.MaxInputTokens), MaxCompletionTokens: int64(budget.MaxOutputTokens), MaxDurationMs: int64(budget.MaxDurationSeconds) * 1000, MaxContextChars: int64(p.LLMContextTokens), CreatedAt: now}
	req := &model.GenerationRequest{ID: uuid.NewString(), RunID: run.ID, UserID: owner, IdempotencyKey: key, RequestHash: hash, RequestJSON: artifact.JSON(input), ManifestID: source.ManifestID, BaseVersion: input.BaseVersion, Recipe: artifact.Recipe, ProfileID: p.ID, ProfileFingerprint: profileFingerprint(p), ParentRunID: parent, QueueDeadline: now.Add(24 * time.Hour)}
	if input.ArtifactID != nil {
		req.ArtifactID = *input.ArtifactID
	}
	saved, err := s.repos.Artifact.Submit(ctx, req, run, source.Title)
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, owner, saved.RunID)
}

type ArtifactRunResult struct {
	ArtifactID  string `json:"artifact_id"`
	VersionID   string `json:"version_id"`
	Quality     string `json:"quality"`
	IsCandidate bool   `json:"is_candidate"`
}
type ArtifactRunUsage struct {
	LLMCalls         int    `json:"llm_calls"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TokenSource      string `json:"token_source"`
}
type ArtifactRunView struct {
	ID              string             `json:"id"`
	ArtifactID      string             `json:"artifact_id"`
	SourceTaskID    int64              `json:"source_task_id"`
	ParentRunID     *string            `json:"parent_run_id"`
	Status          string             `json:"status"`
	Stage           string             `json:"stage"`
	CancelRequested bool               `json:"cancel_requested"`
	CanCancel       bool               `json:"can_cancel"`
	CanRetry        bool               `json:"can_retry"`
	CanResume       bool               `json:"can_resume"`
	Result          *ArtifactRunResult `json:"result"`
	ErrorCode       *string            `json:"error_code"`
	CreatedAt       time.Time          `json:"created_at"`
	StartedAt       *time.Time         `json:"started_at"`
	FinishedAt      *time.Time         `json:"finished_at"`
	LastSeq         int64              `json:"last_seq"`
	Usage           ArtifactRunUsage   `json:"usage"`
}

func (s *ArtifactService) Run(ctx context.Context, owner int64, id string) (*ArtifactRunView, error) {
	r, req, err := s.repos.Artifact.Run(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	active := r.Status == "pending" || r.Status == "running"
	v := &ArtifactRunView{ID: r.ID, ArtifactID: req.ArtifactID, SourceTaskID: r.TaskID, ParentRunID: req.ParentRunID, Status: r.Status, Stage: r.Stage, CancelRequested: r.CancelRequestedAt != nil, CanCancel: active && r.CancelRequestedAt == nil, CanRetry: !active, CanResume: active && r.CancelRequestedAt == nil, CreatedAt: r.CreatedAt, StartedAt: r.ExecutionStartedAt, FinishedAt: r.FinishedAt, LastSeq: r.EventSeq, Usage: ArtifactRunUsage{r.LLMCallsUsed, r.PromptTokensUsed, r.CompletionTokensUsed, r.TokenUsageSource}}
	if r.ErrorCode != "" {
		v.ErrorCode = &r.ErrorCode
	}
	if r.ResultVersionID != nil {
		candidate, e := s.repos.Artifact.ResultCandidate(ctx, *r.ResultVersionID)
		if e != nil {
			return nil, e
		}
		v.Result = &ArtifactRunResult{req.ArtifactID, *r.ResultVersionID, "needs_review", candidate}
	}
	return v, nil
}
func (s *ArtifactService) Cancel(ctx context.Context, owner int64, id string) (*ArtifactRunView, error) {
	if err := s.repos.Artifact.Cancel(ctx, owner, id); err != nil {
		return nil, err
	}
	return s.Run(ctx, owner, id)
}
func (s *ArtifactService) Resume(ctx context.Context, owner int64, id string) (*ArtifactRunView, error) {
	if err := s.repos.Artifact.Resume(ctx, owner, id); err != nil {
		return nil, err
	}
	return s.Run(ctx, owner, id)
}
func (s *ArtifactService) Retry(ctx context.Context, owner int64, id, key string) (*ArtifactRunView, error) {
	r, req, err := s.repos.Artifact.Run(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if r.Status == "pending" || r.Status == "running" {
		return nil, artifact.Err("run_not_terminal", 409)
	}
	if err = artifact.ValidateKey(key); err != nil {
		return nil, err
	}
	prior, err := s.repos.Artifact.ByKey(ctx, owner, key, artifact.Hash("retry:"+id))
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return s.Run(ctx, owner, prior.RunID)
	}
	var input artifact.GenerationRequest
	if err = json.Unmarshal([]byte(req.RequestJSON), &input); err != nil {
		return nil, err
	}
	// Freeze the retry base once via its key; a repeated retry must not depend on a later head.
	input.ArtifactID = &req.ArtifactID
	current, _, err := s.repos.Artifact.Get(ctx, owner, req.ArtifactID)
	if err != nil {
		return nil, err
	}
	input.BaseVersion = current.HeadVersion
	return s.Submit(ctx, owner, key, input, &id)
}
func (s *ArtifactService) Events(ctx context.Context, owner int64, id string, after int64) ([]model.RunEvent, error) {
	return s.repos.Artifact.Events(ctx, owner, id, after)
}

type ArtifactTaskView struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	ResourceID string           `json:"resource_id"`
	Title      string           `json:"title"`
	Status     string           `json:"status"`
	Stage      string           `json:"stage"`
	CanCancel  bool             `json:"can_cancel"`
	CanRetry   bool             `json:"can_retry"`
	CanResume  bool             `json:"can_resume"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Run        *ArtifactRunView `json:"run"`
}

func (s *ArtifactService) Tasks(ctx context.Context, owner int64, page, size int) ([]ArtifactTaskView, int64, error) {
	runs, videos, total, err := s.repos.Artifact.TaskRows(ctx, owner, page, size)
	if err != nil {
		return nil, 0, err
	}
	all := []ArtifactTaskView{}
	for _, r := range runs {
		v, e := s.Run(ctx, owner, r.ID)
		if e != nil {
			return nil, 0, e
		}
		all = append(all, ArtifactTaskView{ID: "artifact:" + r.ID, Type: "artifact_generation", ResourceID: r.ID, Title: r.Goal, Status: r.Status, Stage: r.Stage, CanCancel: v.CanCancel, CanRetry: v.CanRetry, CanResume: v.CanResume, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Run: v})
	}
	for _, v := range videos {
		title := v.Title
		if title == "" {
			title = v.Filename
		}
		all = append(all, ArtifactTaskView{ID: fmt.Sprintf("video:%d", v.ID), Type: "video_processing", ResourceID: strconv.FormatInt(v.ID, 10), Title: title, Status: strconv.Itoa(int(v.Status)), Stage: v.Stage, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	return all, total, nil
}
