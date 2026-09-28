package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestSummaryHandlerOwnerEffectiveExportAndDeletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	md5 := "66666666666666666666666666666666"
	for _, task := range []model.VideoTask{{ID: 41, UserID: 7, FileMD5: md5, Filename: "a.mp4"}, {ID: 42, UserID: 8, FileMD5: md5, Filename: "b.mp4"}} {
		if err = db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	base := "共享的生成原稿"
	if err = db.Create(&model.AISummary{TaskID: 41, FileMD5: md5, Content: base}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.SummaryRevision{ID: "revision-a", UserID: 7, TaskID: 41, Version: 1, Content: "仅 A 的修订", BaseGeneratedHash: artifact.Hash(base), Origin: "agent", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.SummaryRevisionHead{UserID: 7, TaskID: 41, Version: 1, CurrentRevisionID: "revision-a"}).Error; err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	h := NewSummaryRevisionHandler(service.NewSummaryRevisionService(repos, nil, nil), repos)
	request := func(owner int64, path string, export bool) *httptest.ResponseRecorder {
		r := gin.New()
		r.Use(withTestUser(owner))
		if export {
			r.GET("/media/task/:id/summary/export", h.Export)
		} else {
			r.GET("/media/task/:id/summary", h.Get)
		}
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		return response
	}
	for _, tc := range []struct {
		owner  int64
		task   int64
		status int
		want   string
	}{{7, 41, 200, "仅 A 的修订"}, {8, 42, 200, base}, {8, 41, 404, ""}} {
		response := request(tc.owner, "/media/task/"+strconv.FormatInt(tc.task, 10)+"/summary", false)
		if response.Code != tc.status {
			t.Fatalf("owner %d task %d status=%d body=%s", tc.owner, tc.task, response.Code, response.Body.String())
		}
		if tc.status == 200 {
			var envelope struct {
				Data struct {
					Content string `json:"content"`
				} `json:"data"`
			}
			if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.Content != tc.want {
				t.Fatalf("owner %d content=%q, %v", tc.owner, envelope.Data.Content, err)
			}
		}
	}
	exported := request(7, "/media/task/41/summary/export", true)
	if exported.Code != 200 || exported.Body.String() != "仅 A 的修订" {
		t.Fatalf("export=%d %q", exported.Code, exported.Body.String())
	}
	if err = db.Delete(&model.VideoTask{}, 41).Error; err != nil {
		t.Fatal(err)
	}
	if response := request(7, "/media/task/41/summary/export", true); response.Code != 404 {
		t.Fatalf("deleted source export status=%d", response.Code)
	}
}
