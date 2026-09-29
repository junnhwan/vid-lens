package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledURLImportRejectsBeforeCallingService(t *testing.T) {
	h := NewMediaHandler(nil).WithURLImportDisabled(true).WithSlowUploadNotice(true)
	r := gin.New()
	r.POST("/upload-url", h.UploadByURL)
	r.GET("/options", h.ImportOptions)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/upload-url", strings.NewReader(`{"url":"https://example.com/video"}`)))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "暂未开放") {
		t.Fatalf("response = %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/options", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"url_import_enabled":false`) || !strings.Contains(w.Body.String(), `"slow_upload_notice":true`) {
		t.Fatal(w.Body.String())
	}
	h.WithURLImportDisabled(false).WithSlowUploadNotice(false)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/options", nil))
	if !strings.Contains(w.Body.String(), `"url_import_enabled":true`) || !strings.Contains(w.Body.String(), `"slow_upload_notice":false`) {
		t.Fatal(w.Body.String())
	}
}
