package service

import (
	"context"
	"strings"
	"testing"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

func TestOperationAdmissionDoesNotRequireUnrelatedModels(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.User.Create(&model.User{ID: 101, Username: "partial"}); err != nil {
		t.Fatal(err)
	}
	profile := ai.Profile{LLMProvider: "openai", LLMBaseURL: "https://example.com/v1", LLMAPIKey: "test", LLMModel: "llm"}
	svc := NewUserService(repos.User, config.JWTConfig{}).WithOptionalCapabilities(optionalProfileFixture{profile}, true, DefaultRAGRetrievalConfig(), true)
	view, err := svc.OptionalCapabilities(context.Background(), 101)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"summary", "study", "revise", "upload", "align"} {
		if !view.Actions[action].Allowed {
			t.Fatalf("%s blocked: %+v", action, view.Actions[action])
		}
	}
	for _, action := range []string{"chat", "agent", "index", "transcribe", "caption", "ocr"} {
		if view.Actions[action].Allowed {
			t.Fatalf("%s admitted without dependencies", action)
		}
	}
	for _, state := range view.Capabilities {
		if state.Health != "unchecked" {
			t.Fatal("configuration promoted to health proof")
		}
	}
}

type changingCapabilityProfile struct {
	profile ai.Profile
	calls   int
}

func (f *changingCapabilityProfile) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	f.calls++
	p := f.profile
	return &p, nil
}

func TestSubmissionRechecksCurrentProfileAndOwnership(t *testing.T) {
	repos := newMediaTestRepositories(t)
	producer := &recordingMediaProducer{}
	task := &model.VideoTask{UserID: 7, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "fixture", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	seedMediaTestTranscription(t, repos, task)
	profiles := &changingCapabilityProfile{profile: ai.Profile{LLMProvider: "openai", LLMBaseURL: "https://example.com/v1", LLMAPIKey: "fixture", LLMModel: "llm"}}
	// The read was successful, but the current default can change before POST.
	if err := ai.RequireAction(profiles.profile, "summary"); err != nil {
		t.Fatal(err)
	}
	profiles.profile = ai.Profile{}
	svc := (&MediaService{repo: repos, mq: producer}).WithAIProfiles(profiles)
	if err := svc.RequestAnalysis(context.Background(), 7, task.ID, false); err == nil || !strings.Contains(err.Error(), "llm") {
		t.Fatalf("missing config admitted: %v", err)
	}
	if err := svc.RequestAnalysis(context.Background(), 8, task.ID, false); err == nil || !strings.Contains(err.Error(), "无权") {
		t.Fatalf("ownership ignored: %v", err)
	}
	if profiles.calls != 1 {
		t.Fatal("profile was resolved before resource authorization")
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	if err != nil || job != nil {
		t.Fatalf("denied submission dispatched: %+v %v", job, err)
	}
}

func TestPausedHostedProfileDoesNotBlockLocalAlignment(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.User.Create(&model.User{ID: 101, Username: "paused"}); err != nil {
		t.Fatal(err)
	}
	svc := NewUserService(repos.User, config.JWTConfig{}).WithOptionalCapabilities(unavailableHostedProfileFixture{}, true, DefaultRAGRetrievalConfig(), true)
	view, err := svc.OptionalCapabilities(context.Background(), 101)
	if err != nil || view.Actions["summary"].ReasonCode != "hosted_paused" || !view.Actions["align"].Allowed || !view.Actions["upload"].Allowed {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}
