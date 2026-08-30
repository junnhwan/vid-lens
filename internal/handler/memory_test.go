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
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

type memoryHandlerAuthorizer struct{}

func (memoryHandlerAuthorizer) Authorize(_ context.Context, userID int64, scope service.MemoryScope, _ bool) error {
	if scope.Type == model.MemoryScopeUser && scope.ID == "1" && userID == 1 {
		return nil
	}
	return gorm.ErrRecordNotFound
}

func TestMemoryHandlerListsOwnMemoryAndCannotDeleteAnotherUsersMemory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AgentMemoryItem{}, &model.AgentMemoryEvent{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewMemoryRepository(db)
	for _, item := range []*model.AgentMemoryItem{
		{ID: "own", UserID: 1, ScopeType: model.MemoryScopeUser, ScopeID: "1", Kind: "preference", Content: "简洁", SourceType: "user_message", SourceRef: "message:1", Importance: .7},
		{ID: "other", UserID: 2, ScopeType: model.MemoryScopeUser, ScopeID: "2", Kind: "preference", Content: "详细", SourceType: "user_message", SourceRef: "message:2", Importance: .7},
	} {
		if _, err := repo.Append(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewMemoryHandler(service.NewMemoryGovernanceService(repo, memoryHandlerAuthorizer{}))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", int64(1)) })
	router.GET("/memories", handler.List)
	router.DELETE("/memories/:memory_id", handler.Delete)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/memories", nil))
	if list.Code != http.StatusOK || !containsAll(list.Body.String(), `"id":"own"`) || containsAll(list.Body.String(), `"id":"other"`) {
		t.Fatalf("list code=%d body=%s", list.Code, list.Body.String())
	}
	deleteOther := httptest.NewRecorder()
	router.ServeHTTP(deleteOther, httptest.NewRequest(http.MethodDelete, "/memories/other", nil))
	if deleteOther.Code != http.StatusForbidden {
		t.Fatalf("cross-owner delete code=%d body=%s", deleteOther.Code, deleteOther.Body.String())
	}
	other, err := repo.FindForUser(context.Background(), 2, "other")
	if err != nil || other == nil || other.Status == model.MemoryStatusDeleted {
		t.Fatalf("cross-owner memory mutated: %+v err=%v", other, err)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
