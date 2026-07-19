package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestKnowledgeBaseHandlerCreateSuccessAndBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := newKnowledgeBaseHandlerServiceTestEnv(t)
	h := NewKnowledgeBaseHandler(svc)
	r := gin.New()
	r.POST("/knowledge-bases", withTestUser(7), h.Create)

	rec := serveKnowledgeBaseRequest(r, http.MethodPost, "/knowledge-bases", `{"name":"  Go RAG  ","description":"desc"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Go RAG"`) {
		t.Fatalf("create status/body = %d/%s", rec.Code, rec.Body.String())
	}

	badJSON := serveKnowledgeBaseRequest(r, http.MethodPost, "/knowledge-bases", `{"name":`)
	if badJSON.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status = %d, want 400", badJSON.Code)
	}
	blank := serveKnowledgeBaseRequest(r, http.MethodPost, "/knowledge-bases", `{"name":"   "}`)
	if blank.Code != http.StatusBadRequest || !strings.Contains(blank.Body.String(), "不能为空") {
		t.Fatalf("blank status/body = %d/%s", blank.Code, blank.Body.String())
	}
}

func TestKnowledgeBaseHandlerCrossUserNotFoundDoesNotLeakData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := newKnowledgeBaseHandlerServiceTestEnv(t)
	created, err := svc.Create(context.Background(), 7, service.CreateKnowledgeBaseRequest{Name: "private-name", Description: "private description"})
	if err != nil {
		t.Fatal(err)
	}
	h := NewKnowledgeBaseHandler(svc)
	r := gin.New()
	r.GET("/knowledge-bases/:id", withTestUser(8), h.Get)
	rec := serveKnowledgeBaseRequest(r, http.MethodGet, fmt.Sprintf("/knowledge-bases/%d", created.ID), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user status = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "private-name") || strings.Contains(body, "private description") || strings.Contains(body, `"user_id"`) {
		t.Fatalf("cross-user response leaked data: %s", body)
	}
}

func TestKnowledgeBaseHandlerVideoLifecycleAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, repos, _ := newKnowledgeBaseHandlerServiceTestEnv(t)
	createKnowledgeBaseHandlerProfile(t, repos, 7, "embed-a")
	kb, err := svc.Create(context.Background(), 7, service.CreateKnowledgeBaseRequest{Name: "kb"})
	if err != nil {
		t.Fatal(err)
	}
	task := createKnowledgeBaseHandlerTask(t, repos, 7, "member")
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: task.ID, EmbeddingModel: "embed-a", EmbeddingDim: 3, Status: model.RAGIndexStatusIndexed}); err != nil {
		t.Fatal(err)
	}

	h := NewKnowledgeBaseHandler(svc)
	r := gin.New()
	r.POST("/knowledge-bases/:id/videos", withTestUser(7), h.AddVideo)
	r.DELETE("/knowledge-bases/:id/videos/:task_id", withTestUser(7), h.RemoveVideo)

	add := serveKnowledgeBaseRequest(r, http.MethodPost, fmt.Sprintf("/knowledge-bases/%d/videos", kb.ID), fmt.Sprintf(`{"task_id":%d}`, task.ID))
	if add.Code != http.StatusOK {
		t.Fatalf("add status/body = %d/%s", add.Code, add.Body.String())
	}
	duplicate := serveKnowledgeBaseRequest(r, http.MethodPost, fmt.Sprintf("/knowledge-bases/%d/videos", kb.ID), fmt.Sprintf(`{"task_id":%d}`, task.ID))
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate status/body = %d/%s", duplicate.Code, duplicate.Body.String())
	}
	badTask := serveKnowledgeBaseRequest(r, http.MethodPost, fmt.Sprintf("/knowledge-bases/%d/videos", kb.ID), `{"task_id":0}`)
	if badTask.Code != http.StatusBadRequest {
		t.Fatalf("bad task status = %d, want 400", badTask.Code)
	}
	remove := serveKnowledgeBaseRequest(r, http.MethodDelete, fmt.Sprintf("/knowledge-bases/%d/videos/%d", kb.ID, task.ID), "")
	if remove.Code != http.StatusOK {
		t.Fatalf("remove status/body = %d/%s", remove.Code, remove.Body.String())
	}
}

func TestKnowledgeBaseHandlerUpdateDeleteAndRoutesUseExpectedMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _ := newKnowledgeBaseHandlerServiceTestEnv(t)
	created, err := svc.Create(context.Background(), 7, service.CreateKnowledgeBaseRequest{Name: "before"})
	if err != nil {
		t.Fatal(err)
	}
	h := NewKnowledgeBaseHandler(svc)
	r := gin.New()
	r.PATCH("/knowledge-bases/:id", withTestUser(7), h.Update)
	r.DELETE("/knowledge-bases/:id", withTestUser(7), h.Delete)

	patch := serveKnowledgeBaseRequest(r, http.MethodPatch, fmt.Sprintf("/knowledge-bases/%d", created.ID), `{"name":"after"}`)
	if patch.Code != http.StatusOK || !strings.Contains(patch.Body.String(), `"name":"after"`) {
		t.Fatalf("patch status/body = %d/%s", patch.Code, patch.Body.String())
	}
	deleteRec := serveKnowledgeBaseRequest(r, http.MethodDelete, fmt.Sprintf("/knowledge-bases/%d", created.ID), "")
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status/body = %d/%s", deleteRec.Code, deleteRec.Body.String())
	}
}

func withTestUser(userID int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	}
}

func serveKnowledgeBaseRequest(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var requestBody *bytes.Reader
	if body == "" {
		requestBody = bytes.NewReader(nil)
	} else {
		requestBody = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, requestBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func newKnowledgeBaseHandlerServiceTestEnv(t *testing.T) (*service.KnowledgeBaseService, *repository.Repositories, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	repos := repository.NewRepositories(db)
	return service.NewKnowledgeBaseService(repos), repos, db
}

func createKnowledgeBaseHandlerProfile(t *testing.T, repos *repository.Repositories, userID int64, embeddingModel string) {
	t.Helper()
	if err := repos.AIProfile.Create(&model.UserAIProfile{UserID: userID, Name: "default", LLMProvider: "openai", LLMBaseURL: "https://llm.example", LLMModel: "chat", ASRProvider: "asr", ASRBaseURL: "https://asr.example", ASRModel: "asr", EmbeddingProvider: "openai", EmbeddingEndpoint: "https://embed.example", EmbeddingModel: embeddingModel, EmbeddingDim: 3, IsDefault: true}); err != nil {
		t.Fatalf("create profile: %v", err)
	}
}

func createKnowledgeBaseHandlerTask(t *testing.T, repos *repository.Repositories, userID int64, name string) *model.VideoTask {
	t.Helper()
	task := &model.VideoTask{UserID: userID, FileMD5: fmt.Sprintf("%032s", name), Filename: name + ".mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func decodeKnowledgeBaseResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return body
}
