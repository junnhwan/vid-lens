package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

type handlerRecordingAIProfileTester struct {
	profile *service.DecryptedAIProfile
	calls   int
}

func (t *handlerRecordingAIProfileTester) TestProfile(ctx context.Context, profile *service.DecryptedAIProfile) error {
	t.calls++
	copied := *profile
	t.profile = &copied
	return nil
}

func TestAIProfileHandlerTestAcceptsSavedProfileID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tester := &handlerRecordingAIProfileTester{}
	svc := newAIProfileHandlerServiceForTest(t, tester)
	created, err := svc.Create(7, validHandlerAIProfileRequest())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	router := gin.New()
	handler := NewAIProfileHandler(svc)
	router.POST("/ai/profiles/test", func(c *gin.Context) {
		c.Set("userID", int64(7))
		handler.Test(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ai/profiles/test", bytes.NewBufferString(`{"id":`+strconv.FormatInt(created.ID, 10)+`}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if tester.calls != 1 {
		t.Fatalf("tester calls = %d, want 1", tester.calls)
	}
	if tester.profile == nil || tester.profile.ID != created.ID || tester.profile.UserID != 7 {
		t.Fatalf("tester profile = %+v, want saved profile", tester.profile)
	}
}

func newAIProfileHandlerServiceForTest(t *testing.T, tester service.AIProfileTester) *service.AIProfileService {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.UserAIProfile{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	repos := repository.NewRepositories(db)
	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	return service.NewAIProfileService(repos.AIProfile, codec, tester)
}

func validHandlerAIProfileRequest() service.AIProfileRequest {
	return service.AIProfileRequest{
		Name:              "default",
		LLMProvider:       "openai_compatible",
		LLMBaseURL:        "https://llm.example.com/v1",
		LLMAPIKey:         "sk-llm-secret",
		LLMModel:          "chat-model",
		ASRProvider:       "mimo",
		ASRBaseURL:        "https://token-plan-cn.xiaomimimo.com/v1",
		ASRAPIKey:         "tp-asr-secret",
		ASRModel:          "mimo-v2.5-asr",
		EmbeddingProvider: "openai_compatible",
		EmbeddingEndpoint: "https://router.tumuer.me/v1/embeddings",
		EmbeddingAPIKey:   "sk-embedding-secret",
		EmbeddingModel:    "text-embedding-3-small",
		EmbeddingDim:      1536,
		IsDefault:         true,
	}
}
