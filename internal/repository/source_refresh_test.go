package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

func TestSourceRefreshAcceptanceAtomicAndKeepsPublishedSource(t *testing.T) {
	runSourceRefreshAcceptance(t, summaryRevisionDB(t), "")
}
func TestPostgresSourceRefreshAcceptanceAtomicAndKeepsPublishedSource(t *testing.T) {
	f := openPostgresRepositoryTestDB(t)
	runSourceRefreshAcceptance(t, f.db, f.scopedDSN)
}
func runSourceRefreshAcceptance(t *testing.T, db *gorm.DB, dsn string) {
	t.Helper()
	ctx := context.Background()
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 91, 9)
	source, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: sourceFixture(t, "仍然可读的原文。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	opts, _ := processing.Normalize(processing.Options{AutoSummary: true, TextSourcePolicy: "force_asr", OutputMode: "text"}, false)
	old := processing.Intent{ID: uuid.NewString(), Version: 1, GenerationID: uuid.NewString(), RecipeVersion: processing.Recipe, Options: opts, ProfileID: 1, ProfileFingerprint: "before-user-added-ASR"}
	db.Model(&model.VideoTask{}).Where("id=?", task.ID).Updates(map[string]any{"processing_intent_json": artifact.JSON(old), "status": model.TaskStatusFailed})
	taskPtr, _ := repos.Task.FindByID(task.ID)
	task = *taskPtr
	body := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "旧原稿", GenerationID: old.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest}
	db.Create(&body)
	chunk := model.VideoTranscriptionChunk{TaskID: task.ID, ChunkIndex: 0, Status: model.TranscriptionChunkStatusCompleted, Content: source.CanonicalText, SegmentKey: "paid-previous-key"}
	db.Create(&chunk)
	revision := model.SummaryRevision{ID: uuid.NewString(), UserID: task.UserID, TaskID: task.ID, Version: 1, Content: "用户版本", Origin: "manual"}
	db.Create(&revision)
	head := model.SummaryRevisionHead{UserID: task.UserID, TaskID: task.ID, Version: 1, CurrentRevisionID: revision.ID}
	db.Create(&head)
	db.First(&body, "id=?", body.ID)
	db.First(&chunk, "id=?", chunk.ID)
	db.First(&head, "user_id=? AND task_id=?", task.UserID, task.ID)
	db.First(&revision, "id=?", revision.ID)
	bodyBytes, chunkBytes, headBytes, revisionBytes := artifact.JSON(body), artifact.JSON(chunk), artifact.JSON(head), artifact.JSON(revision)
	next := old
	next.ID = uuid.NewString()
	next.GenerationID = uuid.NewString()
	next.ProfileFingerprint = "explicit-new-config"
	frozen := processing.SourceRefreshSnapshot{Operation: processing.OperationSourceRefresh, Intent: next, ExpectedActiveSourceID: source.ID, PreviousInputFingerprint: processing.SourceSummaryInputFingerprint(old)}
	req := PrepareSourceRefreshRequest{UserID: task.UserID, TaskID: task.ID, ExpectedSourceID: source.ID, ExpectedIntentJSON: task.ProcessingIntentJSON, MediaFingerprint: task.FileMD5, Snapshot: frozen, Token: "fresh-dispatch", Now: time.Now(), LeaseUntil: time.Now().Add(time.Minute)}
	bad := req
	bad.ExpectedSourceID = "changed"
	bad.Snapshot.ExpectedActiveSourceID = "changed"
	_, _, err = repos.AcceptImport(ctx, task.UserID, "source_refresh", "rollback", "hash-is-not-64", func(tx *Repositories) (*model.VideoTask, error) {
		out, err := tx.PrepareSourceRefresh(ctx, bad)
		return &out.Task, err
	})
	if err == nil {
		t.Fatal("invalid request accepted")
	}
	hash := processing.Fingerprint("request")
	_, _, err = repos.AcceptImport(ctx, task.UserID, "source_refresh", "rollback", hash, func(tx *Repositories) (*model.VideoTask, error) {
		out, err := tx.PrepareSourceRefresh(ctx, bad)
		return &out.Task, err
	})
	if err == nil {
		t.Fatal("stale source accepted")
	}
	var receipts int64
	db.Model(&model.ImportRequest{}).Count(&receipts)
	if receipts != 0 {
		t.Fatal("failed receipt committed")
	}
	// Failure after dispatch, budget and intent writes must roll all of them
	// back with the acceptance receipt, not leave an unowned source job.
	var beforeJobs, beforeBudgets int64
	db.Model(&model.TaskJob{}).Count(&beforeJobs)
	db.Model(&model.AIRetryBudget{}).Count(&beforeBudgets)
	_, _, err = repos.AcceptImport(ctx, task.UserID, "source_refresh", "post-write-rollback", hash, func(tx *Repositories) (*model.VideoTask, error) {
		out, err := tx.PrepareSourceRefresh(ctx, req)
		if err != nil {
			return nil, err
		}
		return &out.Task, errors.New("fixture failure after all acceptance writes")
	})
	if err == nil {
		t.Fatal("post-write failure accepted")
	}
	rolled, _ := repos.Task.FindByID(task.ID)
	var afterJobs, afterBudgets int64
	db.Model(&model.TaskJob{}).Count(&afterJobs)
	db.Model(&model.AIRetryBudget{}).Count(&afterBudgets)
	db.Model(&model.ImportRequest{}).Count(&receipts)
	if rolled.ProcessingIntentJSON != task.ProcessingIntentJSON || rolled.Status != task.Status || rolled.LeaseVersion != task.LeaseVersion || afterJobs != beforeJobs || afterBudgets != beforeBudgets || receipts != 0 {
		t.Fatal("receipt/job/budget/intent/lease did not roll back together")
	}
	var winners atomic.Int32
	start := make(chan struct{})
	errs := make(chan error, 5)
	var wg sync.WaitGroup
	count := 1
	if dsn != "" {
		count = 5
	}
	for i := 0; i < count; i++ {
		peer := repos
		if dsn != "" {
			peer = NewRepositories(openPostgresRepositoryPeer(t, dsn))
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, won, err := peer.AcceptImport(ctx, task.UserID, "source_refresh", "same-key", hash, func(tx *Repositories) (*model.VideoTask, error) {
				out, err := tx.PrepareSourceRefresh(ctx, req)
				return &out.Task, err
			})
			if won {
				winners.Add(1)
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if winners.Load() != 1 {
		t.Fatal("concurrent same-key acceptance duplicated job")
	}
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if job.GenerationID != next.GenerationID || job.InputSnapshotJSON != artifact.JSON(frozen) || job.Status != model.TaskStatusQueued {
		t.Fatal("frozen source job not atomic")
	}
	stored, _ := repos.Task.FindByID(task.ID)
	if stored.ActiveTextSourceID != source.ID || stored.ProcessingIntentJSON != artifact.JSON(next) {
		t.Fatal("source pointer or intent wrong")
	}
	if gen, err := repos.ImportRequest.ReadAcceptedGeneration(ctx, task.UserID, "source_refresh", "same-key"); err != nil || gen != next.GenerationID {
		t.Fatal("receipt generation not immutable")
	}
	db.First(&body, "id=?", body.ID)
	db.First(&chunk, "id=?", chunk.ID)
	db.First(&head, "user_id=? AND task_id=?", task.UserID, task.ID)
	db.First(&revision, "id=?", revision.ID)
	if artifact.JSON(body) != bodyBytes || artifact.JSON(chunk) != chunkBytes || artifact.JSON(head) != headBytes || artifact.JSON(revision) != revisionBytes {
		t.Fatal("acceptance modified published text/checkpoints/revision/head")
	}
	if _, err = repos.ImportRequest.Lookup(ctx, task.UserID, "source_refresh", "same-key", processing.Fingerprint("mismatch")); err == nil {
		t.Fatal("same-key mismatch accepted")
	}
}
