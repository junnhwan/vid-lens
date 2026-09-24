package service

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/jwt"
	"vid-lens/internal/storage"
)

const mediaTestSecret = "media-test-secret"

// stubObjectStore serves one in-memory object so playback can be exercised
// without MinIO. The recorded OpenObject count proves which guard fired before
// storage was touched.
type stubObjectStore struct {
	body        []byte
	contentType string
	opened      int
	openedKey   string
}

func (s *stubObjectStore) DeleteObject(context.Context, string) error { return nil }

func (s *stubObjectStore) UploadFile(context.Context, string, io.Reader, int64, string) error {
	return nil
}

func (s *stubObjectStore) UploadFromPath(context.Context, string, string, string) (int64, error) {
	return 0, nil
}

func (s *stubObjectStore) BucketName() string { return "vidlens" }

func (s *stubObjectStore) ComposeObject(context.Context, string, []minio.CopySrcOptions) (int64, error) {
	return 0, nil
}

func (s *stubObjectStore) GetPresignedURL(context.Context, string) (string, error) {
	return "http://127.0.0.1:19000/vidlens/videos/stub.mp4?sig=stub", nil
}

func (s *stubObjectStore) ObjectContentType(context.Context, string) string { return s.contentType }

func (s *stubObjectStore) OpenObject(_ context.Context, key string) (storage.Object, error) {
	s.opened++
	s.openedKey = key
	return &stubObject{
		Reader:     bytes.NewReader(s.body),
		objectInfo: minio.ObjectInfo{Size: int64(len(s.body))},
	}, nil
}

func TestOpenTaskVisualFrameUsesSavedFrameAndTaskCredential(t *testing.T) {
	svc, store, task := newPlaybackTestService(t)
	frame := model.VideoVisualFrame{TaskID: task.ID, FrameIndex: 0, TimeMs: 90_000, ObjectKey: "visual-frames/task/frame-90000ms.jpg", Status: model.VisualFrameStatusCompleted}
	if err := svc.repo.VisualFrame.ReplaceTaskFrames(task.ID, []model.VideoVisualFrame{frame}); err != nil {
		t.Fatalf("save visual frame: %v", err)
	}
	stored, err := svc.repo.VisualFrame.ListByTaskID(task.ID)
	if err != nil || len(stored) != 1 {
		t.Fatalf("read visual frame: %v, %#v", err, stored)
	}
	path, err := svc.GetPlaybackURL(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := path[strings.Index(path, "token=")+len("token="):]
	object, err := svc.OpenTaskVisualFrame(context.Background(), task.ID, stored[0].ID, token)
	if err != nil {
		t.Fatalf("OpenTaskVisualFrame: %v", err)
	}
	object.Close()
	if store.openedKey != frame.ObjectKey {
		t.Fatalf("opened %q, want saved frame %q", store.openedKey, frame.ObjectKey)
	}
	store.opened = 0
	otherToken, _ := jwt.GenerateMediaToken(99, task.ID, mediaTestSecret, time.Hour)
	if _, err := svc.OpenTaskVisualFrame(context.Background(), task.ID, stored[0].ID, otherToken); err == nil || store.opened != 0 {
		t.Fatalf("another owner's token should be rejected before storage: err=%v opens=%d", err, store.opened)
	}
}

type stubObject struct {
	*bytes.Reader
	objectInfo minio.ObjectInfo
}

func (s *stubObject) Close() error                    { return nil }
func (s *stubObject) Stat() (minio.ObjectInfo, error) { return s.objectInfo, nil }

func newPlaybackTestService(t *testing.T) (*MediaService, *stubObjectStore, *model.VideoTask) {
	t.Helper()
	repos := newMediaTestRepositories(t)

	task := &model.VideoTask{
		UserID:  12,
		Title:   "playback",
		FileURL: "videos/e6b51483.mp4",
		Status:  model.TaskStatusCompleted,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	store := &stubObjectStore{body: []byte("vidlens-media-body"), contentType: "video/mp4"}
	svc := &MediaService{
		repo:           repos,
		storage:        store,
		playbackSecret: mediaTestSecret,
	}
	return svc, store, task
}

func TestGetPlaybackURLReturnsSameOriginStreamPath(t *testing.T) {
	svc, _, task := newPlaybackTestService(t)

	got, err := svc.GetPlaybackURL(context.Background(), 12, task.ID)
	if err != nil {
		t.Fatalf("GetPlaybackURL: %v", err)
	}
	if !strings.HasPrefix(got, playbackPathPrefix) {
		t.Fatalf("playback URL %q must be a same-origin path, not a storage URL", got)
	}

	token := got[strings.Index(got, "token=")+len("token="):]
	claims, err := jwt.ParseMediaToken(token, mediaTestSecret)
	if err != nil {
		t.Fatalf("embedded credential is not a valid media token: %v", err)
	}
	if claims.UserID != 12 || claims.TaskID != task.ID {
		t.Fatalf("credential scope = user %d task %d, want user 12 task %d", claims.UserID, claims.TaskID, task.ID)
	}
}

func TestGetPlaybackURLRejectsOtherOwner(t *testing.T) {
	svc, _, task := newPlaybackTestService(t)

	if _, err := svc.GetPlaybackURL(context.Background(), 99, task.ID); err == nil {
		t.Fatal("expected another user to be rejected")
	}
}

// The credential is bound to one task, so a shared playback URL cannot be
// replayed against a different video.
func TestOpenTaskMediaRejectsTokenForAnotherTask(t *testing.T) {
	svc, store, task := newPlaybackTestService(t)

	token, err := jwt.GenerateMediaToken(12, task.ID+1, mediaTestSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateMediaToken: %v", err)
	}
	if _, _, _, err := svc.OpenTaskMedia(context.Background(), task.ID, token); err == nil {
		t.Fatal("expected a token scoped to another task to be rejected")
	}
	if store.opened != 0 {
		t.Fatalf("storage opened %d times for a rejected credential, want 0", store.opened)
	}
}

func TestOpenTaskMediaRejectsSessionToken(t *testing.T) {
	svc, _, task := newPlaybackTestService(t)

	session, err := jwt.GenerateToken(12, "test", model.RoleDemo, mediaTestSecret, 72)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if _, _, _, err := svc.OpenTaskMedia(context.Background(), task.ID, session); err == nil {
		t.Fatal("expected a session token to be rejected as a playback credential")
	}
}

// Ownership is re-read from the stored task, so a credential minted for one
// user cannot read a task that belongs to another.
func TestOpenTaskMediaRejectsTokenForAnotherUser(t *testing.T) {
	svc, store, task := newPlaybackTestService(t)

	token, err := jwt.GenerateMediaToken(99, task.ID, mediaTestSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateMediaToken: %v", err)
	}
	if _, _, _, err := svc.OpenTaskMedia(context.Background(), task.ID, token); err == nil {
		t.Fatal("expected a token for another user to be rejected")
	}
	if store.opened != 0 {
		t.Fatalf("storage opened %d times for a rejected credential, want 0", store.opened)
	}
}

// A valid credential must resolve to a seekable object, which is what makes
// Range-based seeking possible in the browser.
func TestOpenTaskMediaReturnsSeekableObject(t *testing.T) {
	svc, store, task := newPlaybackTestService(t)

	path, err := svc.GetPlaybackURL(context.Background(), 12, task.ID)
	if err != nil {
		t.Fatalf("GetPlaybackURL: %v", err)
	}
	token := path[strings.Index(path, "token=")+len("token="):]

	_, object, contentType, err := svc.OpenTaskMedia(context.Background(), task.ID, token)
	if err != nil {
		t.Fatalf("OpenTaskMedia: %v", err)
	}
	defer object.Close()

	if contentType != "video/mp4" {
		t.Fatalf("content type = %q, want video/mp4", contentType)
	}
	// Seek past the start, then read: proves the reader is seekable, not a
	// forward-only stream.
	if _, err := object.Seek(8, io.SeekStart); err != nil {
		t.Fatalf("object is not seekable: %v", err)
	}
	tail, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read after seek: %v", err)
	}
	if string(tail) != string(store.body[8:]) {
		t.Fatalf("read after seek = %q, want %q", tail, store.body[8:])
	}
}
