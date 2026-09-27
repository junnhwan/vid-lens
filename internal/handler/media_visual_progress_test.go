package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestGetVisualProgressHTTPContractAndOwnerScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	task := &model.VideoTask{UserID: 7, FileMD5: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Filename: "lesson.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	media := NewMediaHandler(service.NewMediaService(repos, nil, nil, nil, config.UploadConfig{}, config.ToolsConfig{}, config.JWTConfig{}))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-Owner") == "yes" {
			c.Set("userID", int64(7))
		} else {
			c.Set("userID", int64(8))
		}
	})
	router.GET("/api/v1/media/task/:id/visual-progress", media.GetVisualProgress)
	path := "/api/v1/media/task/" + strconv.FormatInt(task.ID, 10) + "/visual-progress"
	request := func(owner bool) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if owner {
			req.Header.Set("X-Test-Owner", "yes")
		}
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(false); rec.Code != http.StatusNotFound {
		t.Fatalf("other owner status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec := request(true)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Status      string `json:"status"`
			TotalFrames *int   `json:"total_frames"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != 200 || body.Data.Status != "not_started" || body.Data.TotalFrames != nil {
		t.Fatalf("unexpected API payload: %+v", body)
	}
}
