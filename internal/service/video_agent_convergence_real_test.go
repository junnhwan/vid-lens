//go:build real_llm

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	appdb "vid-lens/internal/database"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

// Production supplies a read-only run/profile. This test sends only bounded
// planner calls; it never starts a production run or writes chat messages.
func TestVideoAgentConvergenceRealPlannerProbe(t *testing.T) {
	path, runID := os.Getenv("VIDLENS_AGENT_REPLAY_CONFIG"), os.Getenv("VIDLENS_AGENT_REPLAY_RUN")
	if path == "" || runID == "" {
		t.Skip("set source config and run ID to opt into a read-only real planner probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("source config unavailable")
	}
	connection, err := appdb.OpenPostgres(ctx, cfg.Database)
	if err != nil {
		t.Fatal("source database unavailable")
	}
	defer connection.Close()
	connection.SQL.SetMaxOpenConns(1)
	db := connection.GORM
	if err := db.Exec("SET default_transaction_read_only=on").Error; err != nil {
		t.Fatal(err)
	}
	var source model.AgentRun
	if err := db.First(&source, "id=?", runID).Error; err != nil {
		t.Fatal("source run unavailable")
	}
	var frozenProfile frozenAgentProfile
	if err := json.Unmarshal([]byte(source.ProfileSnapshot), &frozenProfile); err != nil {
		t.Fatal(err)
	}
	passphrase := cfg.Security.APIKeySecret
	if passphrase == "" {
		passphrase = cfg.JWT.Secret
	}
	codec, err := secret.NewCodecFromPassphrase(passphrase)
	if err != nil {
		t.Fatal("source profile codec unavailable")
	}
	repos := repository.NewRepositories(db)
	profiles := NewAIProfileService(repos.AIProfile, codec, nil)
	row, err := profiles.repo.FindByIDForUser(source.UserID, frozenProfile.ProfileID)
	if err != nil || row == nil {
		t.Fatal("source profile unavailable")
	}
	decrypted, err := profiles.decryptProfile(row)
	if err != nil {
		t.Fatal("source profile unavailable")
	}
	profile := providerFromDecrypted(decrypted)
	if profile.LLMModel != frozenProfile.LLMModel {
		t.Fatal("source model changed")
	}
	client, err := ai.NewFactory().NewChatClient(*profile)
	if err != nil {
		t.Fatal("source chat client unavailable")
	}
	records, err := repos.AgentExecution.GetExecution(ctx, source.UserID, source.ID)
	if err != nil || records == nil {
		t.Fatal("source checkpoints unavailable")
	}
	policy := DefaultVideoAgentLoopPolicy()
	state, _ := NewVideoAgentLoopState(source.Goal, policy)
	for number := 1; number <= 3; number++ {
		step, _, err := completedResearchRecord(records, fmt.Sprintf("tool-%d", number))
		if err != nil || step == nil {
			t.Fatal("source tool checkpoint unavailable")
		}
		var checkpoint durableResearchToolCheckpoint
		if err := json.Unmarshal([]byte(step.ResultCheckpoint), &checkpoint); err != nil {
			t.Fatal(err)
		}
		state.CurrentStep++
		state.Evidence = mergeResearchProgressEvidence(state.Evidence, checkpoint.Observation.NewEvidence, 1)
	}
	definitions := NewVideoAgentTools(nil, nil, nil).Registry().Definitions()
	// Both samples use the updated prompt and the same captured evidence.
	// They isolate request mode, not end-to-end retrieval or semantic quality.
	messages, err := buildPlannerMessages(state, definitions)
	if err != nil {
		t.Fatal(err)
	}
	var baselineUsage ai.ChatUsage
	baselineCtx := ai.WithChatBudget(ctx, 1024, func(usage ai.ChatUsage) { baselineUsage = usage })
	started := time.Now()
	response, err := client.Chat(baselineCtx, messages)
	if err != nil {
		t.Fatal("standard planner probe failed")
	}
	baseline, err := parseLLMVideoAgentLoopDecision(response)
	if err != nil {
		t.Fatal("standard planner probe returned invalid JSON")
	}
	t.Logf("standard mode: model=%s duration_ms=%d completion_tokens=%d reasoning_tokens=%d tool=%s", profile.LLMModel, time.Since(started).Milliseconds(), baselineUsage.CompletionTokens, baselineUsage.ReasoningTokens, baseline.Tool)
	started = time.Now()
	decision, usage, err := NewLLMVideoAgentLoopPlanner(client).NextDecisionWithUsage(ctx, state, definitions)
	if err != nil {
		t.Fatal("structured planner probe failed")
	}
	runner, err := NewVideoAgentLoopRunner(NewVideoAgentTools(nil, nil, nil).Registry(), NewLLMVideoAgentLoopPlanner(client), DefaultVideoAgentLoopObserver{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.validateDecision(state, decision); err != nil {
		t.Fatal("structured planner decision invalid")
	}
	t.Logf("structured mode: model=%s duration_ms=%d completion_tokens=%d tool=%s; production read only", profile.LLMModel, time.Since(started).Milliseconds(), usage.CompletionTokens, decision.Tool)
}
