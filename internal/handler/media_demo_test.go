package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/model"
)

// TestDenyIfDemoRoleGate 验证只读 gate 只拦 DEMO，其它角色（含无 role 的旧 token）一律放行。
func TestDenyIfDemoRoleGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		role string
		want bool
		code int
	}{
		{name: "demo blocked", role: model.RoleDemo, want: true, code: http.StatusForbidden},
		{name: "user allowed", role: model.RoleUser, want: false, code: http.StatusOK},
		{name: "admin allowed", role: model.RoleAdmin, want: false, code: http.StatusOK},
		{name: "no role allowed (legacy token)", role: "", want: false, code: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Set("role", tc.role)
			if got := denyIfDemo(c, "上传视频"); got != tc.want {
				t.Fatalf("denyIfDemo() = %v, want %v", got, tc.want)
			}
			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d", rec.Code, tc.code)
			}
		})
	}
}

// TestDemoUserMediaMutationsRejected 演示账号对全部写操作（上传/URL 下载/分片/合并/
// 转写/摘要/删除/索引触发）一律 403，且不触碰底层服务（传 nil svc 即可证明）。
func TestDemoUserMediaMutationsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	media := NewMediaHandler(nil)
	rag := NewRAGHandler(nil, nil, nil)
	cases := []struct {
		name    string
		method  string
		path    string
		handler func(c *gin.Context)
	}{
		{name: "upload", method: http.MethodPost, path: "/media/upload", handler: media.UploadFile},
		{name: "upload-url", method: http.MethodPost, path: "/media/upload-url", handler: media.UploadByURL},
		{name: "upload-chunk", method: http.MethodPost, path: "/media/upload-chunk", handler: media.UploadChunk},
		{name: "merge-chunks", method: http.MethodPost, path: "/media/merge-chunks", handler: media.MergeChunks},
		{name: "analyze", method: http.MethodPost, path: "/media/analyze/1", handler: media.RequestAnalysis},
		{name: "transcribe", method: http.MethodPost, path: "/media/transcribe/1", handler: media.RequestTranscribe},
		{name: "delete", method: http.MethodDelete, path: "/media/task/1", handler: media.DeleteTask},
		{name: "rag-index", method: http.MethodPost, path: "/media/task/1/rag-index", handler: rag.BuildTaskIndex},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Handle(tc.method, tc.path, func(c *gin.Context) {
				c.Set("role", model.RoleDemo)
				c.Set("userID", int64(1))
				tc.handler(c)
			})
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}