package repository

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/summarydoc"
)

func TestSummaryVisualRetryAcceptanceRollbackAndImmutableParent(t *testing.T) {
	runSummaryVisualRetryAcceptance(t, summaryRevisionDB(t), "")
}
func TestPostgresSummaryVisualRetryAcceptanceRollbackAndImmutableParent(t *testing.T) {
	fixture := openPostgresRepositoryTestDB(t)
	runSummaryVisualRetryAcceptance(t, fixture.db, fixture.scopedDSN)
}

func TestSummaryVisualRetryPublishesNewVersionAtomicallyPreservingUserHead(t *testing.T) {
	runSummaryVisualRetryAcceptance(t, summaryRevisionDB(t), "", true)
}
func TestPostgresSummaryVisualRetryPublishesNewVersionAtomicallyPreservingUserHead(t *testing.T) {
	f := openPostgresRepositoryTestDB(t)
	runSummaryVisualRetryAcceptance(t, f.db, f.scopedDSN, true)
}

func runSummaryVisualRetryAcceptance(t *testing.T, db *gorm.DB, scopedDSN string, publish ...bool) {
	t.Helper()
	ctx := context.Background()
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 91, 9)
	source, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: sourceFixture(t, "核对配置条件。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	opts, _ := processing.Normalize(processing.Options{AutoSummary: true, SummaryVisualEnabled: true, OutputMode: "image_text"}, false)
	oldIntent := processing.Intent{ID: uuid.NewString(), GenerationID: uuid.NewString(), Version: 1, Options: opts, ProfileID: 1, ProfileFingerprint: "profile", RecipeVersion: processing.Recipe, PolicyJSON: "{}", BudgetJSON: "{}"}
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: oldIntent.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, PresentationMode: "text", Title: "配置摘要", Overview: "核对条件", Blocks: []summarydoc.Block{{ID: "config", Order: 0, Title: "条件", BodyMarkdown: "检查配置。"}}}
	canonical, _ := summarydoc.CanonicalJSON(doc)
	content, _ := summarydoc.Markdown(doc)
	digest, _ := summarydoc.Digest(doc)
	base := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: content, DocumentJSON: string(canonical), SchemaVersion: summarydoc.SchemaVersion, SourceID: source.ID, SourceDigest: source.SourceDigest, ContentDigest: digest, ContentHashKind: summarydoc.HashKind, GeneratedVersion: 1, GenerationID: oldIntent.GenerationID, ModelName: "old-profile"}
	if err = db.Create(&base).Error; err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{ID: oldIntent.GenerationID, UserID: task.UserID, TaskID: task.ID, SubjectKind: model.AgentRunSubjectSummaryGeneration, SubjectID: oldIntent.GenerationID, ExecutionKind: "artifact", RecipeVersion: processing.Recipe, ScopeType: model.ChatScopeVideo, Status: model.AgentRunStatusCompleted, Stage: "finalizing", ProfileSnapshot: "{}", PolicySnapshot: artifact.JSON(map[string]any{"source_id": source.ID, "source_digest": source.SourceDigest}), BudgetSnapshot: "{}", LLMCallsUsed: 3, VisionCallsUsed: 1, FramesUsed: 1, MaxSteps: 10, MaxLLMCalls: 10, MaxVisionCalls: 8, MaxAttemptsPerStep: 2}
	if err = db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	job := model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeSummary, GenerationID: run.ID, InputSourceID: source.ID, InputText: source.CanonicalText, Status: model.TaskStatusCompleted, MaxRetries: 3}
	if err = db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task = *loaded
	task.ProcessingIntentJSON = artifact.JSON(oldIntent)
	task.Status = model.TaskStatusCompleted
	if err = db.Save(task).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.First(&run, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.First(&base, "id=?", base.ID).Error; err != nil {
		t.Fatal(err)
	}
	parentBytes, bodyBytes := artifact.JSON(run), artifact.JSON(base)
	revision := model.SummaryRevision{ID: uuid.NewString(), UserID: task.UserID, TaskID: task.ID, Version: 1, Content: "保留用户修改", ContentHashKind: model.SummaryHashMarkdown, BaseGeneratedHash: base.ContentDigest, BaseGeneratedHashKind: base.ContentHashKind, Origin: "manual"}
	head := model.SummaryRevisionHead{UserID: task.UserID, TaskID: task.ID, Version: 1, CurrentRevisionID: revision.ID}
	if err = db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&head).Error; err != nil {
		t.Fatal(err)
	}
	db.First(&revision, "id=?", revision.ID)
	db.First(&head, "user_id=? AND task_id=?", task.UserID, task.ID)
	revisionBytes, headBytes := artifact.JSON(revision), artifact.JSON(head)
	intent := oldIntent
	intent.ID = uuid.NewString()
	intent.GenerationID = uuid.NewString()
	now := time.Now()
	request := PrepareSummaryVisualRetryRequest{UserID: task.UserID, TaskID: task.ID, ExpectedGenerationID: base.GenerationID, ExpectedSourceID: source.ID, ExpectedSourceDigest: source.SourceDigest, ExpectedContentDigest: base.ContentDigest, ExpectedGeneratedVersion: base.GeneratedVersion, ExpectedIntentJSON: task.ProcessingIntentJSON, Intent: intent, Token: "visual-dispatch", Now: now, LeaseUntil: now.Add(time.Minute)}
	action, key, hash := "summary_visual_retry", "same-attempt", processing.Fingerprint("receipt")
	bad := request
	bad.ExpectedGeneratedVersion++
	if _, _, err = repos.AcceptImport(ctx, task.UserID, action, key, hash, func(tx *Repositories) (*model.VideoTask, error) {
		out, err := tx.PrepareSummaryVisualRetry(ctx, bad)
		return &out.Task, err
	}); err == nil {
		t.Fatal("stale base accepted")
	}
	var receipts int64
	db.Model(&model.ImportRequest{}).Count(&receipts)
	if receipts != 0 {
		t.Fatal("failed acceptance receipt committed")
	}
	var prepared InitialTaskDispatch
	if scopedDSN != "" {
		var accepted atomic.Int32
		start := make(chan struct{})
		failures := make(chan error, 5)
		var group sync.WaitGroup
		for index := 0; index < 5; index++ {
			peer := NewRepositories(openPostgresRepositoryPeer(t, scopedDSN))
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				_, created, err := peer.AcceptImport(ctx, task.UserID, action, key, hash, func(tx *Repositories) (*model.VideoTask, error) {
					out, err := tx.PrepareSummaryVisualRetry(ctx, request)
					return &out.Task, err
				})
				if err != nil {
					failures <- err
				}
				if created {
					accepted.Add(1)
				}
			}()
		}
		close(start)
		group.Wait()
		close(failures)
		for err := range failures {
			t.Error(err)
		}
		if accepted.Load() != 1 {
			t.Fatalf("same-key concurrent winners=%d", accepted.Load())
		}
		current, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
		prepared = InitialTaskDispatch{Task: task, Token: current.ProcessingToken}
	}
	accepted, created, err := repos.AcceptImport(ctx, task.UserID, action, key, hash, func(tx *Repositories) (*model.VideoTask, error) {
		prepared, err = tx.PrepareSummaryVisualRetry(ctx, request)
		return &prepared.Task, err
	})
	if err != nil || created != (scopedDSN == "") || accepted.ID != task.ID {
		t.Fatalf("accept %t %+v %v", created, accepted, err)
	}
	if _, again, err := repos.AcceptImport(ctx, task.UserID, action, key, hash, func(*Repositories) (*model.VideoTask, error) { t.Fatal("same key reran acceptance"); return nil, nil }); err != nil || again {
		t.Fatal("same key duplicate")
	}
	if _, err := repos.ImportRequest.Lookup(ctx, task.UserID, action, key, processing.Fingerprint("different")); err == nil {
		t.Fatal("mismatched key accepted")
	}
	currentJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	var frozen processing.GenerationSnapshot
	if err = artifact.Decode([]byte(currentJob.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.Operation != processing.OperationVisualRetry || frozen.VisualRetry == nil || frozen.VisualRetry.BaseDocumentJSON != base.DocumentJSON || !frozen.VisualRetry.NewBudgetAuthorized || frozen.VisualRetry.PreviousSpendJSON == "" || currentJob.GenerationID != intent.GenerationID {
		t.Fatalf("frozen retry %+v", frozen)
	}
	if gen, err := repos.ImportRequest.ReadAcceptedGeneration(ctx, task.UserID, action, key); err != nil || gen != intent.GenerationID {
		t.Fatal("original receipt identity lost")
	}
	var afterRun model.AgentRun
	db.First(&afterRun, "id=?", run.ID)
	if artifact.JSON(afterRun) != parentBytes {
		t.Fatal("old run mutated")
	}
	afterBase, _ := repos.Summary.FindByTaskID(task.ID)
	if artifact.JSON(afterBase) != bodyBytes {
		t.Fatal("text rewritten on acceptance")
	}
	claim, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{TaskID: task.ID, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, MessageToken: prepared.Token, NewToken: "retry-worker", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || claim.Outcome != TaskLeaseAcquired {
		t.Fatalf("claim %+v %v", claim, err)
	}
	lease := SummaryGenerationLease{UserID: task.UserID, TaskID: task.ID, GenerationID: intent.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: claim.Token}
	child := run
	child.ID = intent.GenerationID
	child.SubjectID = child.ID
	child.Status = model.AgentRunStatusRunning
	child.Stage = "visual_retry"
	child.LLMCallsUsed = 0
	child.VisionCallsUsed = 0
	child.FramesUsed = 0
	child.CreatedAt = time.Time{}
	child.UpdatedAt = time.Time{}
	if _, err = repos.StartSummaryGeneration(ctx, lease, &child); err != nil {
		t.Fatal(err)
	}
	if err = repos.MarkSummaryVisualTextReused(ctx, lease, frozen); err != nil {
		t.Fatal(err)
	}
	observation := model.VideoVisualObservation{ID: uuid.NewString(), UserID: task.UserID, TaskID: task.ID, VideoRevision: task.FileMD5, ObjectKey: "visual-investigations/retry-owned.jpg", StartMS: 1500, EndMS: 1501, Status: model.VisualObservationStatusObserved, RawResponseHash: "observed-hash", CacheKey: uuid.NewString()}
	if err = db.Create(&observation).Error; err != nil {
		t.Fatal(err)
	}
	publishImage := func(expectedVersion int64) error {
		return repos.TransactionContext(ctx, func(tx *Repositories) error {
			ref, err := tx.RegisterSummaryScreenshot(ctx, RegisterSummaryScreenshotRequest{UserID: task.UserID, TaskID: task.ID, GenerationID: intent.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: claim.Token, BlockID: "config", ObservationID: observation.ID, CueIDs: []string{"cue-1"}})
			if err != nil {
				return err
			}
			imageDoc := doc
			imageDoc.DocumentID = intent.GenerationID
			imageDoc.PresentationMode = "image_text"
			imageDoc.Blocks = append([]summarydoc.Block(nil), doc.Blocks...)
			imageDoc.Blocks[0].Figures = []summarydoc.Figure{{ID: "figure-" + ref.ID, ScreenshotRef: ref.ID, CaptureMS: &ref.CaptureMS, Caption: "配置画面", Alt: "参数配置", Supports: "核对参数"}}
			_, err = tx.PublishSummaryDocument(ctx, PublishSummaryDocumentRequest{UserID: task.UserID, TaskID: task.ID, GenerationID: intent.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: claim.Token, ExpectedGeneratedVersion: expectedVersion, ExpectedGeneratedHash: base.ContentDigest, ExpectedGeneratedHashKind: base.ContentHashKind, Document: imageDoc, ModelName: base.ModelName})
			return err
		})
	}
	if err = publishImage(base.GeneratedVersion + 1); err == nil {
		t.Fatal("visual publication ignored frozen base CAS")
	}
	var screenshots int64
	db.Model(&model.SummaryScreenshotRef{}).Where("generation_id=?", intent.GenerationID).Count(&screenshots)
	if screenshots != 0 {
		t.Fatal("screenshot committed without matching document CAS")
	}
	visualState, reason := "failed", "invalid_visual_response"
	if len(publish) > 0 && publish[0] {
		if err = publishImage(base.GeneratedVersion); err != nil {
			t.Fatal(err)
		}
		visualState, reason = "complete", ""
	}
	wrong := lease
	wrong.LeaseToken = "old-worker"
	if err = repos.CompleteSummaryVisualRetry(ctx, wrong, frozen, "failed", "invalid_visual_response"); err == nil {
		t.Fatal("old lease finished retry")
	}
	if err = repos.CompleteSummaryVisualRetry(ctx, lease, frozen, visualState, reason); err != nil {
		t.Fatal(err)
	}
	afterBase, _ = repos.Summary.FindByTaskID(task.ID)
	if visualState == "failed" && artifact.JSON(afterBase) != bodyBytes {
		t.Fatal("failed visual attempt rewrote body")
	}
	if visualState == "complete" {
		db.Model(&model.SummaryScreenshotRef{}).Where("generation_id=?", intent.GenerationID).Count(&screenshots)
		if screenshots != 1 || afterBase.GenerationID != intent.GenerationID || afterBase.GeneratedVersion != base.GeneratedVersion+1 {
			t.Fatal("visual version and screenshot did not publish together")
		}
		effective, err := repos.SummaryRevision.Effective(ctx, task.UserID, task.ID)
		if err != nil || effective.Content != revision.Content || effective.SourceStatus != "needs_merge" {
			t.Fatalf("user head lost %+v %v", effective, err)
		}
	}
	var keptRevision model.SummaryRevision
	var keptHead model.SummaryRevisionHead
	db.First(&keptRevision, "id=?", revision.ID)
	db.First(&keptHead, "user_id=? AND task_id=?", task.UserID, task.ID)
	if artifact.JSON(keptRevision) != revisionBytes || artifact.JSON(keptHead) != headBytes {
		t.Fatal("user revision/head bytes changed")
	}
	db.First(&afterRun, "id=?", run.ID)
	if artifact.JSON(afterRun) != parentBytes {
		t.Fatal("failed retry changed old run")
	}
}
