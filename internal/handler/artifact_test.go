package handler

import (
	"bytes"
	"context"
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
	r.GET("/artifacts", h.List)
	r.GET("/artifacts/:id", h.Get)
	r.GET("/artifacts/:id/versions", h.Versions)
	r.GET("/artifacts/:id/versions/:version_id", h.Version)
	r.GET("/artifact-runs/:id", h.Run)
	r.POST("/artifacts/:id/edit-runs", h.SubmitEdit)
	r.GET("/artifact-edit-runs/:id", h.EditRun)
	r.GET("/artifact-edit-operations/:id", h.EditOperation)
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
	var editedDetail struct {
		Data service.ArtifactDetail `json:"data"`
	}
	if err = json.Unmarshal(edit.Body.Bytes(), &editedDetail); err != nil || editedDetail.Data.Version == nil {
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
	invalidEdit := request("POST", path+"/edit-runs", `{"instruction":"改名","expected_head_version":2,"selected_block_ids":[],"mode":"apply","unexpected":true}`)
	if invalidEdit.Code != 400 {
		t.Fatalf("edit request must reject unknown fields: %s", invalidEdit.Body.String())
	}
	editRun := model.AgentRun{ID: "fixture-edit-run", UserID: 7, SubjectKind: model.AgentRunSubjectArtifactEdit, SubjectID: "fixture-edit-request", ExecutionKind: "artifact", RecipeVersion: service.ArtifactEditRecipe, ScopeType: "video", TaskID: 42, Goal: "纠正名称", Mode: "preview", ProfileSnapshot: "{}", BudgetSnapshot: "{}", PolicySnapshot: "{}", Status: "pending", Stage: "queued", EventSeq: 1, CreatedAt: time.Now().UTC()}
	if err = db.Create(&editRun).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.ArtifactEditRequest{ID: "fixture-edit-request", RunID: editRun.ID, UserID: 7, IdempotencyKey: "fixture-edit", RequestHash: artifact.Hash("fixture-edit"), RequestJSON: `{}`, ArtifactID: detail.Data.ID, BaseVersionID: editedDetail.Data.Version.ID, BaseVersion: editedDetail.Data.Version.Version, ManifestID: sourceEnvelope.Data.ManifestID, Instruction: editRun.Goal, Mode: editRun.Mode, SelectedBlockIDsJSON: `[]`, Recipe: service.ArtifactEditRecipe, ProfileFingerprint: "fixture", BudgetJSON: `{}`, ToolPolicyJSON: `{}`, QueueDeadline: time.Now().Add(time.Hour), CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	editRunResponse := request("GET", "/artifact-edit-runs/fixture-edit-run", "")
	if editRunResponse.Code != 200 || !strings.Contains(editRunResponse.Body.String(), `"instruction":"纠正名称"`) || !strings.Contains(editRunResponse.Body.String(), `"mode":"preview"`) {
		t.Fatalf("edit run contract: %s", editRunResponse.Body.String())
	}
	operation := model.ArtifactEditOperation{ID: "fixture-edit-operation", UserID: 7, ArtifactID: detail.Data.ID, Kind: model.ArtifactEditOperationKindEdit, BaseVersionID: editedDetail.Data.Version.ID, BaseVersion: editedDetail.Data.Version.Version, ManifestID: sourceEnvelope.Data.ManifestID, CanonicalPatchJSON: `{}`, PatchHash: artifact.Hash("patch"), AuthorizationJSON: `{}`, ScopeJSON: `[]`, ToolSchemaDigest: artifact.Hash("tools"), Basis: artifact.PatchBasisUserInstruction, EvidenceIDsJSON: `[]`, Status: model.ArtifactEditOperationProposed, Summary: "纠正产品名称", CountsJSON: `{"added":0,"updated":1,"deleted":0,"moved":0}`, ChangesJSON: `[]`, BlockMappingsJSON: `[]`, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err = db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	operationResponse := request("GET", "/artifact-edit-operations/fixture-edit-operation", "")
	if operationResponse.Code != 200 || !strings.Contains(operationResponse.Body.String(), `"summary":"纠正产品名称"`) || strings.Contains(operationResponse.Body.String(), "canonical_patch_json") {
		t.Fatalf("safe edit operation contract: %s", operationResponse.Body.String())
	}
	run := model.AgentRun{ID: "fixture-run", UserID: 7, SubjectKind: "generation_request", SubjectID: "fixture-request", ExecutionKind: "artifact", RecipeVersion: artifact.Recipe, ScopeType: "video", TaskID: 42, Goal: "notes", Mode: "artifact", ProfileSnapshot: "{}", BudgetSnapshot: "{}", PolicySnapshot: "{}", Status: "completed", Stage: "completed", EventSeq: 2}
	if err = db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.GenerationRequest{ID: "fixture-request", RunID: run.ID, UserID: 7, IdempotencyKey: "fixture", RequestHash: artifact.Hash("fixture"), RequestJSON: "{}", ArtifactID: detail.Data.ID, ManifestID: sourceEnvelope.Data.ManifestID, QueueDeadline: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	linked := request("GET", path, "")
	var linkedDetail struct {
		Data service.ArtifactDetail `json:"data"`
	}
	if linked.Code != 200 || json.Unmarshal(linked.Body.Bytes(), &linkedDetail) != nil || linkedDetail.Data.LatestRun == nil || linkedDetail.Data.LatestRun.ID != run.ID || linkedDetail.Data.LatestRun.ArtifactID != detail.Data.ID || linkedDetail.Data.LatestRun.SourceTaskID != 42 || linkedDetail.Data.LatestEditRun == nil || linkedDetail.Data.LatestEditRun.ID != editRun.ID {
		t.Fatalf("artifact detail latest_run contract: %s", linked.Body.String())
	}
	listed := request("GET", "/artifacts?source_id=42", "")
	var listedArtifacts struct {
		Data struct {
			List []service.ArtifactListItem `json:"list"`
		} `json:"data"`
	}
	if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &listedArtifacts) != nil || len(listedArtifacts.Data.List) != 1 || listedArtifacts.Data.List[0].LatestRun == nil || listedArtifacts.Data.List[0].LatestRun.ID != run.ID || listedArtifacts.Data.List[0].LatestEditRun == nil || listedArtifacts.Data.List[0].LatestEditRun.ID != editRun.ID {
		t.Fatalf("artifact list latest_run contract: %s", listed.Body.String())
	}
	tasks := request("GET", "/tasks", "")
	var taskEnvelope struct {
		Data struct {
			List []service.ArtifactTaskView `json:"list"`
		} `json:"data"`
	}
	if tasks.Code != 200 || json.Unmarshal(tasks.Body.Bytes(), &taskEnvelope) != nil || len(taskEnvelope.Data.List) == 0 || taskEnvelope.Data.List[0].ResourceID != run.ID || taskEnvelope.Data.List[0].Run == nil || taskEnvelope.Data.List[0].Run.ArtifactID != detail.Data.ID {
		t.Fatalf("task resource/run/artifact identity contract: %s", tasks.Body.String())
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
			"version":        request("GET", path+"/versions/"+detail.Data.Version.ID, "").Body.Bytes(),
			"versions":       request("GET", path+"/versions", "").Body.Bytes(),
			"run":            request("GET", "/artifact-runs/fixture-run", "").Body.Bytes(),
			"edit_run":       editRunResponse.Body.Bytes(),
			"edit_operation": operationResponse.Body.Bytes(),
			"tasks":          request("GET", "/tasks", "").Body.Bytes(),
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

func TestArtifactHandlerRevokedSourceHidesEditRunAndKeepsArtifactListReadable(t *testing.T) {
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
	for _, task := range []model.VideoTask{
		{ID: 42, UserID: 7, FileMD5: "revoked-handler", Filename: "revoked.mp4", Status: model.TaskStatusCompleted},
		{ID: 43, UserID: 7, FileMD5: "other-handler", Filename: "other.mp4", Status: model.TaskStatusCompleted},
	} {
		if err = db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, transcript := range []model.VideoTranscription{
		{TaskID: 42, FileMD5: "revoked-handler", Content: "即将撤销的来源。"},
		{TaskID: 43, FileMD5: "other-handler", Content: "另一份独立来源。"},
	} {
		if err = db.Create(&transcript).Error; err != nil {
			t.Fatal(err)
		}
	}

	repos := repository.NewRepositories(db)
	svc := service.NewArtifactService(repos, nil, nil)
	ctx := context.Background()
	revokedSource, err := svc.Source(ctx, 7, 42)
	if err != nil {
		t.Fatal(err)
	}
	revokedBody := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "待撤销笔记", Blocks: []artifact.Block{{
		BlockID: "revoked", Type: "concept", Title: "待撤销", Content: "来源绑定内容", ClaimOrigin: "source",
		EvidenceRefs: []artifact.Ref{{EvidenceID: revokedSource.Evidence[0].ID, Relation: "supports"}},
	}}, Warnings: []string{}}
	revokedArtifact, err := svc.Create(ctx, 7, []int64{42}, revokedBody)
	if err != nil {
		t.Fatal(err)
	}
	otherSource, err := svc.Source(ctx, 7, 43)
	if err != nil {
		t.Fatal(err)
	}
	otherBody := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "独立笔记", Blocks: []artifact.Block{{
		BlockID: "other", Type: "concept", Title: "独立", Content: "保持可读", ClaimOrigin: "source",
		EvidenceRefs: []artifact.Ref{{EvidenceID: otherSource.Evidence[0].ID, Relation: "supports"}},
	}}, Warnings: []string{}}
	otherArtifact, err := svc.Create(ctx, 7, []int64{43}, otherBody)
	if err != nil {
		t.Fatal(err)
	}

	secret := "REVOKED_HTTP_INSTRUCTION_8d73"
	now := time.Now().UTC()
	run := model.AgentRun{ID: "revoked-edit-run", UserID: 7, SubjectKind: model.AgentRunSubjectArtifactEdit, SubjectID: "revoked-edit-request", ExecutionKind: "artifact", RecipeVersion: service.ArtifactEditRecipe, ScopeType: "video", TaskID: 42, Goal: secret, Mode: "answer", ProfileSnapshot: "{}", BudgetSnapshot: "{}", PolicySnapshot: "{}", Status: model.AgentRunStatusCompleted, Stage: "completed", EventSeq: 1, CreatedAt: now, UpdatedAt: now, FinishedAt: &now}
	if err = db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	request := model.ArtifactEditRequest{ID: run.SubjectID, RunID: run.ID, UserID: 7, IdempotencyKey: "revoked-handler-edit", RequestHash: artifact.Hash("revoked-handler-edit"), RequestJSON: `{}`, ArtifactID: revokedArtifact.ID, BaseVersionID: revokedArtifact.Version.ID, BaseVersion: revokedArtifact.HeadVersion, ManifestID: revokedSource.ManifestID, Instruction: secret, Mode: "answer", SelectedBlockIDsJSON: `[]`, Recipe: service.ArtifactEditRecipe, ProfileFingerprint: "fixture", BudgetJSON: `{}`, ToolPolicyJSON: `{}`, QueueDeadline: now.Add(time.Hour), CreatedAt: now}
	if err = db.Create(&request).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.RunEvent{RunID: run.ID, Seq: 1, Type: "run.completed", DataJSON: artifact.JSON(map[string]any{"status": "completed", "summary": secret}), CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err = repos.Artifact.RevokeSource(42); err != nil {
		t.Fatal(err)
	}

	h := NewArtifactHandler(svc)
	router := gin.New()
	router.Use(withTestUser(7))
	router.GET("/artifacts", h.List)
	router.GET("/artifacts/:id", h.Get)
	router.GET("/artifact-edit-runs/:id", h.EditRun)
	router.GET("/artifact-edit-runs/:id/events", h.EditEvents)
	doRequest := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder
	}

	for _, path := range []string{"/artifact-edit-runs/" + run.ID, "/artifact-edit-runs/" + run.ID + "/events", "/artifacts/" + revokedArtifact.ID} {
		response := doRequest(path)
		if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), `"error_code":"source_deleted"`) || strings.Contains(response.Body.String(), secret) {
			t.Fatalf("revoked public read path=%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}

	listed := doRequest("/artifacts")
	var envelope struct {
		Data struct {
			List []service.ArtifactListItem `json:"list"`
		} `json:"data"`
	}
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &envelope) != nil || len(envelope.Data.List) != 2 || strings.Contains(listed.Body.String(), secret) {
		t.Fatalf("artifact list status=%d body=%s", listed.Code, listed.Body.String())
	}
	var revokedFound, otherFound bool
	for i := range envelope.Data.List {
		switch envelope.Data.List[i].ID {
		case revokedArtifact.ID:
			revokedFound = true
			if envelope.Data.List[i].LatestEditRun != nil {
				t.Fatalf("revoked artifact leaked latest edit run: %+v", envelope.Data.List[i].LatestEditRun)
			}
		case otherArtifact.ID:
			otherFound = true
		}
	}
	if !revokedFound || !otherFound {
		t.Fatalf("artifact list omitted rows: revoked=%v other=%v body=%s", revokedFound, otherFound, listed.Body.String())
	}
}
