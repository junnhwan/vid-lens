package investigator

import (
	"context"
	"errors"
	"github.com/cloudwego/eino/schema"
	"strings"
	"time"
	"vidlens/internal/store"
)

func (e *Engine) RunProbe(ctx context.Context, p store.Probe) error {
	ctx, cancel := context.WithDeadline(ctx, p.DeadlineAt)
	defer cancel()
	// Keep the probe claim alive while it waits for the shared AI lane, too.
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if e.Store.TouchProbe(ctx, p) != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	actx, release, err := e.Resources.AcquireProbe(ctx, p)
	if err != nil {
		return err
	}
	defer release()
	cid, err := e.Store.ProbeProfileID(ctx, p)
	if err != nil {
		return err
	}
	profile, err := e.Store.Profile(ctx, cid)
	if err != nil {
		return err
	}
	profile.MaxOutputTokens = 512
	reports := []usageReport{}
	observer := newUsageObserver("source-context-v2", profile.CachePolicy, nil)
	submission := 0
	observer.flush = func(report usageReport) {
		reports = append(reports, report)
		c, cc := context.WithTimeout(context.Background(), 2*time.Second)
		defer cc()
		_ = e.Store.RecordProbeSubmission(c, p, submission, report)
	}
	callctx := context.WithValue(actx, usageKey{}, observer)
	callctx = context.WithValue(callctx, remoteSubmitKey{}, func() error {
		var err error
		submission, err = e.Store.BeginProbeSubmission(actx, p)
		if err == nil {
			observer = newUsageObserver("source-context-v2", profile.CachePolicy, observer.flush)
		}
		return err
	})
	// A context-scoped observer is reset by each physical RoundTrip, including
	// capability-agent tool rounds. Each report remains linked to its submission.
	callctx = context.WithValue(callctx, probeObserverKey{}, func() *usageObserver { return observer })
	var result any
	if p.Mode == "capabilities" {
		result, err = e.CheckModel(callctx, profile)
	} else {
		profile.CacheHintsVerified = profile.CachePolicy == "qwen_explicit"
		base, be := e.baseModel(callctx, profile)
		if be != nil {
			return be
		}
		prefix := strings.Repeat("This is a fixed source context for a serial cache reporting check. No private video or conversation is included. ", 160)
		for _, question := range []string{"Reply with OK.", "Reply with READY."} {
			_, err = completeModelResponse(callctx, base, []*schema.Message{schema.SystemMessage("Follow the supplied check instruction."), schema.UserMessage(prefix), schema.UserMessage("Stable cache policy check."), schema.UserMessage(question)}, cacheOptions(profile)...)
			if err != nil {
				break
			}
		}
		if err == nil {
			reported, hit, created := false, false, false
			for _, r := range reports {
				reported = reported || r.CacheRead.Status == "reported"
				hit = hit || r.CacheRead.Value != nil && *r.CacheRead.Value > 0
				created = created || r.CacheWrite.Value != nil && *r.CacheWrite.Value > 0
			}
			result = map[string]any{"usage_reported": reported, "cache_hit_observed": hit, "cache_creation_observed": created, "cost_reporting": "unknown", "hints_verified": profile.CachePolicy == "qwen_explicit" && (hit || created), "samples": reports}
		}
	}
	code := ""
	if err != nil {
		code = "CAPABILITY_CHECK_FAILED"
		if p.Mode == "cache" {
			code = "CACHE_CHECK_FAILED"
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code = "PROVIDER_OUTCOME_UNKNOWN"
		}
	}
	c, cc := context.WithTimeout(context.Background(), 3*time.Second)
	defer cc()
	return e.Store.FinishProbe(c, p, result, code)
}

type probeObserverKey struct{}
