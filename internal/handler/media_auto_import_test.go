package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestAutoImportHTTPRequiresKeyAndPreservesDefaultTagsOnReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	url := "https://www.bilibili.com/video/BV1xx411c7mD"
	opts, _ := processing.Normalize(processing.Options{AutoSummary: true, AutoTagsEnabled: true}, false)
	hash := processing.Fingerprint(struct {
		Input   any
		Options processing.Options
	}{url, opts})
	task, _, err := repos.AcceptImport(context.Background(), 7, "upload_url", "http-replay", hash, func(tx *repository.Repositories) (*model.VideoTask, error) {
		task := &model.VideoTask{UserID: 7, FileMD5: "http-fixture", Filename: "video.mp4", Status: model.TaskStatusQueued}
		return task, tx.Task.Create(task)
	})
	if err != nil {
		t.Fatal(err)
	}
	h := NewMediaHandler(service.NewMediaService(repos, nil, nil, nil, config.UploadConfig{}, config.ToolsConfig{}, config.JWTConfig{}))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", int64(7)); c.Next() })
	r.POST("/upload-url", h.UploadByURL)
	for _, test := range []struct {
		name, key, body string
		status          int
	}{
		{"missing-key", "", `{"url":"` + url + `","auto_summary":true}`, 400},
		{"default-tags", "http-replay", `{"url":"` + url + `","auto_summary":true}`, 200},
		{"explicit-disable", "http-replay", `{"url":"` + url + `","auto_summary":true,"auto_tags_enabled":false}`, 409},
		{"invalid-mode", "bad-mode", `{"url":"` + url + `","auto_summary":true,"output_mode":"image_text"}`, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/upload-url", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", test.key)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != test.status {
				t.Fatalf("response=%d %s, task=%d", w.Code, w.Body, task.ID)
			}
		})
	}
}
