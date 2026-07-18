package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"vid-lens/internal/service"

	"github.com/gin-gonic/gin"
)

type uploadSessionHandlerStub struct {
	createFn      func(context.Context, int64, service.CreateUploadSessionRequest) (*service.UploadSessionView, error)
	getFn         func(context.Context, int64, string) (*service.UploadSessionView, error)
	acceptChunkFn func(context.Context, int64, string, int, io.Reader) (*service.UploadChunkResult, error)
	completeFn    func(context.Context, int64, string) (*service.UploadResult, error)
}

func (s *uploadSessionHandlerStub) Create(ctx context.Context, userID int64, req service.CreateUploadSessionRequest) (*service.UploadSessionView, error) {
	return s.createFn(ctx, userID, req)
}

func (s *uploadSessionHandlerStub) Get(ctx context.Context, userID int64, sessionID string) (*service.UploadSessionView, error) {
	return s.getFn(ctx, userID, sessionID)
}

func (s *uploadSessionHandlerStub) AcceptChunk(ctx context.Context, userID int64, sessionID string, index int, reader io.Reader) (*service.UploadChunkResult, error) {
	return s.acceptChunkFn(ctx, userID, sessionID, index, reader)
}

func (s *uploadSessionHandlerStub) Complete(ctx context.Context, userID int64, sessionID string) (*service.UploadResult, error) {
	return s.completeFn(ctx, userID, sessionID)
}

func uploadSessionHandlerTestRouter(svc uploadSessionApplication) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewUploadSessionHandler(svc)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", int64(7))
		c.Next()
	})
	router.POST("/upload-sessions", h.Create)
	router.GET("/upload-sessions/:session_id", h.Get)
	router.PUT("/upload-sessions/:session_id/chunks/:index", h.UploadChunk)
	router.POST("/upload-sessions/:session_id/complete", h.Complete)
	return router
}

func TestUploadSessionHandlerCreateAndGet(t *testing.T) {
	expiresAt := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	stub := &uploadSessionHandlerStub{}
	stub.createFn = func(_ context.Context, userID int64, req service.CreateUploadSessionRequest) (*service.UploadSessionView, error) {
		if userID != 7 || req.Filename != "demo.mp4" || req.FileSize != 11 || req.ChunkSize != 5 || req.TotalChunks != 3 {
			t.Fatalf("Create() input user=%d req=%+v", userID, req)
		}
		return &service.UploadSessionView{SessionID: "session-1", Status: "active", ExpiresAt: expiresAt}, nil
	}
	stub.getFn = func(_ context.Context, userID int64, sessionID string) (*service.UploadSessionView, error) {
		if userID != 7 || sessionID != "session-1" {
			t.Fatalf("Get() input user=%d session=%q", userID, sessionID)
		}
		return &service.UploadSessionView{SessionID: sessionID, Uploaded: []int{0, 2}, Status: "active", ExpiresAt: expiresAt}, nil
	}
	stub.acceptChunkFn = func(context.Context, int64, string, int, io.Reader) (*service.UploadChunkResult, error) {
		t.Fatal("unexpected AcceptChunk()")
		return nil, nil
	}
	stub.completeFn = func(context.Context, int64, string) (*service.UploadResult, error) {
		t.Fatal("unexpected Complete()")
		return nil, nil
	}
	router := uploadSessionHandlerTestRouter(stub)

	body := `{"filename":"demo.mp4","file_size":11,"chunk_size":5,"total_chunks":3,"expected_md5":"92b9cccc0b98c3a0b8d0df25a421c0e3"}`
	createReq := httptest.NewRequest(http.MethodPost, "/upload-sessions", bytes.NewBufferString(body))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}

	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/upload-sessions/session-1", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var payload struct {
		Data service.UploadSessionView `json:"data"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Uploaded) != 2 || payload.Data.Uploaded[0] != 0 || payload.Data.Uploaded[1] != 2 {
		t.Fatalf("get payload=%+v", payload.Data)
	}
}

func TestUploadSessionHandlerAcceptsRawChunkAndEnforcesManifestSize(t *testing.T) {
	acceptedCalls := 0
	stub := &uploadSessionHandlerStub{}
	stub.createFn = func(context.Context, int64, service.CreateUploadSessionRequest) (*service.UploadSessionView, error) {
		return nil, nil
	}
	stub.getFn = func(_ context.Context, userID int64, sessionID string) (*service.UploadSessionView, error) {
		return &service.UploadSessionView{SessionID: sessionID, FileSize: 11, ChunkSize: 5, TotalChunks: 3, Status: "active"}, nil
	}
	stub.acceptChunkFn = func(_ context.Context, userID int64, sessionID string, index int, reader io.Reader) (*service.UploadChunkResult, error) {
		acceptedCalls++
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if userID != 7 || sessionID != "session-1" || index != 0 || string(data) != "abcde" {
			t.Fatalf("AcceptChunk() user=%d session=%q index=%d data=%q", userID, sessionID, index, data)
		}
		return &service.UploadChunkResult{ChunkIndex: index, ActualSize: int64(len(data))}, nil
	}
	stub.completeFn = func(context.Context, int64, string) (*service.UploadResult, error) { return nil, nil }
	router := uploadSessionHandlerTestRouter(stub)

	req := httptest.NewRequest(http.MethodPut, "/upload-sessions/session-1/chunks/0", bytes.NewBufferString("abcde"))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || acceptedCalls != 1 {
		t.Fatalf("raw chunk status=%d calls=%d body=%s", rec.Code, acceptedCalls, rec.Body.String())
	}

	oversized := httptest.NewRequest(http.MethodPut, "/upload-sessions/session-1/chunks/0", bytes.NewBufferString("abcdef"))
	oversized.Header.Set("Content-Type", "application/octet-stream")
	overRec := httptest.NewRecorder()
	router.ServeHTTP(overRec, oversized)
	if overRec.Code != http.StatusBadRequest {
		t.Fatalf("oversized status=%d body=%s", overRec.Code, overRec.Body.String())
	}
	if acceptedCalls != 1 {
		t.Fatalf("oversized body reached service; calls=%d", acceptedCalls)
	}
}

func TestUploadSessionHandlerCompletesOwnedSession(t *testing.T) {
	stub := &uploadSessionHandlerStub{}
	stub.createFn = func(context.Context, int64, service.CreateUploadSessionRequest) (*service.UploadSessionView, error) {
		return nil, nil
	}
	stub.getFn = func(context.Context, int64, string) (*service.UploadSessionView, error) { return nil, nil }
	stub.acceptChunkFn = func(context.Context, int64, string, int, io.Reader) (*service.UploadChunkResult, error) {
		return nil, nil
	}
	stub.completeFn = func(_ context.Context, userID int64, sessionID string) (*service.UploadResult, error) {
		if userID != 7 || sessionID != "session-1" {
			t.Fatalf("Complete() user=%d session=%q", userID, sessionID)
		}
		return &service.UploadResult{TaskID: 42, Filename: "demo.mp4"}, nil
	}
	router := uploadSessionHandlerTestRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upload-sessions/session-1/complete", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadSessionHTTPStatusMapping(t *testing.T) {
	cases := []struct {
		kind service.UploadSessionErrorKind
		want int
	}{
		{service.UploadSessionErrorInvalid, http.StatusBadRequest},
		{service.UploadSessionErrorNotFound, http.StatusNotFound},
		{service.UploadSessionErrorConflict, http.StatusConflict},
		{service.UploadSessionErrorInProgress, http.StatusConflict},
		{service.UploadSessionErrorFailed, http.StatusConflict},
		{service.UploadSessionErrorExpired, http.StatusGone},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			err := &service.UploadSessionError{Kind: tc.kind, Message: "safe message"}
			if got := uploadSessionHTTPStatus(err); got != tc.want {
				t.Fatalf("status=%d want=%d", got, tc.want)
			}
		})
	}
	if got := uploadSessionHTTPStatus(io.EOF); got != http.StatusInternalServerError {
		t.Fatalf("unknown status=%d", got)
	}
}

func TestUploadSessionHandlerMapsOwnerIsolationToNotFound(t *testing.T) {
	stub := &uploadSessionHandlerStub{}
	stub.createFn = func(context.Context, int64, service.CreateUploadSessionRequest) (*service.UploadSessionView, error) {
		return nil, nil
	}
	stub.getFn = func(context.Context, int64, string) (*service.UploadSessionView, error) {
		return nil, &service.UploadSessionError{Kind: service.UploadSessionErrorNotFound, Message: "上传会话不存在"}
	}
	stub.acceptChunkFn = func(context.Context, int64, string, int, io.Reader) (*service.UploadChunkResult, error) {
		return nil, nil
	}
	stub.completeFn = func(context.Context, int64, string) (*service.UploadResult, error) { return nil, nil }
	router := uploadSessionHandlerTestRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/upload-sessions/foreign", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExpectedUploadSessionChunkSize(t *testing.T) {
	view := &service.UploadSessionView{FileSize: 11, ChunkSize: 5, TotalChunks: 3}
	for index, want := range []int64{5, 5, 1} {
		got, err := expectedUploadSessionChunkSize(view, strconv.Itoa(index))
		if err != nil || got != want {
			t.Fatalf("index=%d size=%d err=%v want=%d", index, got, err, want)
		}
	}
	for _, index := range []string{"-1", "3", "not-a-number"} {
		if _, err := expectedUploadSessionChunkSize(view, index); err == nil {
			t.Fatalf("index=%q error=nil", index)
		}
	}
}
