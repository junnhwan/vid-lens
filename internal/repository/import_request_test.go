package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

func TestImportRequestAtomicReplay(t *testing.T) { runImportReplay(t, summaryRevisionDB(t)) }
func TestPostgresImportRequestAtomicReplay(t *testing.T) {
	runImportReplay(t, openPostgresRepositoryTestDB(t).db)
}

func runImportReplay(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repos := NewRepositories(db)
	hash := processing.Fingerprint("url+options")
	count := 0
	create := func(tx *Repositories) (*model.VideoTask, error) {
		count++
		task := model.VideoTask{UserID: 7, Filename: "lesson.mp4", FileMD5: "11111111111111111111111111111111"}
		return &task, tx.Task.Create(&task)
	}
	wantErr := errors.New("dispatch failure")
	_, _, err := repos.AcceptImport(ctx, 7, "url", "request-1", hash, func(tx *Repositories) (*model.VideoTask, error) {
		_, err := create(tx)
		if err != nil {
			return nil, err
		}
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("rollback=%v", err)
	}
	var rows int64
	db.Model(&model.VideoTask{}).Count(&rows)
	if rows != 0 {
		t.Fatalf("task leaked=%d", rows)
	}
	first, created, err := repos.AcceptImport(ctx, 7, "url", "request-1", hash, create)
	if err != nil || !created {
		t.Fatalf("accept=%v created=%v", err, created)
	}
	second, created, err := repos.AcceptImport(ctx, 7, "url", "request-1", hash, create)
	if err != nil || created || second.ID != first.ID || count != 2 {
		t.Fatalf("replay=%v created=%v count=%d", err, created, count)
	}
	if _, _, err = repos.AcceptImport(ctx, 7, "url", "request-1", processing.Fingerprint("changed"), create); err == nil {
		t.Fatal("same key different request accepted")
	}
	if prior, err := repos.ImportRequest.Lookup(ctx, 8, "url", "request-1", hash); err != nil || prior != nil {
		t.Fatalf("owner isolation=%v %v", prior, err)
	}
	if err := repos.Task.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repos.AcceptImport(ctx, 7, "url", "request-1", hash, create); err == nil {
		t.Fatal("deleted task resurrected")
	}
}

func TestPostgresImportRequestConcurrentAcceptance(t *testing.T) {
	db := openPostgresRepositoryTestDB(t).db
	repos := NewRepositories(db)
	ctx := context.Background()
	hash := processing.Fingerprint("stable request")
	var wg sync.WaitGroup
	start := make(chan struct{})
	ids := make(chan int64, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			task, _, err := repos.AcceptImport(ctx, 7, "url", "concurrent-1", hash, func(tx *Repositories) (*model.VideoTask, error) {
				task := model.VideoTask{UserID: 7, Filename: "lesson.mp4", FileMD5: "11111111111111111111111111111111"}
				return &task, tx.Task.Create(&task)
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- task.ID
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var first int64
	for id := range ids {
		if first == 0 {
			first = id
		}
		if id != first {
			t.Fatalf("duplicate tasks %d/%d", first, id)
		}
	}
	var count int64
	db.Model(&model.VideoTask{}).Count(&count)
	if count != 1 {
		t.Fatalf("tasks=%d", count)
	}
}

func TestImportRequestHistoricalReceiptDoesNotInventGeneration(t *testing.T) {
	db := summaryRevisionDB(t)
	repos := NewRepositories(db)
	ctx := context.Background()
	task, _, err := repos.AcceptImport(ctx, 7, "url", "legacy-receipt", processing.Fingerprint("legacy"), func(tx *Repositories) (*model.VideoTask, error) {
		task := &model.VideoTask{UserID: 7}
		return task, tx.Task.Create(task)
	})
	if err != nil {
		t.Fatal(err)
	}
	// A newly populated task intent cannot retroactively identify a historical
	// request that originally had no captured generation identity.
	if err = db.Model(task).Update("processing_intent_json", `{"generation_id":"new-workflow"}`).Error; err != nil {
		t.Fatal(err)
	}
	generation, err := repos.ImportRequest.ReadAcceptedGeneration(ctx, 7, "url", "legacy-receipt")
	if err != nil || generation != "" {
		t.Fatalf("legacy generation invented %q %v", generation, err)
	}
	if _, err = repos.ImportRequest.ReadAcceptedGeneration(ctx, 8, "url", "legacy-receipt"); err == nil {
		t.Fatal("receipt read crossed owner")
	}
}

func TestImportRequestInitialGenerationIsImmutable(t *testing.T) {
	runImportInitialGeneration(t, summaryRevisionDB(t))
}
func TestPostgresImportRequestInitialGenerationIsImmutable(t *testing.T) {
	runImportInitialGeneration(t, openPostgresRepositoryTestDB(t).db)
}
func runImportInitialGeneration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repos := NewRepositories(db)
	options, _ := processing.Normalize(processing.Options{AutoSummary: true}, false)
	intent := processing.Intent{ID: "accepted-intent", Version: 1, GenerationID: "initial-generation", Options: options, RecipeVersion: processing.Recipe}
	task, created, err := repos.AcceptImport(ctx, 7, "url", "stable-generation", processing.Fingerprint("input"), func(tx *Repositories) (*model.VideoTask, error) {
		task := &model.VideoTask{UserID: 7, ProcessingIntentJSON: artifact.JSON(intent)}
		return task, tx.Task.Create(task)
	})
	if err != nil || !created {
		t.Fatalf("accept %t %v", created, err)
	}
	intent.GenerationID = "explicit-later-generation"
	if err = db.Model(task).Update("processing_intent_json", artifact.JSON(intent)).Error; err != nil {
		t.Fatal(err)
	}
	generation, err := repos.ImportRequest.ReadAcceptedGeneration(ctx, 7, "url", "stable-generation")
	if err != nil || generation != "initial-generation" {
		t.Fatalf("generation=%q %v", generation, err)
	}
	replay, created, err := repos.AcceptImport(ctx, 7, "url", "stable-generation", processing.Fingerprint("input"), func(*Repositories) (*model.VideoTask, error) { t.Fatal("replay invoked callback"); return nil, nil })
	if err != nil || created || replay.ID != task.ID {
		t.Fatal("receipt replay created work")
	}
}
