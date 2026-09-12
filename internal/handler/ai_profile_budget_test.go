package handler

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAIProfileBudgetOptionsStaticRouteAndDemoReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAIProfileHandlerServiceForTest(t, nil)
	h := NewAIProfileHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", int64(7)); c.Set("role", "DEMO") })
	r.GET("/api/v1/ai/profiles/budget-options", h.BudgetOptions)
	r.PUT("/api/v1/ai/profiles/:id", h.Update)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ai/profiles/budget-options", nil))
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"max_tool_calls"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"unit":"seconds"`)) {
		t.Fatalf("options: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/ai/profiles/1", bytes.NewBufferString(`{"agent_budget":null}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("demo can alter budget: %d %s", rec.Code, rec.Body.String())
	}
}
