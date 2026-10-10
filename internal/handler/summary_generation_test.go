package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestSummaryGenerationHTTPReadAuthAndStrictCursors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&[]model.VideoTask{{ID: 1, UserID: 7, Status: model.TaskStatusCompleted}, {ID: 2, UserID: 8, Status: model.TaskStatusCompleted}}).Error; err != nil {
		t.Fatal(err)
	}
	h := NewSummaryGenerationHandler(service.NewSummaryGenerationReadService(repository.NewRepositories(db)))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", int64(7)); c.Next() })
	router.GET("/media/task/:id/summary/generation", h.Latest)
	router.GET("/media/task/:id/summary/generation/:generation_id/events", h.Events)
	for _, test := range []struct {
		url    string
		status int
	}{
		{"/media/task/1/summary/generation", 200},
		{"/media/task/2/summary/generation", 404},
		{"/media/task/invalid/summary/generation", 400},
		{"/media/task/1/summary/generation/unknown/events", 404},
		{"/media/task/1/summary/generation/unknown/events?after_seq=-1", 400},
		{"/media/task/1/summary/generation/unknown/events?after_seq=abc", 400},
		{"/media/task/1/summary/generation/unknown/events?after_seq=1&after_seq=2", 400},
		{"/media/task/1/summary/generation/unknown/events?limit=0", 400},
		{"/media/task/1/summary/generation/unknown/events?limit=101", 400},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", test.url, nil))
		if response.Code != test.status {
			t.Fatalf("%s=%d %s", test.url, response.Code, response.Body)
		}
	}
}
