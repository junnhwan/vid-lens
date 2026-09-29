package handler

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/jwt"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/storage"
)

const streamTestSecret = "stream-test-secret"

// fakeObjectStore serves one in-memory object, which is enough to assert the
// HTTP contract the browser player depends on: a video content type, a 200 for
// the whole file, and a 206 honoring Range for seeking.
type fakeObjectStore struct{ body []byte }

func (s *fakeObjectStore) DeleteObject(context.Context, string) error { return nil }

func (s *fakeObjectStore) UploadFile(context.Context, string, io.Reader, int64, string) error {
	return nil
}

func (s *fakeObjectStore) UploadFromPath(context.Context, string, string, string) (int64, error) {
	return 0, nil
}

func (s *fakeObjectStore) BucketName() string { return "vidlens" }

func (s *fakeObjectStore) ComposeObject(context.Context, string, []minio.CopySrcOptions) (int64, error) {
	return 0, nil
}

func (s *fakeObjectStore) GetPresignedURL(context.Context, string) (string, error) { return "", nil }

func (s *fakeObjectStore) ObjectContentType(context.Context, string) string { return "video/mp4" }

func (s *fakeObjectStore) OpenObject(context.Context, string) (storage.Object, error) {
	return &fakeObject{
		Reader:     bytes.NewReader(s.body),
		objectInfo: minio.ObjectInfo{Size: int64(len(s.body))},
	}, nil
}

type fakeObject struct {
	*bytes.Reader
	objectInfo minio.ObjectInfo
}

func (s *fakeObject) Close() error                    { return nil }
func (s *fakeObject) Stat() (minio.ObjectInfo, error) { return s.objectInfo, nil }

func newStreamTestRouter(t *testing.T) (*gin.Engine, *model.VideoTask, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	repos := repository.NewRepositories(db)

	task := &model.VideoTask{
		UserID:   12,
		Title:    "stream",
		Filename: "中文视频.mp4",
		FileURL:  "videos/stream-fixture.mp4",
		Status:   model.TaskStatusCompleted,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	svc := service.NewMediaService(
		repos,
		&fakeObjectStore{body: []byte("0011223344556677")},
		nil, nil,
		config.UploadConfig{}, config.ToolsConfig{},
		config.JWTConfig{Secret: streamTestSecret},
	)
	media := NewMediaHandler(svc)

	router := gin.New()
	router.GET("/api/v1/media/task/:id/stream", media.StreamTaskMedia)
	router.GET("/api/v1/media/task/:id/download", media.DownloadTaskMedia)
	router.HEAD("/api/v1/media/task/:id/download", media.DownloadTaskMedia)

	token, err := jwt.GenerateMediaToken(12, task.ID, streamTestSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateMediaToken: %v", err)
	}
	return router, task, token
}

func TestStreamTaskMediaServesFullBody(t *testing.T) {
	router, task, token := newStreamTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/task/"+strconv.FormatInt(task.ID, 10)+"/stream?token="+token, nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("content type = %q, want video/mp4", got)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes so the player can seek", got)
	}
	if rec.Body.String() != "0011223344556677" {
		t.Fatalf("body = %q, want the stored bytes", rec.Body.String())
	}
}

// Seeking in the browser is a Range request; without 206 the player would
// re-download the file from the start on every jump.
func TestStreamTaskMediaHonorsRange(t *testing.T) {
	router, task, token := newStreamTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/task/"+strconv.FormatInt(task.ID, 10)+"/stream?token="+token, nil)
	req.Header.Set("Range", "bytes=8-11")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 8-11/16" {
		t.Fatalf("Content-Range = %q, want bytes 8-11/16", got)
	}
	if rec.Body.String() != "4455" {
		t.Fatalf("body = %q, want the requested slice", rec.Body.String())
	}
}

func TestStreamTaskMediaRejectsInvalidCredential(t *testing.T) {
	router, task, _ := newStreamTestRouter(t)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(
		http.MethodGet, "/api/v1/media/task/"+strconv.FormatInt(task.ID, 10)+"/stream?token=not-a-token", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDownloadTaskMediaAttachmentRangeAndHEAD(t *testing.T) {
	router, task, token := newStreamTestRouter(t)
	url := "/api/v1/media/task/" + strconv.FormatInt(task.ID, 10) + "/download?token=" + token
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, url, nil)
		req.Header.Set("Authorization", "Bearer stale-session-token")
		if method == http.MethodGet {
			req.Header.Set("Range", "bytes=8-11")
		}
		router.ServeHTTP(rec, req)
		kind, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
		if err != nil || kind != "attachment" || params["filename"] != task.Filename {
			t.Fatalf("download filename: %s (%v)", rec.Header().Get("Content-Disposition"), err)
		}
		if method == http.MethodGet && (rec.Code != 206 || rec.Body.String() != "4455" || rec.Header().Get("Content-Range") != "bytes 8-11/16") {
			t.Fatalf("download range: %d %s", rec.Code, rec.Body.String())
		}
		if method == http.MethodHead && (rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "16") {
			t.Fatalf("download HEAD: %d, %v", rec.Code, rec.Header())
		}
	}
	for _, credential := range []string{"", "not-a-token", tokenForAnotherTask(t, task.ID)} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, strings.Split(url, "?")[0]+"?token="+credential, nil))
		if rec.Code != 404 {
			t.Fatalf("invalid download credential: status %d", rec.Code)
		}
	}
}

func tokenForAnotherTask(t *testing.T, taskID int64) string {
	t.Helper()
	token, err := jwt.GenerateMediaToken(12, taskID+1, streamTestSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
