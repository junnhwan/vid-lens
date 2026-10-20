package investigator

import (
	"context"
	"errors"
	"github.com/cloudwego/eino/schema"
	"strings"
	"time"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func (e *Engine) RunProbe(ctx context.Context, p store.Probe) (err error) {
	ctx, cancel := context.WithDeadline(ctx, p.DeadlineAt)
	defer cancel()
	var result any
	reports := []usageReport{}
	// Keep the owned execution alive independently of provider response speed.
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
	// Settle every claimed probe, including setup/resource failures and expiry.
	// Retrying queue delivery must never repeat an uncertain provider call.
	defer func() {
		code := probeFailureCode(p.Mode, err, ctx.Err(), reports)
		if code != "" {
			diagnostic := map[string]any{}
			if len(reports) > 0 {
				last := reports[len(reports)-1]
				diagnostic["http_status"] = last.HTTPStatus
				diagnostic["provider_error_code"] = last.ProviderErrorCode
				diagnostic["provider_error_param"] = last.ProviderErrorParam
			}
			result = map[string]any{"diagnostic": diagnostic}
		}
		c, cc := context.WithTimeout(context.Background(), 3*time.Second)
		defer cc()
		err = e.Store.FinishProbe(c, p, result, code)
		if errors.Is(err, domain.ErrFenced) {
			err = nil
		}
	}()
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
	return err
}

type probeObserverKey struct{}

func probeFailureCode(mode string, err, contextErr error, reports []usageReport) string {
	if err == nil && contextErr == nil {
		return ""
	}
	if len(reports) > 0 {
		last := reports[len(reports)-1]
		if last.HTTPStatus != nil {
			switch status := *last.HTTPStatus; {
			case status == 429:
				if last.ProviderErrorCode == "account_cooldown" {
					return "PROVIDER_ACCOUNT_COOLDOWN"
				}
				return "PROVIDER_RATE_LIMITED"
			case status == 401 || status == 403:
				return "PROVIDER_AUTH_FAILED"
			case status == 404 || last.ProviderErrorCode == "model_not_found" || last.ProviderErrorCode == "no_available_channel":
				return "PROVIDER_MODEL_UNAVAILABLE"
			case status >= 500:
				return "PROVIDER_UNAVAILABLE"
			case status >= 400:
				return "PROVIDER_REQUEST_REJECTED"
			}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(contextErr, context.DeadlineExceeded) {
		return "PROBE_EXECUTION_TIMEOUT"
	}
	if errors.Is(err, context.Canceled) || errors.Is(contextErr, context.Canceled) {
		return "PROBE_INTERRUPTED"
	}
	if len(reports) > 0 {
		last := reports[len(reports)-1]
		if last.HTTPStatus == nil || last.StreamStatus != "complete" {
			return "PROVIDER_CONNECTION_FAILED"
		}
	}
	var capabilityErr *capabilityCheckFailure
	if errors.As(err, &capabilityErr) {
		return capabilityErr.code
	}
	var problem *domain.Problem
	if errors.As(err, &problem) {
		if problem.Code == "RESOURCE_LIMIT_EXCEEDED" || problem.Code == "PROBE_RESOURCE_BUSY" {
			return "PROBE_RESOURCE_BUSY"
		}
		return problem.Code
	}
	if mode == "cache" {
		return "CACHE_CHECK_FAILED"
	}
	return "CAPABILITY_CHECK_FAILED"
}
