package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type artifactEditModelFixture struct {
	mu        sync.Mutex
	responses []string
}

func (f *artifactEditModelFixture) serve(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.responses) == 0 {
		http.Error(w, "unexpected model call", http.StatusInternalServerError)
		return
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	artifactStreamResponse(w, response, "stop")
}

func TestArtifactEditFixtureModelAnswerDoesNotCreateVersionAndPersistsToolRecord(t *testing.T) {
	fixture := &artifactEditModelFixture{responses: []string{
		`{"tool":"answer_artifact_question","reason":"这是普通询问","public_summary":"只回答，不修改笔记","arguments":{"message":"当前证据只说明安装步骤，不能确认名称。","evidence_ids":[]}}`,
	}}
	service, db, calls := artifactFixture(t, fixture.serve)
	ctx := context.Background()
	detail := createArtifactEditFixtureTarget(t, service)

	run, err := service.SubmitEdit(ctx, 7, detail.ID, "answer-edit-key", ArtifactEditRequest{
		Instruction: "这个名称对吗？", ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModeAnswer,
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.SubmitEdit(ctx, 7, detail.ID, "answer-edit-key", ArtifactEditRequest{
		Instruction: "这个名称对吗？", ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModeAnswer,
	})
	if err != nil || replayed.ID != run.ID {
		t.Fatalf("submit replay=%+v err=%v", replayed, err)
	}
	if err = service.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := service.EditRun(ctx, 7, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.AgentRunStatusCompleted || completed.Result == nil || completed.Result.Kind != "answer" {
		t.Fatalf("completed run = %+v", completed)
	}
	wire, err := json.Marshal(completed)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	result, ok := decoded["result"].(map[string]any)
	if evidence, present := result["evidence_ids"].([]any); !ok || !present || len(evidence) != 0 {
		t.Fatalf("answer wire result must contain evidence_ids: []: %s", wire)
	}
	current, err := service.Get(ctx, 7, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.HeadVersion != detail.HeadVersion {
		t.Fatalf("answer created version: head=%d want=%d", current.HeadVersion, detail.HeadVersion)
	}
	var tools []model.AgentToolCall
	if err = db.Where("run_id=? AND call_kind=?", run.ID, model.AgentCallKindTool).Order("created_at").Find(&tools).Error; err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].ToolName != ArtifactEditToolAnswerQuestion || tools[0].Status != model.AgentToolCallStatusCompleted {
		t.Fatalf("tool records = %+v", tools)
	}
	if calls.Load() != 1 {
		t.Fatalf("model calls=%d want=1", calls.Load())
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("finished_at", time.Now().Add(-8*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err = service.repos.Artifact.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	retained, err := service.EditRun(ctx, 7, run.ID)
	if err != nil || retained.Result == nil || retained.Result.Kind != "answer" || retained.Result.Message != completed.Result.Message {
		t.Fatalf("prune removed durable answer result: run=%+v err=%v", retained, err)
	}
}

func TestArtifactEditFixtureModelApplyCommitsVersionAndPersistsToolRecords(t *testing.T) {
	fixture := &artifactEditModelFixture{}
	service, db, calls := artifactFixture(t, fixture.serve)
	ctx := context.Background()
	detail := createArtifactEditFixtureTarget(t, service)
	updated := "第一步安装。第二步验证。"
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: detail.ID, BaseVersionID: detail.Version.ID, BaseVersion: detail.HeadVersion,
		Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{},
		Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(detail.Version.Body.Blocks[0]), Content: &updated}},
	}
	patchJSON, _ := json.Marshal(patch)
	fixture.responses = []string{
		`{"tool":"propose_artifact_patch","reason":"明确要求改写","public_summary":"生成安装步骤修改","arguments":{"summary":"拆成安装步骤","patch":` + string(patchJSON) + `}}`,
		`{"tool":"commit_artifact_patch","reason":"proposal 已验证","public_summary":"正在保存新版本","arguments":{}}`,
	}

	run, err := service.SubmitEdit(ctx, 7, detail.ID, "apply-edit-key", ArtifactEditRequest{
		Instruction: "把安装部分改写成两个步骤", ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModeApply,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := service.EditRun(ctx, 7, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Result == nil || completed.Result.Kind != "committed" || completed.Result.ResultVersionID == "" {
		t.Fatalf("completed run = %+v", completed)
	}
	current, err := service.Get(ctx, 7, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.HeadVersion != detail.HeadVersion+1 || current.Version.Origin != "agent" || current.Version.Body.Blocks[0].Content != updated {
		t.Fatalf("committed artifact = %+v", current)
	}
	operation, err := service.EditOperation(ctx, 7, completed.Result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != model.ArtifactEditOperationCommitted || operation.ResultVersionID == nil || operation.Counts.Updated != 1 || operation.CreatedAt.IsZero() || operation.UpdatedAt.IsZero() {
		t.Fatalf("operation = %+v", operation)
	}
	var tools []model.AgentToolCall
	if err = db.Where("run_id=? AND call_kind=?", run.ID, model.AgentCallKindTool).Order("created_at").Find(&tools).Error; err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].ToolName != ArtifactEditToolProposePatch || tools[1].ToolName != ArtifactEditToolCommitPatch || tools[0].Status != model.AgentToolCallStatusCompleted || tools[1].Status != model.AgentToolCallStatusCompleted {
		t.Fatalf("tool records = %+v", tools)
	}
	if calls.Load() != 2 {
		t.Fatalf("model calls=%d want=2", calls.Load())
	}
	undone, err := service.UndoEdit(ctx, 7, operation.ID, "undo-edit-key", current.HeadVersion)
	if err != nil {
		t.Fatal(err)
	}
	if undone.UndoVersionID == nil || undone.CanUndo {
		t.Fatalf("undone operation = %+v", undone)
	}
	afterUndo, err := service.Get(ctx, 7, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterUndo.HeadVersion != detail.HeadVersion+2 || afterUndo.Version.Origin != "undo" || afterUndo.Version.Body.Blocks[0].Content != "旧内容" {
		t.Fatalf("artifact after undo = %+v", afterUndo)
	}
}

func TestArtifactEditFixtureModelPreviewPersistsProposalWithoutMovingHead(t *testing.T) {
	fixture := &artifactEditModelFixture{}
	service, _, calls := artifactFixture(t, fixture.serve)
	ctx := context.Background()
	detail := createArtifactEditFixtureTarget(t, service)
	updated := "候选修改"
	patch := artifact.Patch{SchemaVersion: 1, ArtifactID: detail.ID, BaseVersionID: detail.Version.ID, BaseVersion: detail.HeadVersion, Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{}, Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(detail.Version.Body.Blocks[0]), Content: &updated}}}
	patchJSON, _ := json.Marshal(patch)
	fixture.responses = []string{`{"tool":"propose_artifact_patch","reason":"先展示候选","public_summary":"生成候选差异","arguments":{"summary":"候选修改","patch":` + string(patchJSON) + `}}`}
	run, err := service.SubmitEdit(ctx, 7, detail.ID, "preview-edit-key", ArtifactEditRequest{Instruction: "请修改安装内容", ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModePreview})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := service.EditRun(ctx, 7, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Result == nil || completed.Result.Kind != "proposal" || completed.Result.OperationID == "" {
		t.Fatalf("preview run = %+v", completed)
	}
	current, err := service.Get(ctx, 7, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.HeadVersion != detail.HeadVersion {
		t.Fatalf("preview moved head to %d", current.HeadVersion)
	}
	operation, err := service.EditOperation(ctx, 7, completed.Result.OperationID)
	if err != nil || operation.Status != model.ArtifactEditOperationProposed || !operation.CanApply {
		t.Fatalf("proposal=%+v err=%v", operation, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("model calls=%d want=1", calls.Load())
	}
}

func TestArtifactEditDurationLimitUsesArtifactTerminalState(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	service, db, _ := artifactFixture(t, func(_ http.ResponseWriter, request *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-request.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	})
	detail := createArtifactEditFixtureTarget(t, service)
	run, err := service.SubmitEdit(context.Background(), 7, detail.ID, "duration-edit-key", ArtifactEditRequest{
		Instruction: "这个名称对吗？", ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModeAnswer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("max_duration_ms", 150).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- service.ExecuteArtifactEdit(context.Background(), run.ID) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("model call did not start before duration deadline")
	}
	if err = <-done; err != nil {
		t.Fatalf("ExecuteArtifactEdit() error = %v", err)
	}
	var stored model.AgentRun
	if err = db.Where("id=?", run.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.AgentRunStatusBudgetExhausted || stored.Stage != model.AgentRunStatusBudgetExhausted || stored.ErrorCode != "budget_exhausted" || stored.RunLeaseToken != "" || stored.RunLeaseUntil != nil || stored.FinishedAt == nil {
		t.Fatalf("duration terminal run = %+v", stored)
	}
	var step model.AgentStep
	if err = db.Where("run_id=?", run.ID).First(&step).Error; err != nil {
		t.Fatal(err)
	}
	if step.Status != model.AgentStepStatusFailed || step.ErrorCode != "duration_limit" {
		t.Fatalf("duration step = %+v", step)
	}
	events, err := service.EditEvents(context.Background(), 7, run.ID, 0)
	if err != nil || len(events) == 0 || events[len(events)-1].Type != "run.failed" {
		t.Fatalf("duration events = %+v error %v", events, err)
	}
}

func TestArtifactEditParentCancellationLeavesLeaseRecoverable(t *testing.T) {
	started := make(chan struct{})
	var mu sync.Mutex
	requests := 0
	service, db, _ := artifactFixture(t, func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests++
		attempt := requests
		mu.Unlock()
		if attempt == 1 {
			close(started)
			select {
			case <-request.Context().Done():
			case <-time.After(500 * time.Millisecond):
			}
			return
		}
		artifactStreamResponse(w, `{"tool":"answer_artifact_question","reason":"恢复后完成","public_summary":"只回答，不修改","arguments":{"message":"恢复成功。","evidence_ids":[]}}`, "stop")
	})
	detail := createArtifactEditFixtureTarget(t, service)
	run, err := service.SubmitEdit(context.Background(), 7, detail.ID, "parent-cancel-edit-key", ArtifactEditRequest{
		Instruction: "这个名称对吗？", ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModeAnswer,
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.ExecuteArtifactEdit(parent, run.ID) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("model call did not start before parent cancellation")
	}
	cancel()
	if executeErr := <-done; !errors.Is(executeErr, context.Canceled) {
		t.Fatalf("ExecuteArtifactEdit() error = %v, want context canceled", executeErr)
	}
	var interrupted model.AgentRun
	if err = db.Where("id=?", run.ID).First(&interrupted).Error; err != nil {
		t.Fatal(err)
	}
	if interrupted.Status != model.AgentRunStatusRunning || interrupted.RunLeaseToken == "" || interrupted.RunLeaseUntil == nil || interrupted.FinishedAt != nil {
		t.Fatalf("parent cancellation made run unrecoverable: %+v", interrupted)
	}
	events, err := service.EditEvents(context.Background(), 7, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == "run.completed" || event.Type == "run.failed" || event.Type == "run.cancelled" {
			t.Fatalf("parent cancellation wrote terminal event: %+v", events)
		}
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("run_lease_until", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err = service.ExecuteArtifactEdit(context.Background(), run.ID); err != nil {
		t.Fatalf("recovered ExecuteArtifactEdit() error = %v", err)
	}
	var recovered model.AgentRun
	if err = db.Where("id=?", run.ID).First(&recovered).Error; err != nil {
		t.Fatal(err)
	}
	if recovered.Status != model.AgentRunStatusCompleted || recovered.Stage != model.AgentRunStatusCompleted || recovered.RunLeaseToken != "" || recovered.RunLeaseUntil != nil || recovered.FinishedAt == nil || recovered.RunLeaseEpoch <= interrupted.RunLeaseEpoch {
		t.Fatalf("recovered run = %+v; interrupted epoch=%d", recovered, interrupted.RunLeaseEpoch)
	}
	events, err = service.EditEvents(context.Background(), 7, run.ID, 0)
	if err != nil || len(events) == 0 || events[len(events)-1].Type != "run.completed" {
		t.Fatalf("recovered events = %+v error %v", events, err)
	}
}

func TestArtifactEditClaimBudgetUsesArtifactTerminalState(t *testing.T) {
	service, db, calls := artifactFixture(t, artifactModelResponse)
	detail := createArtifactEditFixtureTarget(t, service)
	run, err := service.SubmitEdit(context.Background(), 7, detail.ID, "claim-budget-edit-key", ArtifactEditRequest{
		Instruction: "这个名称对吗？", ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModeAnswer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("max_llm_calls", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err = service.ExecuteArtifactEdit(context.Background(), run.ID); err != nil {
		t.Fatalf("ExecuteArtifactEdit() error = %v", err)
	}
	var stored model.AgentRun
	if err = db.Where("id=?", run.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.AgentRunStatusBudgetExhausted || stored.Stage != model.AgentRunStatusBudgetExhausted || stored.RunLeaseToken != "" || stored.RunLeaseUntil != nil || stored.FinishedAt == nil {
		t.Fatalf("claim budget terminal run = %+v", stored)
	}
	events, err := service.EditEvents(context.Background(), 7, run.ID, 0)
	if err != nil || len(events) == 0 || events[len(events)-1].Type != "run.failed" {
		t.Fatalf("claim budget events = %+v error %v", events, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("claim budget invoked model %d times", calls.Load())
	}
}

func TestArtifactRunRoutesRejectCrossSubjectReadsAndMutations(t *testing.T) {
	service, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	detail := createArtifactEditFixtureTarget(t, service)
	artifactID := detail.ID

	generation, err := service.Submit(ctx, 7, "cross-subject-generation", artifact.GenerationRequest{
		Kind: "study", Scope: "video", SourceIDs: []int64{42}, Goal: "重新生成", ArtifactID: &artifactID, BaseVersion: detail.HeadVersion,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := service.SubmitEdit(ctx, 7, detail.ID, "cross-subject-edit", ArtifactEditRequest{
		Instruction: "修改安装说明", ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModePreview,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = service.CancelEdit(ctx, 7, generation.ID); err == nil {
		t.Fatal("edit cancel accepted a generation run")
	} else {
		requireArtifactCode(t, err, "not_found")
	}
	if _, err = service.Cancel(ctx, 7, edit.ID); err == nil {
		t.Fatal("generation cancel accepted an edit run")
	} else {
		requireArtifactCode(t, err, "not_found")
	}
	currentGeneration, err := service.Run(ctx, 7, generation.ID)
	if err != nil || currentGeneration.Status != model.AgentRunStatusPending || currentGeneration.CancelRequested {
		t.Fatalf("generation changed after cross-subject cancel: run=%+v err=%v", currentGeneration, err)
	}
	currentEdit, err := service.EditRun(ctx, 7, edit.ID)
	if err != nil || currentEdit.Status != model.AgentRunStatusPending || currentEdit.CancelRequested {
		t.Fatalf("edit changed after cross-subject cancel: run=%+v err=%v", currentEdit, err)
	}

	if _, err = service.EditEvents(ctx, 7, generation.ID, 0); err == nil {
		t.Fatal("edit events exposed a generation run")
	} else {
		requireArtifactCode(t, err, "not_found")
	}
	if _, err = service.Events(ctx, 7, edit.ID, 0); err == nil {
		t.Fatal("generation events exposed an edit run")
	} else {
		requireArtifactCode(t, err, "not_found")
	}
	if events, eventsErr := service.Events(ctx, 7, generation.ID, 0); eventsErr != nil || len(events) != 1 {
		t.Fatalf("generation events=%+v err=%v", events, eventsErr)
	}
	if events, eventsErr := service.EditEvents(ctx, 7, edit.ID, 0); eventsErr != nil || len(events) != 1 {
		t.Fatalf("edit events=%+v err=%v", events, eventsErr)
	}

	publishedAt := time.Now().UTC()
	oldCreatedAt := publishedAt.Add(-time.Minute)
	if err = db.Model(&model.ArtifactEditDispatch{}).Where("run_id=?", edit.ID).Updates(map[string]any{"published_at": publishedAt, "created_at": oldCreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	var dispatchesBefore int64
	if err = db.Model(&model.ArtifactEditDispatch{}).Where("run_id=?", edit.ID).Count(&dispatchesBefore).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Resume(ctx, 7, edit.ID); err == nil {
		t.Fatal("generation resume accepted an edit run")
	} else {
		requireArtifactCode(t, err, "not_found")
	}
	var dispatchesAfter int64
	if err = db.Model(&model.ArtifactEditDispatch{}).Where("run_id=?", edit.ID).Count(&dispatchesAfter).Error; err != nil {
		t.Fatal(err)
	}
	if dispatchesAfter != dispatchesBefore {
		t.Fatalf("cross-subject resume queued edit dispatch: before=%d after=%d", dispatchesBefore, dispatchesAfter)
	}

	if currentGeneration, err = service.Cancel(ctx, 7, generation.ID); err != nil || currentGeneration.Status != model.AgentRunStatusCancelled {
		t.Fatalf("generation cancel run=%+v err=%v", currentGeneration, err)
	}
	if currentEdit, err = service.CancelEdit(ctx, 7, edit.ID); err != nil || currentEdit.Status != model.AgentRunStatusCancelled {
		t.Fatalf("edit cancel run=%+v err=%v", currentEdit, err)
	}
}

func TestArtifactEditSourceRevocationGatesOutcomeReplayAndKeepsListReadable(t *testing.T) {
	fixture := &artifactEditModelFixture{}
	service, db, _ := artifactFixture(t, fixture.serve)
	ctx := context.Background()
	detail := createArtifactEditFixtureTarget(t, service)

	if err := db.Create(&model.VideoTask{ID: 43, UserID: 7, FileMD5: "other-source", Filename: "other.mp4", Status: model.TaskStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VideoTranscription{TaskID: 43, FileMD5: "other-source", Content: "另一份独立来源。"}).Error; err != nil {
		t.Fatal(err)
	}
	otherSource, err := service.Source(ctx, 7, 43)
	if err != nil {
		t.Fatal(err)
	}
	otherBody := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "独立笔记", Blocks: []artifact.Block{{
		BlockID: "other", Type: "concept", Title: "独立内容", Content: "不应受另一来源删除影响。", ClaimOrigin: "source",
		EvidenceRefs: []artifact.Ref{{EvidenceID: otherSource.Evidence[0].ID, Relation: "supports"}},
	}}, Warnings: []string{}}
	other, err := service.Create(ctx, 7, []int64{43}, otherBody)
	if err != nil {
		t.Fatal(err)
	}

	secret := "SOURCE_BOUND_SECRET_9f4f"
	updated := "修改内容 " + secret
	patch := artifact.Patch{
		SchemaVersion: 1, ArtifactID: detail.ID, BaseVersionID: detail.Version.ID, BaseVersion: detail.HeadVersion,
		Basis: artifact.PatchBasisUserInstruction, EvidenceIDs: []string{},
		Operations: []artifact.PatchOperation{{Op: artifact.PatchOpUpdateBlock, BlockID: "install", ExpectedHash: artifact.BlockHash(detail.Version.Body.Blocks[0]), Content: &updated}},
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	fixture.responses = []string{`{"tool":"propose_artifact_patch","reason":"生成候选","public_summary":"来源敏感变更","arguments":{"summary":"来源敏感变更","patch":` + string(patchJSON) + `}}`}
	run, err := service.SubmitEdit(ctx, 7, detail.ID, "source-gate-preview", ArtifactEditRequest{
		Instruction: "修改安装说明 " + secret, ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModePreview,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := service.EditRun(ctx, 7, run.ID)
	if err != nil || completed.Result == nil || completed.Result.OperationID == "" {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	operationID := completed.Result.OperationID
	applied, err := service.ApplyEdit(ctx, 7, operationID, "source-gate-apply", detail.HeadVersion)
	if err != nil || applied.ResultVersionID == nil {
		t.Fatalf("apply=%+v err=%v", applied, err)
	}
	undone, err := service.UndoEdit(ctx, 7, operationID, "source-gate-undo", detail.HeadVersion+1)
	if err != nil || undone.UndoVersionID == nil {
		t.Fatalf("undo=%+v err=%v", undone, err)
	}

	if err = service.repos.Artifact.RevokeSource(42); err != nil {
		t.Fatal(err)
	}
	if _, err = service.EditOperation(ctx, 7, operationID); err == nil {
		t.Fatal("revoked operation remained readable")
	} else {
		requireArtifactCode(t, err, "source_deleted")
	}
	if _, err = service.ApplyEdit(ctx, 7, operationID, "source-gate-apply", detail.HeadVersion); err == nil {
		t.Fatal("apply idempotency replay bypassed source revocation")
	} else {
		requireArtifactCode(t, err, "source_deleted")
	}
	if _, err = service.UndoEdit(ctx, 7, operationID, "source-gate-undo", detail.HeadVersion+1); err == nil {
		t.Fatal("undo idempotency replay bypassed source revocation")
	} else {
		requireArtifactCode(t, err, "source_deleted")
	}

	rows, total, err := service.List(ctx, 7, 0, 1, 20)
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("list rows=%+v total=%d err=%v", rows, total, err)
	}
	var revokedFound, otherFound bool
	for i := range rows {
		switch rows[i].ID {
		case detail.ID:
			revokedFound = true
			if rows[i].LatestEditRun != nil {
				t.Fatalf("revoked list item exposed latest edit run: %+v", rows[i].LatestEditRun)
			}
		case other.ID:
			otherFound = true
		}
	}
	wire, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if !revokedFound || !otherFound || strings.Contains(string(wire), secret) {
		t.Fatalf("safe list metadata mismatch: revoked=%v other=%v wire=%s", revokedFound, otherFound, wire)
	}
}

func TestArtifactEditSourceRevocationGatesPendingRunEventsAndLatestRun(t *testing.T) {
	service, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	detail := createArtifactEditFixtureTarget(t, service)

	if err := db.Create(&model.VideoTask{ID: 43, UserID: 7, FileMD5: "pending-other-source", Filename: "other.mp4", Status: model.TaskStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VideoTranscription{TaskID: 43, FileMD5: "pending-other-source", Content: "另一份独立来源。"}).Error; err != nil {
		t.Fatal(err)
	}
	otherSource, err := service.Source(ctx, 7, 43)
	if err != nil {
		t.Fatal(err)
	}
	otherBody := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "独立笔记", Blocks: []artifact.Block{{
		BlockID: "other", Type: "concept", Title: "独立内容", Content: "不应受另一来源删除影响。", ClaimOrigin: "source",
		EvidenceRefs: []artifact.Ref{{EvidenceID: otherSource.Evidence[0].ID, Relation: "supports"}},
	}}, Warnings: []string{}}
	other, err := service.Create(ctx, 7, []int64{43}, otherBody)
	if err != nil {
		t.Fatal(err)
	}

	secret := "REVOKED_PENDING_INSTRUCTION_5c1b"
	run, err := service.SubmitEdit(ctx, 7, detail.ID, "source-gate-pending", ArtifactEditRequest{
		Instruction: secret, ExpectedHeadVersion: detail.HeadVersion, SelectedBlockIDs: []string{"install"}, Mode: ArtifactEditModeAnswer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.repos.Artifact.RevokeSource(42); err != nil {
		t.Fatal(err)
	}

	// The worker/recovery repository read intentionally remains available after
	// revocation; only public service projections are source-gated.
	rawRun, rawRequest, err := service.repos.Artifact.EditRun(ctx, 7, run.ID)
	if err != nil || rawRun.ID != run.ID || rawRequest.Instruction != secret {
		t.Fatalf("internal edit run read=%+v request=%+v err=%v", rawRun, rawRequest, err)
	}
	if _, err = service.EditRun(ctx, 7, run.ID); err == nil {
		t.Fatal("revoked pending edit run remained publicly readable")
	} else {
		requireArtifactCode(t, err, "source_deleted")
	}
	if _, err = service.EditEvents(ctx, 7, run.ID, 0); err == nil {
		t.Fatal("revoked pending edit events remained publicly readable")
	} else {
		requireArtifactCode(t, err, "source_deleted")
	}
	if _, err = service.Get(ctx, 7, detail.ID); err == nil {
		t.Fatal("revoked artifact detail remained readable")
	} else {
		requireArtifactCode(t, err, "source_deleted")
	}

	rows, total, err := service.List(ctx, 7, 0, 1, 20)
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("list rows=%+v total=%d err=%v", rows, total, err)
	}
	var revokedFound, otherFound bool
	for i := range rows {
		switch rows[i].ID {
		case detail.ID:
			revokedFound = true
			if rows[i].LatestEditRun != nil {
				t.Fatalf("revoked list item exposed latest edit run: %+v", rows[i].LatestEditRun)
			}
		case other.ID:
			otherFound = true
		}
	}
	wire, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if !revokedFound || !otherFound || strings.Contains(string(wire), secret) {
		t.Fatalf("safe list metadata mismatch: revoked=%v other=%v wire=%s", revokedFound, otherFound, wire)
	}
}

func createArtifactEditFixtureTarget(t *testing.T, service *ArtifactService) *ArtifactDetail {
	t.Helper()
	ctx := context.Background()
	source, err := service.Source(ctx, 7, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Evidence) == 0 {
		t.Fatal("fixture source has no evidence")
	}
	body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "安装笔记", Blocks: []artifact.Block{{
		BlockID: "install", Type: "concept", Title: "安装", Content: "旧内容", ClaimOrigin: "source",
		EvidenceRefs: []artifact.Ref{{EvidenceID: source.Evidence[0].ID, Relation: "supports"}},
	}}, Warnings: []string{}}
	detail, err := service.Create(ctx, 7, []int64{42}, body)
	if err != nil {
		t.Fatal(err)
	}
	return detail
}
