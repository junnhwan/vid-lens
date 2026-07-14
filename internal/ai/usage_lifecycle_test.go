package ai

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type fakeUsageController struct{ reserved, settled, released int }

func (f *fakeUsageController) Reserve(context.Context, Call) (UsageReservation, error) {
	f.reserved++
	return UsageReservation{Key: "k"}, nil
}
func (f *fakeUsageController) Settle(context.Context, UsageReservation, CallResult) error {
	f.settled++
	return nil
}
func (f *fakeUsageController) Release(context.Context, UsageReservation, CallResult) error {
	f.released++
	return nil
}

type resultChat struct{ err error }

func (c resultChat) Chat(context.Context, []ChatMessage) (string, error) { return "", c.err }
func TestAdmissionLifecycleSettlesSuccessAndReleasesFailure(t *testing.T) {
	u := &fakeUsageController{}
	a := &QuotaAdmission{Usage: u}
	ok := AdmitChat(resultChat{}, a, "p", "m")
	_, _ = ok.Chat(context.Background(), []ChatMessage{{Content: "hello"}})
	bad := AdmitChat(resultChat{err: errors.New("boom")}, a, "p", "m")
	_, _ = bad.Chat(context.Background(), nil)
	if u.reserved != 2 || u.settled != 1 || u.released != 1 {
		t.Fatalf("usage=%+v", u)
	}
}

type failingUsageController struct {
	settleErr  error
	releaseErr error
}

func (f failingUsageController) Reserve(context.Context, Call) (UsageReservation, error) {
	return UsageReservation{Key: "ledger-1"}, nil
}
func (f failingUsageController) Settle(context.Context, UsageReservation, CallResult) error {
	return f.settleErr
}
func (f failingUsageController) Release(context.Context, UsageReservation, CallResult) error {
	return f.releaseErr
}

func TestAdmissionLifecycleReportsSettlementAndReleaseFailuresWithoutMaskingProviderResult(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	settleFailure := errors.New("mysql settle unavailable")
	success := AdmitChat(resultChat{}, &QuotaAdmission{Usage: failingUsageController{settleErr: settleFailure}}, "p", "m")
	if _, err := success.Chat(context.Background(), nil); err != nil {
		t.Fatalf("settlement bookkeeping must not turn a successful provider call into a provider failure: %v", err)
	}
	releaseFailure := errors.New("mysql release unavailable")
	providerFailure := errors.New("provider failed")
	failed := AdmitChat(resultChat{err: providerFailure}, &QuotaAdmission{Usage: failingUsageController{releaseErr: releaseFailure}}, "p", "m")
	if _, err := failed.Chat(context.Background(), nil); !errors.Is(err, providerFailure) {
		t.Fatalf("provider error was masked: %v", err)
	}
	text := logs.String()
	if !strings.Contains(text, "AI usage settlement failed") || !strings.Contains(text, "AI usage release failed") {
		t.Fatalf("usage lifecycle failures were silent: %s", text)
	}
}
