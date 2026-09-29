package visualprogress

import "context"

type attemptKey struct{}
type jobTypeKey struct{}

// WithAttempt identifies the media processing attempt that owns visual work.
// The token never appears in public API responses.
func WithAttempt(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, attemptKey{}, token)
}

func WithJobAttempt(ctx context.Context, token, jobType string) context.Context {
	return context.WithValue(WithAttempt(ctx, token), jobTypeKey{}, jobType)
}

func JobType(ctx context.Context) string {
	if ctx != nil {
		if job, _ := ctx.Value(jobTypeKey{}).(string); job != "" {
			return job
		}
	}
	return "transcribe"
}

func Attempt(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	token, _ := ctx.Value(attemptKey{}).(string)
	return token
}
