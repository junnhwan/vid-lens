package processingguard

import "context"

type contextKey struct{}

// Guard verifies that the current worker still owns the right to perform the
// next mutable or remote side effect.
type Guard func(context.Context) error

// With attaches a processing guard without coupling service packages to the
// queue implementation that owns the lease.
func With(ctx context.Context, guard Guard) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if guard == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, guard)
}

// Check first honors cancellation (including a lease-loss cancel cause), then
// synchronously revalidates the attached processing guard when present.
func Check(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	guard, _ := ctx.Value(contextKey{}).(Guard)
	if guard == nil {
		return nil
	}
	return guard(ctx)
}
