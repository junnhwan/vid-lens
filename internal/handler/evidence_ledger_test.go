package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestEvidenceLedgerHandlerEnforcesOwnerAndAppendsCorrection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	ledger := service.NewEvidenceLedgerService(repos)
	runID := "33333333-3333-3333-3333-333333333333"
	if err := ledger.RecordAnswer(context.Background(), service.EvidenceLedgerRecordRequest{
		UserID: 1, SessionID: 2, MessageID: 3, TaskID: 4, RunID: runID,
		RawAnswer: "待核验事实。[C1]",
		Evidence:  []service.Citation{{TaskID: 4, CitationID: "C1", EvidenceID: "handler-source", ChunkID: 5, Content: "引用"}},
	}); err != nil {
		t.Fatal(err)
	}
	view, err := ledger.GetRun(context.Background(), 1, runID)
	if err != nil || view == nil || len(view.Claims) != 1 {
		t.Fatalf("seed view=%+v err=%v", view, err)
	}
	claimID := view.Claims[0].ID

	h := &ChatHandler{}
	h.SetEvidenceLedgerService(ledger)
	owner := gin.New()
	owner.Use(func(c *gin.Context) { c.Set("userID", int64(1)) })
	owner.GET("/agent/evidence-ledgers/:run_id", h.GetEvidenceLedger)
	owner.POST("/agent/evidence-ledgers/claims/:claim_id/corrections", h.CorrectEvidenceClaim)

	get := httptest.NewRecorder()
	owner.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/agent/evidence-ledgers/"+runID, nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"run_id":"`+runID+`"`) || !strings.Contains(get.Body.String(), `"source_ref":"handler-source"`) {
		t.Fatalf("owner get code=%d body=%s", get.Code, get.Body.String())
	}

	correct := httptest.NewRecorder()
	body := strings.NewReader(`{"text":"人工更正后的事实。","reason":"人工查看原视频"}`)
	req := httptest.NewRequest(http.MethodPost, "/agent/evidence-ledgers/claims/"+claimID+"/corrections", body)
	req.Header.Set("Content-Type", "application/json")
	owner.ServeHTTP(correct, req)
	if correct.Code != http.StatusOK || !strings.Contains(correct.Body.String(), `"status":"corrected"`) {
		t.Fatalf("correction code=%d body=%s", correct.Code, correct.Body.String())
	}

	other := gin.New()
	other.Use(func(c *gin.Context) { c.Set("userID", int64(2)) })
	other.GET("/agent/evidence-ledgers/:run_id", h.GetEvidenceLedger)
	denied := httptest.NewRecorder()
	other.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/agent/evidence-ledgers/"+runID, nil))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("cross-owner get code=%d body=%s", denied.Code, denied.Body.String())
	}
}

func TestEvidenceLedgerHandlerRejectsDemoCorrection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ChatHandler{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", int64(1))
		c.Set("role", model.RoleDemo)
	})
	router.POST("/claims/:claim_id/corrections", h.CorrectEvidenceClaim)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/claims/claim/corrections", strings.NewReader(`{"text":"x","reason":"y"}`)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("demo correction code=%d body=%s", w.Code, w.Body.String())
	}
}
