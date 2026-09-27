package visualprogress

import "context"

type attemptKey struct{}

// WithAttempt identifies the media processing attempt that owns visual work.
// The token never appears in public API responses.
func WithAttempt(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, attemptKey{}, token)
}

func Attempt(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	token, _ := ctx.Value(attemptKey{}).(string)
	return token
}
