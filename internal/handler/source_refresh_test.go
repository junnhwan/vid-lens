package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http/httptest"
	"strings"
	"testing"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/jwt"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestSourceRefreshHTTPAuthOwnerRequiredIdentityAndDuplicateKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	db.Create(&model.VideoTask{ID: 2, UserID: 8, Status: model.TaskStatusCompleted})
	h := NewMediaHandler(service.NewMediaService(repository.NewRepositories(db), nil, nil, nil, config.UploadConfig{}, config.ToolsConfig{}, config.JWTConfig{}))
	router := gin.New()
	router.Use(middleware.JWTAuth("source-refresh-secret"))
	router.POST("/media/task/:id/text-source/refresh", h.RequestSourceRefresh)
	token, _ := jwt.GenerateToken(7, "test", "USER", "source-refresh-secret", 1)
	for _, test := range []struct {
		name, body string
		auth       bool
		keys       []string
		status     int
	}{
		{"unauthenticated", `{"expected_source_id":""}`, false, []string{"key"}, 401},
		{"foreign", `{"expected_source_id":""}`, true, []string{"key"}, 404},
		{"required expected identity", `{}`, true, []string{"key"}, 400},
		{"schema", `{"expected_source_id":42}`, true, []string{"key"}, 400},
		{"duplicate header", `{"expected_source_id":""}`, true, []string{"one", "two"}, 400},
		{"missing header", `{"expected_source_id":""}`, true, nil, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/media/task/2/text-source/refresh", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			if test.auth {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			for _, key := range test.keys {
				req.Header.Add("Idempotency-Key", key)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
		})
	}
	var rows int64
	db.Model(&model.ImportRequest{}).Count(&rows)
	if rows != 0 {
		t.Fatal("rejected HTTP requests accepted workflow")
	}
}

func TestSourceRefreshHTTPAcceptedReceiptAndLegacyRefusal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	task := model.VideoTask{UserID: 7, Status: model.TaskStatusCompleted}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	input := service.SourceRefreshRequest{ExpectedSourceID: new(string)}
	hash := processing.Fingerprint(struct {
		TaskID  int64
		Request service.SourceRefreshRequest
	}{task.ID, input})
	_, _, err = repos.AcceptImport(context.Background(), 7, "source_refresh", "original-http-key", hash, func(*repository.Repositories) (*model.VideoTask, error) { return &task, nil })
	if err != nil {
		t.Fatal(err)
	}
	h := NewMediaHandler(service.NewMediaService(repos, nil, nil, nil, config.UploadConfig{}, config.ToolsConfig{}, config.JWTConfig{}))
	router := gin.New()
	router.Use(middleware.JWTAuth("source-refresh-secret"))
	router.POST("/media/task/:id/text-source/refresh", h.RequestSourceRefresh)
	token, _ := jwt.GenerateToken(7, "test", "USER", "source-refresh-secret", 1)
	for _, test := range []struct {
		key    string
		status int
		reason string
	}{{"original-http-key", 202, ""}, {"legacy-new-key", 422, "source_refresh_requires_processing_intent"}} {
		req := httptest.NewRequest("POST", "/media/task/1/text-source/refresh", strings.NewReader(artifact.JSON(input)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", test.key)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != test.status || test.reason != "" && !strings.Contains(rec.Body.String(), test.reason) {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		if test.status == 202 && !strings.Contains(rec.Body.String(), `"operation":"source_refresh"`) {
			t.Fatal("accepted response contract missing operation")
		}
	}
}
