package config

import (
	"encoding/json"
	"errors"
)

// QuestionTiming is admitted with a run, independently of the immutable model
// route. Restoring an older run never grants it a new time budget.
type QuestionTiming struct {
	Revision             string `json:"revision"`
	ActiveSeconds        int64  `json:"active_seconds"`
	WallSeconds          int64  `json:"wall_seconds"`
	AuthorCallSeconds    int64  `json:"author_call_seconds"`
	ReviewCallSeconds    int64  `json:"review_call_seconds"`
	FirstActivitySeconds int64  `json:"first_activity_seconds"`
	IdleSeconds          int64  `json:"idle_seconds"`
}

func NewQuestionTiming() *QuestionTiming {
	return &QuestionTiming{
		Revision: "qa-timing-2026-10-30-v1", ActiveSeconds: 1200, WallSeconds: 1500,
		AuthorCallSeconds: 600, ReviewCallSeconds: 600, FirstActivitySeconds: 120, IdleSeconds: 120,
	}
}

func (p *QuestionTiming) Valid() bool {
	return p != nil && *p == *NewQuestionTiming()
}

// ForRun is shared by admission/claim, queue delivery, execution and recovery.
// The absence of a policy deliberately retains the historic profile limits.
func (p Profile) ForRun(kind string, checkpoint json.RawMessage) (Profile, error) {
	if kind != "qa.answer" {
		return p, nil
	}
	var cp struct {
		Timing *QuestionTiming `json:"question_timing"`
	}
	if len(checkpoint) > 0 {
		if err := json.Unmarshal(checkpoint, &cp); err != nil {
			return p, err
		}
	}
	if cp.Timing == nil {
		return p, nil
	}
	if !cp.Timing.Valid() {
		return p, errors.New("unsupported question timing policy")
	}
	p.AnswerActiveSeconds = cp.Timing.ActiveSeconds
	p.AnswerSeconds = cp.Timing.WallSeconds
	p.RestoreAnswerSeconds = max(p.RestoreAnswerSeconds, p.AnswerSeconds)
	return p, nil
}
