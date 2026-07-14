package ai

import (
	"context"
	"errors"
	"testing"
	"vid-lens/internal/pkg/quota"
)

type fakeAdmission struct {
	n      int
	denyAt int
}

func (f *fakeAdmission) Admit(context.Context, Call) (quota.Decision, error) {
	f.n++
	if f.n == f.denyAt {
		return quota.Decision{Allowed: false, Scope: "provider"}, nil
	}
	return quota.Decision{Allowed: true}, nil
}

type fakeChat struct{}

func (fakeChat) Chat(context.Context, []ChatMessage) (string, error) { return "ok", nil }

type fakeEmbed struct{}

func (fakeEmbed) Embed(context.Context, string) ([]float32, error) { return []float32{1}, nil }

type fakeStrategy struct{}

func (fakeStrategy) Transcribe(context.Context, string) (string, error)         { return "ok", nil }
func (fakeStrategy) TranscribeChunks(context.Context, []string) (string, error) { return "ok", nil }
func (fakeStrategy) Summarize(context.Context, string) (string, error)          { return "ok", nil }
func TestAdmissionDecoratorsCheckEveryProviderAttempt(t *testing.T) {
	a := &fakeAdmission{denyAt: 2}
	c := AdmitChat(fakeChat{}, a, "p", "m")
	if _, e := c.Chat(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Chat(context.Background(), nil); !errors.Is(e, ErrAdmissionRejected) {
		t.Fatalf("err=%v", e)
	}
	if a.n != 2 {
		t.Fatalf("calls=%d", a.n)
	}
}
func TestEmbeddingAndASRAdmission(t *testing.T) {
	a := &fakeAdmission{}
	_, _ = AdmitEmbedding(fakeEmbed{}, a, "p", "e").Embed(context.Background(), "x")
	s := AdmitStrategy(fakeStrategy{}, a, "p", "a", "l")
	_, _ = s.Transcribe(context.Background(), "x")
	_, _ = s.Summarize(context.Background(), "x")
	if a.n != 3 {
		t.Fatalf("admissions=%d", a.n)
	}
}
