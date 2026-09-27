package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestArtifactCORSAllowsConflictAndReplayHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS())
	r.PATCH("/artifacts/x", func(c *gin.Context) { c.Status(200) })
	req := httptest.NewRequest("OPTIONS", "/artifacts/x", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "authorization,idempotency-key,last-event-id,content-type")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("preflight %d %s", rec.Code, rec.Body.String())
	}
}
