package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"vid-lens/internal/config"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/jwt"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestSummaryVisualRetryHTTPAuthenticationOwnershipAndSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.VideoTask{ID: 2, UserID: 8, Status: model.TaskStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	svc := service.NewMediaService(repos, nil, nil, nil, config.UploadConfig{}, config.ToolsConfig{}, config.JWTConfig{})
	h := NewMediaHandler(svc)
	router := gin.New()
	router.Use(middleware.JWTAuth("retry-test-secret"))
	router.POST("/media/task/:id/summary/visual-retry", h.RequestSummaryVisualRetry)
	token, err := jwt.GenerateToken(7, "test", "USER", "retry-test-secret", 1)
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"expected_generation_id":"parent-generation","expected_generated_version":1,"expected_content_digest":"` + strings.Repeat("a", 64) + `","expected_source_id":"source-id","expected_source_digest":"` + strings.Repeat("b", 64) + `","authorize_new_visual_budget":true}`
	for _, test := range []struct {
		name, body string
		auth       bool
		keys       []string
		status     int
	}{
		{"missing authentication", valid, false, []string{"retry-key"}, 401},
		{"foreign owner", valid, true, []string{"retry-key"}, 404},
		{"duplicate key headers", valid, true, []string{"one", "two"}, 400},
		{"missing key", valid, true, nil, 400},
		{"budget not authorized", strings.Replace(valid, `:true}`, `:false}`, 1), true, []string{"retry-key"}, 400},
		{"wrong JSON field type", `{"expected_generated_version":"1"}`, true, []string{"retry-key"}, 400},
		{"incomplete schema", `{}`, true, []string{"retry-key"}, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/media/task/2/summary/visual-retry", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			if test.auth {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			for _, key := range test.keys {
				req.Header.Add("Idempotency-Key", key)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != test.status {
				t.Fatalf("HTTP %d: %s", response.Code, response.Body)
			}
		})
	}
	var receipts, jobs int64
	db.Model(&model.ImportRequest{}).Count(&receipts)
	db.Model(&model.TaskJob{}).Count(&jobs)
	if receipts != 0 || jobs != 0 {
		t.Fatal("rejected HTTP requests created work")
	}
}
