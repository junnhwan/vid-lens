package repository

import (
	"errors"
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestUsageLedgerReservationSettlementReleaseAreIdempotent(t *testing.T) {
	repos := newGovernanceTestRepos(t)
	now := time.Date(2026, 7, 13, 23, 59, 59, 0, time.FixedZone("CST", 8*3600))
	req := UsageReservation{IdempotencyKey: "call-1", UserID: 8, TaskID: 3, Kind: model.AICallKindLLM, Provider: "mock", Model: "m", Unit: model.UsageUnitToken, ReservedUnits: 100, UsageDate: "2026-07-13", ExpiresAt: now.Add(time.Hour), Now: now}
	ledger, created, event, err := repos.UsageLedger.Reserve(req)
	if err != nil || !created || ledger.Status != model.UsageLedgerReserved || event == nil || event.DeltaUnits != 100 {
		t.Fatalf("reserve ledger=%+v created=%v event=%+v err=%v", ledger, created, event, err)
	}
	same, created, event, err := repos.UsageLedger.Reserve(req)
	if err != nil || created || event != nil || same.ID != ledger.ID {
		t.Fatalf("duplicate reserve created=%v event=%+v err=%v", created, event, err)
	}

	prompt, completion, total := int64(40), int64(20), int64(60)
	settled, changed, settleEvent, err := repos.UsageLedger.Settle("call-1", UsageSettlement{ActualUnits: floatPtr(60), PromptTokens: &prompt, CompletionTokens: &completion, TotalTokens: &total, UsageSource: model.UsageSourceActual, ProviderRequestID: "req-1", Now: now.Add(time.Minute)})
	if err != nil || !changed || settled.Status != model.UsageLedgerSettled || settleEvent.DeltaUnits != -40 {
		t.Fatalf("settle=%+v changed=%v event=%+v err=%v", settled, changed, settleEvent, err)
	}
	again, changed, event, err := repos.UsageLedger.Settle("call-1", UsageSettlement{ActualUnits: floatPtr(60), PromptTokens: &prompt, CompletionTokens: &completion, TotalTokens: &total, UsageSource: model.UsageSourceActual, ProviderRequestID: "req-1", Now: now.Add(2 * time.Minute)})
	if err != nil || changed || event != nil || again.Status != model.UsageLedgerSettled {
		t.Fatalf("replay settle changed=%v event=%+v err=%v", changed, event, err)
	}
	if _, _, _, err := repos.UsageLedger.Release("call-1", "late failure", now); !errors.Is(err, ErrUsageLedgerTerminal) {
		t.Fatalf("release settled err=%v", err)
	}

	req.IdempotencyKey = "call-2"
	req.ReservedUnits = 30
	if _, _, _, err := repos.UsageLedger.Reserve(req); err != nil {
		t.Fatal(err)
	}
	released, changed, releaseEvent, err := repos.UsageLedger.Release("call-2", "provider failed", now.Add(time.Minute))
	if err != nil || !changed || released.Status != model.UsageLedgerReleased || releaseEvent.DeltaUnits != -30 {
		t.Fatalf("release=%+v changed=%v event=%+v err=%v", released, changed, releaseEvent, err)
	}
}

func TestUsageLedgerUnknownMeasurementsRemainNullAndDateIsFrozen(t *testing.T) {
	repos := newGovernanceTestRepos(t)
	now := time.Date(2026, 7, 13, 23, 59, 59, 0, time.UTC)
	_, _, _, err := repos.UsageLedger.Reserve(UsageReservation{IdempotencyKey: "unknown", UserID: 1, Kind: model.AICallKindASR, Unit: model.UsageUnitSecond, ReservedUnits: 120, UsageDate: "2026-07-13", ExpiresAt: now.Add(time.Hour), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	got, changed, event, err := repos.UsageLedger.Settle("unknown", UsageSettlement{UsageSource: model.UsageSourceUnknown, Now: now.Add(2 * time.Second)})
	if err != nil || !changed || got.ActualUnits != nil || got.TotalTokens != nil || got.EstimatedCost != nil || got.UsageDate != "2026-07-13" || event.DeltaUnits != 0 {
		t.Fatalf("got=%+v event=%+v err=%v", got, event, err)
	}
}

func floatPtr(v float64) *float64 { return &v }
