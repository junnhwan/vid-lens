package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestQuestionTimingCoordinatesRoundAndCallsWithoutChangingOldRuns(t *testing.T) {
	old := DefaultProfile()
	frozen := NewQuestionTiming()
	raw, err := json.Marshal(map[string]any{"question_timing": frozen})
	if err != nil {
		t.Fatal(err)
	}
	p, err := old.ForRun("qa.answer", raw)
	if err != nil || p.ActiveBudget("qa.answer") != 20*time.Minute || p.AnswerSeconds != 1500 {
		t.Fatal("round budget was not applied", p.AnswerActiveSeconds, p.AnswerSeconds, err)
	}
	if p.AnswerActiveSeconds < frozen.AuthorCallSeconds+frozen.ReviewCallSeconds || p.AnswerSeconds < p.AnswerActiveSeconds+p.AnswerQueueSeconds {
		t.Fatal("queue/round/author/review budgets conflict")
	}
	for _, kind := range []string{"qa.answer", "article.generate"} {
		legacy, err := old.ForRun(kind, json.RawMessage(`{"schema_version":1}`))
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(legacy)
		b, _ := json.Marshal(old)
		if string(a) != string(b) {
			t.Fatal("historic profile changed")
		}
	}
	frozen.ActiveSeconds++
	bad, _ := json.Marshal(map[string]any{"question_timing": frozen})
	if _, err := old.ForRun("qa.answer", bad); err == nil {
		t.Fatal("unrecognized frozen policy accepted")
	}
}
