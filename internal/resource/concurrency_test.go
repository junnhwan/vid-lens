package resource_test

import (
	"context"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func TestGenerationResourcesFollowLiveLimitsAndDrainHigherSlots(t *testing.T) {
	s, rm, _ := testsupport.Services(t)
	ctx := context.Background()
	v, err := s.SaveExecutionSettings(ctx, store.ExecutionInput{MaxVideoRunning: 4})
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := s.NewSession(ctx, domain.ID(), time.Hour)
	var claims []domain.Claim
	for _, url := range []string{"local://one.mp4", "local://two.mp4", "local://three.mp4", "local://four.mp4"} {
		a, err := s.CreateRequest(ctx, owner.ID, domain.ID(), url, "", true)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := s.Request(ctx, owner.ID, a.ID)
		if _, err = s.Pool.Exec(ctx, "UPDATE job_runs SET kind='article.generate' WHERE id=$1", *r.ActiveRunID); err != nil {
			t.Fatal(err)
		}
		c, err := s.Claim(ctx, *r.ActiveRunID, 1, domain.ID())
		if err != nil || c == nil {
			t.Fatalf("claim: %v", err)
		}
		claims = append(claims, *c)
	}
	releases := make([]func(), 3)
	for n := range releases {
		_, releases[n], err = rm.Acquire(ctx, claims[n], "ai")
		if err != nil {
			t.Fatal(err)
		}
		defer releases[n]()
	}
	releases[0]() // slot zero is free while two higher slots remain occupied.
	v, err = s.SaveExecutionSettings(ctx, store.ExecutionInput{ExpectedRevision: v.Revision, MaxVideoRunning: 1})
	if err != nil {
		t.Fatal(err)
	}
	blocked := func() {
		t.Helper()
		limited, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		if _, release, err := rm.Acquire(limited, claims[3], "ai"); err == nil {
			release()
			t.Fatal("new call escaped the lowered generation limit")
		}
	}
	blocked()
	question := claims[3]
	question.Run.Kind = "qa.answer"
	questionCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	_, release, err := rm.Acquire(questionCtx, question, "ai")
	if err != nil {
		t.Fatalf("question lane blocked by generations: %v", err)
	}
	release()
	releases[1]()
	blocked()
	releases[2]()
	_, release, err = rm.Acquire(questionCtx, claims[3], "ai")
	if err != nil {
		t.Fatalf("last old call drained but no new call admitted: %v", err)
	}
	release()
	if _, err = s.SaveExecutionSettings(ctx, store.ExecutionInput{ExpectedRevision: v.Revision, MaxVideoRunning: 10}); err != nil {
		t.Fatal(err)
	}
	for n := range releases {
		_, releases[n], err = rm.Acquire(ctx, claims[n], "ai")
		if err != nil {
			t.Fatal(err)
		}
		defer releases[n]()
	}
}
