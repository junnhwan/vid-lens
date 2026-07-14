package mq

import (
	"context"

	"vid-lens/internal/model"
	"vid-lens/internal/observability"
)

// ContextWithTraceID is kept for message producer compatibility. The value is
// a business correlation ID, not an OpenTelemetry trace/span.
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return observability.WithCorrelation(ctx, observability.Correlation{TraceID: traceID})
}

func TraceIDFromContext(ctx context.Context) string {
	return observability.CorrelationFromContext(ctx).TraceID
}

func contextForTaskJob(ctx context.Context, task *model.VideoTask, job *model.TaskJob) context.Context {
	fields := observability.Correlation{}
	if task != nil {
		fields.TraceID = task.TraceID
		fields.TaskID = task.ID
		fields.UserID = task.UserID
		fields.Stage = task.Stage
		fields.Attempt = task.RetryCount + 1
	}
	if job != nil {
		fields.TraceID = firstNonEmpty(job.TraceID, fields.TraceID)
		fields.TaskID = firstNonZero(job.TaskID, fields.TaskID)
		fields.JobID = job.ID
		fields.UserID = firstNonZero(job.UserID, fields.UserID)
		fields.JobType = job.JobType
		fields.Stage = firstNonEmpty(job.Stage, fields.Stage)
		fields.Attempt = job.RetryCount + 1
	}
	return observability.WithCorrelation(ctx, fields)
}

func contextForTaskJobStage(ctx context.Context, task *model.VideoTask, job *model.TaskJob, stage string) context.Context {
	ctx = contextForTaskJob(ctx, task, job)
	return observability.WithCorrelation(ctx, observability.Correlation{Stage: stage})
}

func contextForTaskJobAttempt(ctx context.Context, task *model.VideoTask, job *model.TaskJob, attempt int) context.Context {
	ctx = contextForTaskJob(ctx, task, job)
	return observability.WithCorrelation(ctx, observability.Correlation{Attempt: attempt})
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
func firstNonZero(value, fallback int64) int64 {
	if value != 0 {
		return value
	}
	return fallback
}
