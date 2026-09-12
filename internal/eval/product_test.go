package eval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProductFailuresRemainInDenominatorAndUsageUnknown(t *testing.T) {
	cases := []ProductCase{}
	for _, id := range []string{"success", "failure", "limited", "unsaved"} {
		cases = append(cases, ProductCase{Version: ProductSchemaVersion, ID: id, SourceGroup: "fixture", Split: "dev", Turns: []ProductTurn{{SessionID: 1, Kind: "agent", Question: id}}})
	}
	results, err := RunProductCases(context.Background(), cases, func(_ context.Context, turn ProductTurn) (ProductObservation, error) {
		ob := ProductObservation{Status: "completed", MessageID: 1, Answer: "answer"}
		switch turn.Question {
		case "failure":
			time.Sleep(time.Millisecond)
			return ob, errors.New("save failed")
		case "limited":
			ob.Degraded = true
		case "unsaved":
			ob.MessageID = 0
		}
		return ob, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	d := ProductDistribution(results)
	if d["total"] != 4 || d["completed"] != 1 || d["failed"] != 1 || d["limited"] != 1 || d["pending_confirmation"] != 1 {
		t.Fatal(d)
	}
	if results[1].Turns[0].DurationMS <= 0 {
		t.Fatal("failure latency lost")
	}
	for _, r := range results {
		if r.SemanticSuccess != nil || r.Turns[0].Usage.Source != "unknown" || r.Turns[0].Usage.PromptTokens != nil {
			t.Fatalf("fabricated metric: %+v", r)
		}
	}
}

func TestProductSequenceStopsDependentTurnsAndRetainsCancelledCases(t *testing.T) {
	cases := []ProductCase{{Version: ProductSchemaVersion, ID: "cancel", SourceGroup: "f", Split: "dev", Turns: []ProductTurn{{SessionID: 1, Kind: "chat", Question: "one"}, {SessionID: 1, Kind: "agent", Question: "two"}}}}
	calls := 0
	results, err := RunProductCases(context.Background(), cases, func(context.Context, ProductTurn) (ProductObservation, error) {
		calls++
		return ProductObservation{}, context.Canceled
	})
	if err != nil || calls != 1 || len(results) != 1 || results[0].Classification != "cancelled" {
		t.Fatalf("%+v %v %d", results, err, calls)
	}
}
