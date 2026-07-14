package observability

import "context"

// Correlation is a set of business identifiers used to join logs, task rows and
// AI audit records. It is intentionally not an OpenTelemetry span context.
type Correlation struct {
	TraceID string
	TaskID  int64
	JobID   int64
	UserID  int64
	JobType string
	Stage   string
	Attempt int
}

type correlationContextKey struct{}

// WithCorrelation merges non-zero fields into the correlation already carried
// by ctx. This keeps parent identifiers when a child only changes its stage.
func WithCorrelation(ctx context.Context, fields Correlation) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	merged := CorrelationFromContext(ctx)
	if fields.TraceID != "" {
		merged.TraceID = fields.TraceID
	}
	if fields.TaskID != 0 {
		merged.TaskID = fields.TaskID
	}
	if fields.JobID != 0 {
		merged.JobID = fields.JobID
	}
	if fields.UserID != 0 {
		merged.UserID = fields.UserID
	}
	if fields.JobType != "" {
		merged.JobType = fields.JobType
	}
	if fields.Stage != "" {
		merged.Stage = fields.Stage
	}
	if fields.Attempt != 0 {
		merged.Attempt = fields.Attempt
	}
	return context.WithValue(ctx, correlationContextKey{}, merged)
}

func CorrelationFromContext(ctx context.Context) Correlation {
	if ctx == nil {
		return Correlation{}
	}
	fields, _ := ctx.Value(correlationContextKey{}).(Correlation)
	return fields
}
