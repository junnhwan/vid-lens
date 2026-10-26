package store

import "context"

// The HTTP boundary supplies the authenticated actor's submission guard. It is
// evaluated inside the serialized admission transaction, after idempotency
// replay/conflict checks and input validation, before creating durable work.
// A permitted attempt consumes rate tokens even if a later DB operation fails;
// Redis rate tokens are not a transactional or monetary quota.
type SubmissionCheck func(context.Context, string, string) error

type submissionCheckKey struct{}

func WithSubmissionCheck(ctx context.Context, check SubmissionCheck) context.Context {
	return context.WithValue(ctx, submissionCheckKey{}, check)
}

func checkSubmission(ctx context.Context, kind, session string) error {
	if check, ok := ctx.Value(submissionCheckKey{}).(SubmissionCheck); ok {
		return check(ctx, kind, session)
	}
	return nil // Internal workers and isolated Store callers are not HTTP submissions.
}
