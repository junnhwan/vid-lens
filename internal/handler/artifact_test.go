package handler

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

func TestArtifactHandlerContractRoundTripAndEventReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.VideoTask{ID: 42, UserID: 7, FileMD5: "handler", Filename: "事务", Status: model.TaskStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.VideoTranscription{TaskID: 42, FileMD5: "handler", Content: "事务保证原子性。"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.NewArtifactService(repository.NewRepositories(db), nil, nil)
	h := NewArtifactHandler(svc)
	r := gin.New()
	r.Use(withTestUser(7))
	r.GET("/sources/video/:id", h.Source)
	r.POST("/artifacts", h.Create)
	r.GET("/artifacts/:id", h.Get)
	r.GET("/artifacts/:id/versions", h.Versions)
	r.GET("/artifacts/:id/versions/:version_id", h.Version)
	r.GET("/artifact-runs/:id", h.Run)
	r.GET("/tasks", h.Tasks)
	r.PATCH("/artifacts/:id", h.Save)
	r.GET("/artifact-runs/:id/events", h.Events)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		return rec
	}
	source := request("GET", "/sources/video/42", "")
	if source.Code != 200 {
		t.Fatal(source.Body.String())
	}
	var sourceEnvelope struct {
		Data service.ArtifactSource `json:"data"`
	}
	if err = json.Unmarshal(source.Body.Bytes(), &sourceEnvelope); err != nil {
		t.Fatal(err)
	}
	body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "学习笔记", Blocks: []artifact.Block{{BlockID: "one", Type: "concept", Title: "原子性", Content: "我的解释", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{{EvidenceID: sourceEnvelope.Data.Evidence[0].ID, Relation: "context"}}}}, Warnings: []string{}}
	created := request("POST", "/artifacts", artifact.JSON(map[string]any{"source_ids": []int{42}, "body": body}))
	if created.Code != 200 {
		t.Fatal(created.Body.String())
	}
	var detail struct {
		Data service.ArtifactDetail `json:"data"`
	}
	if err = json.Unmarshal(created.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Data.Version == nil || detail.Data.Version.SourceStatus != "current" || detail.Data.CurrentVersionID == nil {
		t.Fatal(created.Body.String())
	}
	path := "/artifacts/" + detail.Data.ID
	edit := request("PATCH", path, artifact.JSON(map[string]any{"expected_head_version": 1, "body": body}))
	if edit.Code != 200 {
		t.Fatal(edit.Body.String())
	}
	conflict := request("PATCH", path, artifact.JSON(map[string]any{"expected_head_version": 1, "body": body}))
	if conflict.Code != 409 || !strings.Contains(conflict.Body.String(), `"error_code":"version_conflict"`) {
		t.Fatal(conflict.Body.String())
	}
	invalid := request("POST", "/artifacts", `{"source_ids":[42],"body":{},"unexpected":true}`)
	if invalid.Code != 400 {
		t.Fatal(invalid.Body.String())
	}
	run := model.AgentRun{ID: "fixture-run", UserID: 7, SubjectKind: "generation_request", SubjectID: "fixture-request", ExecutionKind: "artifact", RecipeVersion: artifact.Recipe, ScopeType: "video", TaskID: 42, Goal: "notes", Mode: "artifact", ProfileSnapshot: "{}", BudgetSnapshot: "{}", PolicySnapshot: "{}", Status: "completed", Stage: "completed", EventSeq: 2}
	if err = db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.GenerationRequest{ID: "fixture-request", RunID: run.ID, UserID: 7, IdempotencyKey: "fixture", RequestHash: artifact.Hash("fixture"), RequestJSON: "{}", ArtifactID: detail.Data.ID, ManifestID: sourceEnvelope.Data.ManifestID, QueueDeadline: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 2; i++ {
		if err = db.Create(&model.RunEvent{RunID: run.ID, Seq: i, Type: "run.completed", DataJSON: `{"status":"completed"}`}).Error; err != nil {
			t.Fatal(err)
		}
	}
	stream := request("GET", "/artifact-runs/fixture-run/events?after_seq=1", "")
	if stream.Code != http.StatusOK || !strings.Contains(stream.Body.String(), "id: 2") || strings.Contains(stream.Body.String(), "id: 1") || !strings.Contains(stream.Body.String(), `"schema_version":1`) {
		t.Fatal(stream.Body.String())
	}
	future := request("GET", "/artifact-runs/fixture-run/events?after_seq=3", "")
	if future.Code != 400 {
		t.Fatal(future.Body.String())
	}
	if output := os.Getenv("VIDLENS_ARTIFACT_CONTRACT_OUT"); output != "" {
		samples := map[string]json.RawMessage{
			"source": source.Body.Bytes(), "created_artifact": created.Body.Bytes(), "artifact": edit.Body.Bytes(), "version_conflict": conflict.Body.Bytes(),
			"version":  request("GET", path+"/versions/"+detail.Data.Version.ID, "").Body.Bytes(),
			"versions": request("GET", path+"/versions", "").Body.Bytes(),
			"run":      request("GET", "/artifact-runs/fixture-run", "").Body.Bytes(),
			"tasks":    request("GET", "/tasks", "").Body.Bytes(),
		}
		raw, e := json.MarshalIndent(samples, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(output, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
