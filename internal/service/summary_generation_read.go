package service

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
)

type SummaryGenerationReadService struct{ repos *repository.Repositories }

func NewSummaryGenerationReadService(repos *repository.Repositories) *SummaryGenerationReadService {
	return &SummaryGenerationReadService{repos}
}

type SummaryGenerationSourceView struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Digest   string `json:"digest"`
	Language string `json:"language"`
	Quality  string `json:"quality"`
}
type SummaryGenerationActivity struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	Title      string     `json:"title"`
	Detail     string     `json:"detail,omitempty"`
	Attempt    int        `json:"attempt"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMS int64      `json:"duration_ms"`
}
type SummaryGenerationView struct {
	GeneratedContentDigest string                       `json:"generated_content_digest,omitempty"`
	GeneratedSourceID      string                       `json:"generated_source_id,omitempty"`
	GeneratedSourceDigest  string                       `json:"generated_source_digest,omitempty"`
	VisualRetryAvailable   bool                         `json:"visual_retry_available"`
	Operation              string                       `json:"operation,omitempty"`
	ParentGenerationID     string                       `json:"parent_generation_id,omitempty"`
	MindmapEnabled         *bool                        `json:"mindmap_enabled,omitempty"`
	TaskID                 int64                        `json:"task_id"`
	GenerationID           string                       `json:"generation_id"`
	ResultGenerationID     string                       `json:"result_generation_id"`
	Legacy                 bool                         `json:"legacy"`
	RequestedMode          string                       `json:"requested_mode"`
	ResolvedMode           string                       `json:"resolved_mode"`
	TextState              string                       `json:"text_state"`
	VisualState            string                       `json:"visual_state"`
	ResultState            string                       `json:"result_state"`
	Stage                  string                       `json:"stage"`
	Status                 string                       `json:"status"`
	StopReason             string                       `json:"stop_reason,omitempty"`
	FallbackReason         string                       `json:"fallback_reason,omitempty"`
	SourceStatus           string                       `json:"source_status"`
	Source                 *SummaryGenerationSourceView `json:"source,omitempty"`
	GeneratedVersion       int64                        `json:"generated_version"`
	ContentDigest          string                       `json:"content_digest"`
	ContentHashKind        string                       `json:"content_hash_kind"`
	Activities             []SummaryGenerationActivity  `json:"activities"`
	EventHighWatermark     int64                        `json:"event_high_watermark"`
}
type SummaryGenerationEvent struct {
	Seq       int64          `json:"seq"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}
type SummaryGenerationEventPage struct {
	GenerationID  string                   `json:"generation_id"`
	Events        []SummaryGenerationEvent `json:"events"`
	HighWatermark int64                    `json:"high_watermark"`
	NextAfterSeq  int64                    `json:"next_after_seq"`
	HasMore       bool                     `json:"has_more"`
	CursorGap     bool                     `json:"cursor_gap"`
}

func (s *SummaryGenerationReadService) Latest(ctx context.Context, owner, taskID int64) (*SummaryGenerationView, error) {
	read, err := s.repos.ReadSummaryGeneration(ctx, owner, taskID, "")
	if err != nil {
		return nil, err
	}
	view := &SummaryGenerationView{TaskID: taskID, GenerationID: read.GenerationID, Legacy: read.GenerationID == "", RequestedMode: "auto", TextState: "pending", VisualState: "not_requested", ResultState: "pending", Stage: read.Task.Stage, Status: generationTaskStatus(read.Task.Status), Activities: []SummaryGenerationActivity{}}
	var frozenOptions *processing.Options
	if read.Intent != nil && read.Intent.GenerationID == read.GenerationID {
		options := read.Intent.Options
		frozenOptions = &options
	}
	if read.Job != nil && read.Job.GenerationID == read.GenerationID && read.Job.InputSnapshotJSON != "" {
		var snapshot processing.GenerationSnapshot
		if json.Unmarshal([]byte(read.Job.InputSnapshotJSON), &snapshot) == nil && snapshot.Intent.GenerationID == read.GenerationID {
			if snapshot.Operation == processing.OperationVisualRetry && snapshot.VisualRetry != nil {
				view.Operation = snapshot.Operation
				view.ParentGenerationID = snapshot.VisualRetry.ParentGenerationID
				view.TextState = "ready"
			}
			if intent, err := processing.Decode(artifact.JSON(snapshot.Intent)); err == nil {
				options := intent.Options
				frozenOptions = &options
			}
		}
	}
	if frozenOptions != nil {
		view.RequestedMode = frozenOptions.OutputMode
		mindmap := frozenOptions.MindmapEnabled
		view.MindmapEnabled = &mindmap
		if frozenOptions.SummaryVisualEnabled {
			view.VisualState = "pending"
		}
	}
	if read.Job != nil && read.Job.GenerationID == read.GenerationID {
		view.Stage = read.Job.Stage
		view.Status = generationTaskStatus(read.Job.Status)
		if read.Job.NextRetryAt != nil {
			view.Status = "retry_waiting"
		}
	}
	if read.Source != nil {
		view.Source = &SummaryGenerationSourceView{read.Source.ID, read.Source.Kind, read.Source.SourceDigest, read.Source.Language, read.Source.Quality}
	}
	if effective := read.Effective; effective != nil {
		view.SourceStatus = effective.SourceStatus
		view.ContentDigest = effective.ContentDigest
		view.ContentHashKind = effective.ContentHashKind
		if effective.Content != "" {
			view.ResultState = "ready"
			view.ResolvedMode = "text"
			if effective.Document != nil {
				view.ResolvedMode = effective.Document.PresentationMode
			}
		}
		if effective.Generated != nil {
			view.GeneratedContentDigest = effective.Generated.ContentDigest
			view.GeneratedSourceID = effective.Generated.SourceID
			view.GeneratedSourceDigest = effective.Generated.SourceDigest
			if generatedDoc, parseErr := summarydoc.Parse([]byte(effective.Generated.DocumentJSON)); parseErr == nil && generatedDoc.PresentationMode == "text" && read.Task.ActiveTextSourceID == effective.Generated.SourceID && !repository.SummaryJobActive(read.Job) && read.Task.VisualCaptionAllowed() && read.Task.EffectiveVisualMode() != model.VisualModeOff {
				view.VisualRetryAvailable = true
			}
			view.GeneratedVersion = effective.Generated.GeneratedVersion
			view.ResultGenerationID = effective.Generated.GenerationID
			if read.GenerationID == "" || effective.Generated.GenerationID == read.GenerationID {
				view.TextState = "ready"
			}
		}
	}
	if run := read.Run; run != nil {
		view.Stage = run.Stage
		view.Status = run.Status
		view.RequestedMode = run.Mode
		view.StopReason = run.StopReason
		view.EventHighWatermark = run.EventSeq
		activityMap := map[string]*SummaryGenerationActivity{}
		for _, step := range read.Steps {
			state := "running"
			switch step.Status {
			case model.AgentStepStatusCompleted:
				state = "done"
			case model.AgentStepStatusFailed, model.AgentStepStatusAmbiguous:
				state = "error"
			}
			activity := &SummaryGenerationActivity{ID: step.StepID, Kind: step.Kind, State: state, Title: firstNonEmpty(safeGenerationActivity(step.SafeReason, 40), "处理视频摘要"), Attempt: step.Attempt, StartedAt: step.StartedAt, FinishedAt: step.FinishedAt, DurationMS: step.DurationMs}
			prior := activityMap[step.StepID]
			if prior == nil || prior.Attempt <= activity.Attempt {
				activityMap[step.StepID] = activity
			}
		}
		for _, event := range read.Events {
			data := publicSummaryGenerationEventData(event.DataJSON)
			if event.Type == "run.text_ready" || event.Type == "run.text_reused" {
				view.TextState = "ready"
			}
			if event.Type == "run.retry_waiting" && run.Status == "running" {
				view.Status = "retry_waiting"
			}
			if event.Type == "run.visual_started" && run.Status == "running" {
				view.VisualState = "running"
			}
			if event.Type == "run.completed" {
				if value, ok := data["visual_state"].(string); ok {
					view.VisualState = value
				}
				if value, ok := data["fallback_reason"].(string); ok {
					view.FallbackReason = value
				}
			}
			if event.Type == "activity.started" && run.Status == "running" {
				view.Status = "running"
			}
			if event.Type != "activity.started" && event.Type != "activity.finished" {
				continue
			}
			id, _ := data["activity_id"].(string)
			if id == "" {
				continue
			}
			attempt := 1
			if number, ok := data["attempt"].(float64); ok {
				attempt = int(number)
			}
			activity := activityMap[id]
			if activity == nil {
				activity = &SummaryGenerationActivity{ID: id, Attempt: attempt, Title: "保存摘要", State: "running", StartedAt: event.CreatedAt}
				activityMap[id] = activity
			}
			if attempt < activity.Attempt {
				continue
			}
			activity.Attempt = attempt
			if value, ok := data["title"].(string); ok && value != "" {
				activity.Title = value
			}
			if value, ok := data["detail"].(string); ok {
				activity.Detail = value
			}
			if value, ok := data["kind"].(string); ok {
				activity.Kind = value
			}
			if event.Type == "activity.finished" {
				if value, ok := data["state"].(string); ok {
					activity.State = value
				}
				now := event.CreatedAt
				activity.FinishedAt = &now
			}
		}
		for _, activity := range activityMap {
			if activity.State == "running" && (run.Status == "cancelled" || run.Status == "failed" || run.Status == "budget_exhausted") {
				activity.State = "error"
				if run.Status == "cancelled" {
					activity.State = "cancelled"
				}
				activity.FinishedAt = run.FinishedAt
			}
			if activity.FinishedAt != nil {
				activity.DurationMS = max(0, activity.FinishedAt.Sub(activity.StartedAt).Milliseconds())
			}
			view.Activities = append(view.Activities, *activity)
		}
		sort.SliceStable(view.Activities, func(i, j int) bool {
			if view.Activities[i].StartedAt.Equal(view.Activities[j].StartedAt) {
				return view.Activities[i].ID < view.Activities[j].ID
			}
			return view.Activities[i].StartedAt.Before(view.Activities[j].StartedAt)
		})
		if run.Status == "cancelled" {
			if view.TextState != "ready" {
				view.TextState = "cancelled"
			}
			if view.VisualState == "running" || view.VisualState == "pending" {
				view.VisualState = "cancelled"
			}
		}
		if run.Status == "failed" || run.Status == "budget_exhausted" {
			if view.TextState != "ready" {
				view.TextState = "failed"
			}
			if view.VisualState == "running" || view.VisualState == "pending" {
				view.VisualState = "failed"
			}
		}
	}
	return view, nil
}
func (s *SummaryGenerationReadService) Events(ctx context.Context, owner, taskID int64, generation string, after int64, limit int) (*SummaryGenerationEventPage, error) {
	read, events, gap, err := s.repos.ReadSummaryGenerationEvents(ctx, owner, taskID, generation, after, limit)
	if err != nil {
		return nil, err
	}
	page := &SummaryGenerationEventPage{GenerationID: generation, Events: []SummaryGenerationEvent{}, NextAfterSeq: after, CursorGap: gap}
	if read.Run != nil {
		page.HighWatermark = read.Run.EventSeq
	}
	for _, event := range events {
		page.Events = append(page.Events, SummaryGenerationEvent{event.Seq, event.Type, publicSummaryGenerationEventData(event.DataJSON), event.CreatedAt})
		page.NextAfterSeq = event.Seq
	}
	page.HasMore = page.NextAfterSeq < page.HighWatermark
	return page, nil
}
func generationTaskStatus(status int8) string {
	switch status {
	case model.TaskStatusPending:
		return "pending"
	case model.TaskStatusQueued:
		return "queued"
	case model.TaskStatusRunning:
		return "running"
	case model.TaskStatusCompleted:
		return "completed"
	case model.TaskStatusFailed:
		return "failed"
	case model.TaskStatusDead:
		return "failed"
	}
	return "pending"
}

// The public reader does not serialize AgentRun, checkpoints, provider inputs,
// raw errors, profile snapshots or arbitrary future event payload fields.
func publicSummaryGenerationEventData(raw string) map[string]any {
	var data map[string]any
	_ = json.Unmarshal([]byte(raw), &data)
	out := map[string]any{}
	for _, key := range []string{"activity_id", "attempt", "kind", "state", "status", "stage", "text_state", "visual_state", "result_state", "generated_version", "content_digest", "requested_mode", "resolved_mode", "fallback_reason", "stop_reason", "started_at", "finished_at", "duration_ms", "next_retry_at", "operation", "parent_generation_id", "result_generation_id"} {
		if value, ok := data[key]; ok {
			out[key] = value
		}
	}
	for _, key := range []string{"title", "detail"} {
		if value, ok := data[key].(string); ok {
			limit := 40
			if key == "detail" {
				limit = 120
			}
			if value = safeGenerationActivity(value, limit); value != "" {
				out[key] = value
			}
		}
	}
	return out
}
