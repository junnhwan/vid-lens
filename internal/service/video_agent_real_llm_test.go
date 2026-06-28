//go:build real_llm

package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

func TestVideoAgentRealLLMSmoke(t *testing.T) {
	if os.Getenv("VIDLENS_REAL_LLM_SMOKE") != "1" {
		t.Skip("set VIDLENS_REAL_LLM_SMOKE=1 to run real LLM smoke test")
	}
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	repos := repository.NewRepositories(db)
	secretText := cfg.Security.APIKeySecret
	if secretText == "" {
		secretText = cfg.JWT.Secret
	}
	codec, err := secret.NewCodecFromPassphrase(secretText)
	if err != nil {
		t.Fatalf("secret codec: %v", err)
	}
	profileSvc := NewAIProfileService(repos.AIProfile, codec, nil)
	session, profile, chunkCount := findRealLLMAgentFixture(t, repos, db, profileSvc)
	defer func() {
		_ = db.Where("session_id = ?", session.ID).Delete(&model.ChatMessage{}).Error
		_ = db.Delete(&model.ChatSession{}, session.ID).Error
	}()
	t.Logf("fixture user_id=%d session_id=%d task_id=%d llm_model=%s embedding_model=%s chunks=%d", session.UserID, session.ID, session.TaskID, profile.LLMModel, profile.EmbeddingModel, chunkCount)

	factory := ai.NewFactory()
	embeddingClient, err := factory.NewEmbeddingClient(*profile)
	if err != nil {
		t.Fatalf("new embedding client: %v", err)
	}
	chatClient, err := factory.NewChatClient(*profile)
	if err != nil {
		t.Fatalf("new chat client: %v", err)
	}
	retrieved := loadSmokeRetrievedChunks(t, repos, session.UserID, session.TaskID, profile.EmbeddingModel)
	agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{results: retrieved}, ChatConfig{
		TopK:        cfg.RAG.TopK,
		CandidateK:  cfg.RAG.CandidateK,
		MinScore:    cfg.RAG.MinScore,
		RecentTurns: cfg.RAG.RecentTurns,
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := agent.Ask(ctx, VideoAgentRequest{
		UserID:    session.UserID,
		SessionID: session.ID,
		Question:  "总结一下这个视频里最核心的观点，并给出依据。",
		TopK:      3,
	}, embeddingClient, chatClient, *profile)
	if err != nil {
		t.Fatalf("agent ask: %v", err)
	}
	if result.Answer == "" {
		t.Fatal("empty answer")
	}
	if len(result.Citations) == 0 {
		t.Fatal("empty citations")
	}
	if len(result.Trace) == 0 {
		t.Fatal("empty trace")
	}
	t.Logf("template=%s model=%s answer_chars=%d citations=%d trace=%s", result.Template, result.Model, len([]rune(result.Answer)), len(result.Citations), summarizeAgentTrace(result.Trace))
}

func loadSmokeRetrievedChunks(t *testing.T, repos *repository.Repositories, userID, taskID int64, embeddingModel string) []RetrievedChunk {
	t.Helper()
	chunks, err := repos.VideoChunk.ListByTaskID(userID, taskID, embeddingModel)
	if err != nil {
		t.Fatalf("load smoke chunks: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks for smoke fixture")
	}
	limit := 3
	if len(chunks) < limit {
		limit = len(chunks)
	}
	retrieved := make([]RetrievedChunk, 0, limit)
	for i := 0; i < limit; i++ {
		retrieved = append(retrieved, RetrievedChunk{
			ChunkID:      chunks[i].ID,
			ChunkIndex:   chunks[i].ChunkIndex,
			Content:      chunks[i].Content,
			Score:        0.9,
			Source:       RetrievalSourceVector,
			VectorRank:   i + 1,
			RRFScore:     1.0 / float64(i+1),
			MatchedQuery: "smoke",
		})
	}
	return retrieved
}

func findRealLLMAgentFixture(t *testing.T, repos *repository.Repositories, db *gorm.DB, profileSvc *AIProfileService) (*model.ChatSession, *ai.Profile, int64) {
	t.Helper()
	type row struct {
		UserID     int64
		TaskID     int64
		ChunkCount int64
	}
	var candidates []row
	if err := db.Table("video_chunks AS vc").
		Select("vc.user_id, vc.task_id, COUNT(vc.id) AS chunk_count").
		Joins("JOIN user_ai_profiles p ON p.user_id = vc.user_id AND p.is_default = ?", true).
		Group("vc.user_id, vc.task_id").
		Order("MAX(vc.id) DESC").
		Limit(5).
		Scan(&candidates).Error; err != nil {
		t.Fatalf("find fixture candidates: %v", err)
	}
	for _, candidate := range candidates {
		profile, err := profileSvc.GetDefaultAIProfile(candidate.UserID)
		if err != nil {
			t.Logf("skip user_id=%d: %v", candidate.UserID, err)
			continue
		}
		if profile.EmbeddingModel == "" || profile.EmbeddingEndpoint == "" || profile.LLMModel == "" || profile.LLMBaseURL == "" {
			t.Logf("skip user_id=%d: incomplete profile", candidate.UserID)
			continue
		}
		session := &model.ChatSession{
			UserID: candidate.UserID,
			TaskID: candidate.TaskID,
			Title:  "real-llm-smoke-test",
		}
		if err := repos.Chat.CreateSession(session); err != nil {
			t.Fatalf("create smoke test chat session: %v", err)
		}
		return session, profile, candidate.ChunkCount
	}
	t.Fatalf("no usable chat session with default profile and video chunks found")
	return nil, nil, 0
}

func summarizeAgentTrace(trace []VideoAgentStep) string {
	out := ""
	for i, step := range trace {
		if i > 0 {
			out += " -> "
		}
		out += fmt.Sprintf("%s:%s", step.Tool, step.OutputRef)
		if step.Error != "" {
			out += "(error)"
		}
	}
	return out
}
