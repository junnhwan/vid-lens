package domain

import "context"

type traceKey struct{}

func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}
func TraceID(ctx context.Context) string { id, _ := ctx.Value(traceKey{}).(string); return id }
