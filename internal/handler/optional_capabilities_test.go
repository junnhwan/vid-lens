package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

type preferenceProfileFixture struct{}

func (preferenceProfileFixture) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	return &ai.Profile{}, nil
}

func TestOptionalCapabilitiesRequiresBooleanAndOnlyChangesAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(db)
	for _, u := range []model.User{{ID: 1, Username: "own"}, {ID: 2, Username: "other"}} {
		if err := repo.Create(&u); err != nil {
			t.Fatal(err)
		}
	}
	svc := service.NewUserService(repo, config.JWTConfig{}).WithOptionalCapabilities(preferenceProfileFixture{}, true, service.DefaultRAGRetrievalConfig(), false)
	h := NewUserHandler(svc)
	role := model.RoleUser
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", int64(1)); c.Set("role", role) })
	r.PATCH("/optional", h.SetOptionalCapabilities)
	request := func(body string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/optional", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}
	for _, body := range []string{`{}`, `{"rerank_enabled":null}`, `{"rerank_enabled":"true"}`} {
		if status := request(body); status != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, status)
		}
	}
	if status := request(`{"rerank_enabled":true,"user_id":2}`); status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	own, _ := repo.RerankEnabled(context.Background(), 1)
	other, _ := repo.RerankEnabled(context.Background(), 2)
	if !own || other {
		t.Fatalf("own=%v other=%v", own, other)
	}
	if status := request(`{"rerank_enabled":false}`); status != http.StatusOK {
		t.Fatalf("explicit false status=%d", status)
	}
	role = model.RoleDemo
	if status := request(`{"rerank_enabled":true}`); status != http.StatusForbidden {
		t.Fatalf("demo status=%d", status)
	}
	own, _ = repo.RerankEnabled(context.Background(), 1)
	if own {
		t.Fatal("demo write changed preference")
	}
}
