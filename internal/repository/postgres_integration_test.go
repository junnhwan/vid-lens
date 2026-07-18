package repository

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

const postgresIntegrationDSNEnv = "VIDLENS_POSTGRES_INTEGRATION_DSN"

type postgresRepositoryTestDB struct {
	db        *gorm.DB
	scopedDSN string
}

func openPostgresRepositoryTestDB(t *testing.T) *postgresRepositoryTestDB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(postgresIntegrationDSNEnv))
	if dsn == "" {
		t.Skip(postgresIntegrationDSNEnv + " is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL integration DSN: %v", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		t.Fatalf("integration DSN scheme = %q, want postgres or postgresql", parsed.Scheme)
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if databaseName == "" || strings.Contains(databaseName, "/") {
		t.Fatalf("integration DSN must name exactly one database, path = %q", parsed.Path)
	}
	switch strings.ToLower(databaseName) {
	case "postgres", "template0", "template1":
		t.Fatalf("refusing to run repository integration tests in administrative database %q", databaseName)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL integration database: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatalf("get PostgreSQL integration pool: %v", err)
	}
	t.Cleanup(func() { _ = adminSQL.Close() })

	var connectedDatabase string
	if err := admin.Raw("SELECT current_database()").Scan(&connectedDatabase).Error; err != nil {
		t.Fatalf("read PostgreSQL integration database name: %v", err)
	}
	if connectedDatabase != databaseName {
		t.Fatalf("connected database = %q, DSN database = %q", connectedDatabase, databaseName)
	}

	schemaName := fmt.Sprintf("vidlens_repository_test_%d", time.Now().UnixNano())
	quotedSchema := `"` + schemaName + `"`
	if err := admin.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatalf("create PostgreSQL integration schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error; err != nil {
			t.Errorf("drop PostgreSQL integration schema: %v", err)
		}
	})

	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	scopedDSN := parsed.String()
	db := openPostgresRepositoryPeer(t, scopedDSN)
	if err := model.Migrate(db); err != nil {
		t.Fatalf("migrate PostgreSQL repository schema: %v", err)
	}

	return &postgresRepositoryTestDB{db: db, scopedDSN: scopedDSN}
}

func openPostgresRepositoryPeer(t *testing.T, dsn string) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open scoped PostgreSQL integration connection: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get scoped PostgreSQL integration pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestPostgresForUpdateBlocksConcurrentTransaction(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	peer := openPostgresRepositoryPeer(t, testDB.scopedDSN)

	asset := &model.VideoAsset{
		FileMD5:        "11111111111111111111111111111111",
		ObjectName:     "postgres-lock.mp4",
		LifecycleState: model.AssetLifecycleActive,
	}
	if err := testDB.db.Create(asset).Error; err != nil {
		t.Fatalf("create lock fixture: %v", err)
	}

	firstTx := testDB.db.Begin()
	if firstTx.Error != nil {
		t.Fatalf("begin first transaction: %v", firstTx.Error)
	}
	firstDone := false
	t.Cleanup(func() {
		if !firstDone {
			_ = firstTx.Rollback().Error
		}
	})
	if _, err := NewAssetRepository(firstTx).FindByIDForUpdateUnscoped(asset.ID); err != nil {
		t.Fatalf("first FOR UPDATE: %v", err)
	}

	queryStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondTx := peer.Begin()
		if secondTx.Error != nil {
			secondDone <- secondTx.Error
			return
		}
		defer secondTx.Rollback()
		close(queryStarted)
		_, err := NewAssetRepository(secondTx).FindByIDForUpdateUnscoped(asset.ID)
		secondDone <- err
	}()
	<-queryStarted

	select {
	case err := <-secondDone:
		t.Fatalf("second FOR UPDATE completed before the first lock was released: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	if err := firstTx.Rollback().Error; err != nil {
		t.Fatalf("release first FOR UPDATE lock: %v", err)
	}
	firstDone = true
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second FOR UPDATE after lock release: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second FOR UPDATE remained blocked after the first lock was released")
	}
}

func TestPostgresLeaseCompetitionHasSingleClaimant(t *testing.T) {
	t.Run("processing", func(t *testing.T) {
		testDB := openPostgresRepositoryTestDB(t)
		peer := openPostgresRepositoryPeer(t, testDB.scopedDSN)
		now := time.Now().UTC().Truncate(time.Millisecond)
		task := &model.VideoTask{
			UserID: 1, FileMD5: "22222222222222222222222222222222", Filename: "processing.mp4",
			Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, MaxRetries: 3,
		}
		if err := testDB.db.Create(task).Error; err != nil {
			t.Fatalf("create processing fixture: %v", err)
		}

		start := make(chan struct{})
		results := make(chan TaskLeaseClaim, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i, db := range []*gorm.DB{testDB.db, peer} {
			wg.Add(1)
			go func(i int, db *gorm.DB) {
				defer wg.Done()
				<-start
				claim, err := NewRepositories(db).ClaimTaskProcessing(TaskProcessingClaimRequest{
					TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
					Now: now, LeaseUntil: now.Add(time.Minute), NewToken: fmt.Sprintf("processing-%d", i),
				})
				results <- claim
				errs <- err
			}(i, db)
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("processing lease competition: %v", err)
			}
		}
		acquired := 0
		for claim := range results {
			if claim.Outcome == TaskLeaseAcquired {
				acquired++
			}
		}
		if acquired != 1 {
			t.Fatalf("processing lease acquired by %d claimants, want 1", acquired)
		}
	})

	t.Run("dispatch", func(t *testing.T) {
		testDB := openPostgresRepositoryTestDB(t)
		peer := openPostgresRepositoryPeer(t, testDB.scopedDSN)
		now := time.Now().UTC().Truncate(time.Millisecond)
		due := now.Add(-time.Second)
		task := &model.VideoTask{
			UserID: 2, FileMD5: "33333333333333333333333333333333", Filename: "dispatch.mp4",
			Status: model.TaskStatusFailed, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
			NextRetryAt: &due, RetryCount: 1, MaxRetries: 3,
		}
		if err := testDB.db.Create(task).Error; err != nil {
			t.Fatalf("create dispatch task fixture: %v", err)
		}
		job := &model.TaskJob{
			TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeTranscribe,
			Status: model.TaskStatusFailed, Stage: model.TaskStageTranscribing,
			NextRetryAt: &due, RetryCount: 1, MaxRetries: 3,
		}
		if err := testDB.db.Create(job).Error; err != nil {
			t.Fatalf("create dispatch job fixture: %v", err)
		}

		start := make(chan struct{})
		claimed := make(chan bool, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i, db := range []*gorm.DB{testDB.db, peer} {
			wg.Add(1)
			go func(i int, db *gorm.DB) {
				defer wg.Done()
				<-start
				ok, err := NewRepositories(db).ClaimRetryDispatch(TaskDispatchClaimRequest{
					TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
					ExpectedVersion: 0, Now: now, LeaseUntil: now.Add(time.Minute), Token: fmt.Sprintf("dispatch-%d", i),
				})
				claimed <- ok
				errs <- err
			}(i, db)
		}
		close(start)
		wg.Wait()
		close(claimed)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("dispatch lease competition: %v", err)
			}
		}
		count := 0
		for ok := range claimed {
			if ok {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("dispatch lease acquired by %d claimants, want 1", count)
		}
	})

	t.Run("cleanup", func(t *testing.T) {
		testDB := openPostgresRepositoryTestDB(t)
		peer := openPostgresRepositoryPeer(t, testDB.scopedDSN)
		now := time.Now().UTC().Truncate(time.Millisecond)
		job := &model.TaskCleanupJob{
			TaskID: 10, UserID: 3, FileMD5: "44444444444444444444444444444444",
			Status: model.TaskCleanupStatusPending,
		}
		if err := testDB.db.Create(job).Error; err != nil {
			t.Fatalf("create cleanup fixture: %v", err)
		}

		start := make(chan struct{})
		claimed := make(chan bool, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i, db := range []*gorm.DB{testDB.db, peer} {
			wg.Add(1)
			go func(i int, db *gorm.DB) {
				defer wg.Done()
				<-start
				ok, err := NewTaskCleanupJobRepository(db).Claim(TaskCleanupClaimRequest{
					JobID: job.ID, Token: fmt.Sprintf("cleanup-%d", i), Now: now, LeaseUntil: now.Add(time.Minute),
				})
				claimed <- ok
				errs <- err
			}(i, db)
		}
		close(start)
		wg.Wait()
		close(claimed)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("cleanup lease competition: %v", err)
			}
		}
		count := 0
		for ok := range claimed {
			if ok {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("cleanup lease acquired by %d claimants, want 1", count)
		}
	})
}

func TestPostgresRetryBudgetIsAtomicAndAttemptUnique(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	repos := NewRepositories(testDB.db)
	if _, err := repos.RetryBudget.Ensure(RetryBudgetSpec{
		BudgetID: "postgres-budget", Operation: "summary", MaxAttempts: 4,
		Deadline: now.Add(time.Hour), Now: now,
	}); err != nil {
		t.Fatalf("ensure retry budget: %v", err)
	}

	consumeConcurrently := func(attemptKeys []string) []model.RetryBudgetDecision {
		t.Helper()
		start := make(chan struct{})
		decisions := make(chan model.RetryBudgetDecision, len(attemptKeys))
		errs := make(chan error, len(attemptKeys))
		var wg sync.WaitGroup
		for _, attemptKey := range attemptKeys {
			wg.Add(1)
			go func(attemptKey string) {
				defer wg.Done()
				<-start
				decision, err := NewRetryBudgetRepository(testDB.db).Consume(
					"postgres-budget", attemptKey, model.RetryAttemptLayerProvider, now,
				)
				decisions <- decision
				errs <- err
			}(attemptKey)
		}
		close(start)
		wg.Wait()
		close(decisions)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("consume retry budget concurrently: %v", err)
			}
		}
		result := make([]model.RetryBudgetDecision, 0, len(attemptKeys))
		for decision := range decisions {
			result = append(result, decision)
		}
		return result
	}

	sharedDecisions := consumeConcurrently([]string{"shared-attempt", "shared-attempt", "shared-attempt", "shared-attempt"})
	newSharedAttempts := 0
	for _, decision := range sharedDecisions {
		if !decision.Allowed {
			t.Fatalf("same-key retry replay was rejected: %+v", decision)
		}
		if !decision.Duplicate {
			newSharedAttempts++
		}
	}
	if newSharedAttempts != 1 {
		t.Fatalf("same-key retry created %d new attempts, want 1", newSharedAttempts)
	}

	distinctKeys := make([]string, 10)
	for i := range distinctKeys {
		distinctKeys[i] = fmt.Sprintf("attempt-%02d", i)
	}
	distinctDecisions := consumeConcurrently(distinctKeys)
	newDistinctAttempts := 0
	for _, decision := range distinctDecisions {
		if decision.Allowed && !decision.Duplicate {
			newDistinctAttempts++
		}
	}
	if newDistinctAttempts != 3 {
		t.Fatalf("new distinct retry attempts allowed = %d, want 3 after one shared attempt", newDistinctAttempts)
	}
	var attemptCount int64
	if err := testDB.db.Model(&model.AIRetryAttempt{}).Where("budget_id = ?", "postgres-budget").Count(&attemptCount).Error; err != nil {
		t.Fatalf("count retry attempts: %v", err)
	}
	if attemptCount != 4 {
		t.Fatalf("persisted retry attempts = %d, want 4", attemptCount)
	}
	var sharedCount int64
	if err := testDB.db.Model(&model.AIRetryAttempt{}).
		Where("budget_id = ? AND attempt_key = ?", "postgres-budget", "shared-attempt").
		Count(&sharedCount).Error; err != nil {
		t.Fatalf("count shared retry attempt: %v", err)
	}
	if sharedCount != 1 {
		t.Fatalf("shared retry attempt rows = %d, want 1", sharedCount)
	}
	budget, err := repos.RetryBudget.Get("postgres-budget")
	if err != nil {
		t.Fatalf("get retry budget: %v", err)
	}
	if budget.AttemptCount != 4 {
		t.Fatalf("retry budget attempt_count = %d, want 4", budget.AttemptCount)
	}
}

func TestPostgresUsageLedgerLifecycleIsIdempotent(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	repo := NewUsageLedgerRepository(testDB.db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	reservation := UsageReservation{
		IdempotencyKey: "postgres-usage-settle", UserID: 8, TaskID: 3,
		Kind: model.AICallKindLLM, Provider: "mock", Model: "m", Unit: model.UsageUnitToken,
		ReservedUnits: 100, UsageDate: "2026-07-17", ExpiresAt: now.Add(time.Hour), Now: now,
	}
	ledger, created, event, err := repo.Reserve(reservation)
	if err != nil || !created || ledger.Status != model.UsageLedgerReserved || event == nil || event.DeltaUnits != 100 {
		t.Fatalf("reserve ledger=%+v created=%v event=%+v err=%v", ledger, created, event, err)
	}
	replayed, created, event, err := repo.Reserve(reservation)
	if err != nil || created || event != nil || replayed.ID != ledger.ID {
		t.Fatalf("replay reserve ledger=%+v created=%v event=%+v err=%v", replayed, created, event, err)
	}

	prompt, completion, total := int64(40), int64(20), int64(60)
	settlement := UsageSettlement{
		ActualUnits: floatPtr(60), PromptTokens: &prompt, CompletionTokens: &completion, TotalTokens: &total,
		UsageSource: model.UsageSourceActual, ProviderRequestID: "pg-request-1", Now: now.Add(time.Minute),
	}
	settled, changed, settleEvent, err := repo.Settle(reservation.IdempotencyKey, settlement)
	if err != nil || !changed || settled.Status != model.UsageLedgerSettled || settleEvent == nil || settleEvent.DeltaUnits != -40 {
		t.Fatalf("settle ledger=%+v changed=%v event=%+v err=%v", settled, changed, settleEvent, err)
	}
	settlement.Now = now.Add(2 * time.Minute)
	replayedSettle, changed, settleEvent, err := repo.Settle(reservation.IdempotencyKey, settlement)
	if err != nil || changed || settleEvent != nil || replayedSettle.Status != model.UsageLedgerSettled {
		t.Fatalf("replay settle ledger=%+v changed=%v event=%+v err=%v", replayedSettle, changed, settleEvent, err)
	}

	reservation.IdempotencyKey = "postgres-usage-release"
	reservation.ReservedUnits = 30
	if _, created, _, err := repo.Reserve(reservation); err != nil || !created {
		t.Fatalf("reserve release fixture created=%v err=%v", created, err)
	}
	released, changed, releaseEvent, err := repo.Release(reservation.IdempotencyKey, "provider failed", now.Add(time.Minute))
	if err != nil || !changed || released.Status != model.UsageLedgerReleased || releaseEvent == nil || releaseEvent.DeltaUnits != -30 {
		t.Fatalf("release ledger=%+v changed=%v event=%+v err=%v", released, changed, releaseEvent, err)
	}
	replayedRelease, changed, releaseEvent, err := repo.Release(reservation.IdempotencyKey, "provider failed", now.Add(2*time.Minute))
	if err != nil || changed || releaseEvent != nil || replayedRelease.Status != model.UsageLedgerReleased {
		t.Fatalf("replay release ledger=%+v changed=%v event=%+v err=%v", replayedRelease, changed, releaseEvent, err)
	}
}

func TestPostgresRepositoryUpsertsUseDeclaredUniqueKeys(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	repos := NewRepositories(testDB.db)
	now := time.Now().UTC().Truncate(time.Millisecond)

	failure := &model.KafkaMessageFailure{
		ConsumerGroup: "postgres-group", ConsumerName: "transcribe", Topic: "video.transcribe",
		Partition: 2, MessageOffset: 41, MessageKey: []byte("key"), Payload: []byte("bad"),
		ErrorMessage: "invalid payload", CreatedAt: now, UpdatedAt: now,
	}
	if err := repos.TaskMessageFailure.Record(failure); err != nil {
		t.Fatalf("record Kafka failure: %v", err)
	}
	failure.ConsumerName = "transcribe-retry"
	failure.ErrorMessage = "same offset retried"
	failure.UpdatedAt = now.Add(time.Minute)
	if err := repos.TaskMessageFailure.Record(failure); err != nil {
		t.Fatalf("upsert Kafka failure: %v", err)
	}
	var failures []model.KafkaMessageFailure
	if err := testDB.db.Find(&failures).Error; err != nil {
		t.Fatalf("query Kafka failures: %v", err)
	}
	if len(failures) != 1 || failures[0].ErrorMessage != "same offset retried" || failures[0].ConsumerName != "transcribe-retry" {
		t.Fatalf("Kafka failure upsert rows = %+v", failures)
	}

	if err := repos.AICallLog.IncrementDailyUsage(9, "2026-07-17", model.AICallKindASR, model.AICallStatusSuccess, 100, 20, 15); err != nil {
		t.Fatalf("insert daily usage: %v", err)
	}
	if err := repos.AICallLog.IncrementDailyUsage(9, "2026-07-17", model.AICallKindASR, model.AICallStatusFailed, 40, 5, 7); err != nil {
		t.Fatalf("upsert daily usage: %v", err)
	}
	usage, err := repos.AICallLog.FindDailyUsage(9, "2026-07-17")
	if err != nil {
		t.Fatalf("find daily usage: %v", err)
	}
	if usage == nil || usage.ASRRequests != 2 || usage.ASRSeconds != 22 || usage.FailedRequests != 1 || usage.InputChars != 140 || usage.OutputChars != 25 {
		t.Fatalf("daily usage upsert = %+v", usage)
	}
	var usageRows int64
	if err := testDB.db.Model(&model.UserUsageDaily{}).Count(&usageRows).Error; err != nil {
		t.Fatalf("count daily usage rows: %v", err)
	}
	if usageRows != 1 {
		t.Fatalf("daily usage rows = %d, want 1", usageRows)
	}
}

func TestPostgresInitialDispatchPersistsTaskJobBudgetAndLeaseAtomically(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	repos := NewRepositories(testDB.db)
	now := time.Date(2026, 7, 17, 9, 0, 0, 123456000, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)
	task := &model.VideoTask{
		UserID: 23, FileMD5: "abababababababababababababababab", Filename: "initial-dispatch.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: "trace-postgres-initial", MaxRetries: 4,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	prepared, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: leaseUntil, Token: "initial-dispatch-token",
	})
	if err != nil {
		t.Fatalf("prepare initial dispatch: %v", err)
	}

	storedTask, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if err != nil {
		t.Fatalf("find task job: %v", err)
	}
	budget, err := repos.RetryBudget.Get(prepared.RetryBudgetID)
	if err != nil {
		t.Fatalf("find retry budget: %v", err)
	}

	if storedTask.Status != model.TaskStatusQueued || storedTask.Stage != model.TaskStageTranscribing || storedTask.LastJobType != model.TaskJobTypeTranscribe {
		t.Fatalf("stored task state = status:%d stage:%q job:%q", storedTask.Status, storedTask.Stage, storedTask.LastJobType)
	}
	if storedTask.ProcessingToken != prepared.Token || storedTask.LeaseKind != model.TaskLeaseKindDispatch || storedTask.LeaseExpiresAt == nil || !storedTask.LeaseExpiresAt.Equal(leaseUntil) || storedTask.LeaseVersion != 1 {
		t.Fatalf("stored task lease = token:%q kind:%q expires:%v version:%d", storedTask.ProcessingToken, storedTask.LeaseKind, storedTask.LeaseExpiresAt, storedTask.LeaseVersion)
	}
	if job == nil || job.Status != model.TaskStatusQueued || job.ProcessingToken != prepared.Token || job.LeaseKind != model.TaskLeaseKindDispatch || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.Equal(leaseUntil) || job.LeaseVersion != storedTask.LeaseVersion {
		t.Fatalf("stored task job = %+v", job)
	}
	if job.RetryBudgetID != prepared.RetryBudgetID || budget.TaskID != task.ID || budget.JobID != job.ID || budget.Operation != model.TaskJobTypeTranscribe || budget.MaxAttempts != 4 {
		t.Fatalf("retry budget correlation = prepared:%q job:%+v budget:%+v", prepared.RetryBudgetID, job, budget)
	}
	if task.Status != storedTask.Status || task.ProcessingToken != storedTask.ProcessingToken || task.LeaseVersion != storedTask.LeaseVersion {
		t.Fatalf("caller task not synchronized after commit: caller=%+v stored=%+v", task, storedTask)
	}
}

func TestPostgresInitialDispatchRollsBackTaskAndJobWhenBudgetPersistenceFails(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	repos := NewRepositories(testDB.db)
	task := &model.VideoTask{
		UserID: 24, FileMD5: "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd", Filename: "rollback.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: "trace-postgres-rollback", MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := testDB.db.Exec("DROP TABLE ai_retry_budgets").Error; err != nil {
		t.Fatalf("remove retry budget table fixture: %v", err)
	}

	now := time.Date(2026, 7, 17, 9, 30, 0, 0, time.UTC)
	_, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeAnalyze, Stage: model.TaskStageSummarizing,
		Now: now, LeaseUntil: now.Add(2 * time.Minute), Token: "rollback-token",
	})
	if err == nil {
		t.Fatal("prepare initial dispatch succeeded without retry budget table")
	}

	storedTask, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task after rollback: %v", findErr)
	}
	job, findErr := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeAnalyze)
	if findErr != nil {
		t.Fatalf("find task job after rollback: %v", findErr)
	}
	if storedTask.Status != model.TaskStatusPending || storedTask.Stage != model.TaskStageUploaded || storedTask.LastJobType != "" || storedTask.ProcessingToken != "" || storedTask.LeaseVersion != 0 {
		t.Fatalf("task update was not rolled back: %+v", storedTask)
	}
	if job != nil {
		t.Fatalf("task job insert was not rolled back: %+v", job)
	}
	if task.Status != model.TaskStatusPending || task.Stage != model.TaskStageUploaded || task.ProcessingToken != "" || task.LeaseVersion != 0 {
		t.Fatalf("caller task mutated before failed transaction committed: %+v", task)
	}
}

func TestPostgresExpiredInitialDispatchIsDueAndTokenHandoffIsCASProtected(t *testing.T) {
	testDB := openPostgresRepositoryTestDB(t)
	repos := NewRepositories(testDB.db)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)
	task := &model.VideoTask{
		UserID: 25, FileMD5: "efefefefefefefefefefefefefefefef", Filename: "handoff.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: "trace-postgres-handoff", MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	prepared, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: leaseUntil, Token: "dispatch-token",
	})
	if err != nil {
		t.Fatalf("prepare initial dispatch: %v", err)
	}

	due, err := repos.Task.FindDueRetryTasks(leaseUntil.Add(time.Microsecond), 10)
	if err != nil {
		t.Fatalf("find due retry tasks: %v", err)
	}
	found := false
	for _, candidate := range due {
		if candidate.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expired initial dispatch task %d was not visible to scheduler", task.ID)
	}

	wrong, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		MessageToken: "wrong-token", Now: now.Add(time.Second), LeaseUntil: now.Add(5 * time.Minute), NewToken: "worker-wrong",
	})
	if err != nil {
		t.Fatalf("claim with wrong token: %v", err)
	}
	if wrong.Outcome != TaskLeaseStale {
		t.Fatalf("wrong-token outcome = %q, want stale", wrong.Outcome)
	}

	claim, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		MessageToken: prepared.Token, Now: now.Add(time.Second), LeaseUntil: now.Add(5 * time.Minute), NewToken: "worker-token",
	})
	if err != nil {
		t.Fatalf("claim with dispatch token: %v", err)
	}
	if claim.Outcome != TaskLeaseAcquired || claim.Token != "worker-token" || claim.Version != prepared.Task.LeaseVersion+1 {
		t.Fatalf("processing claim = %+v", claim)
	}
	storedTask, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find claimed task: %v", err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if err != nil {
		t.Fatalf("find claimed task job: %v", err)
	}
	if storedTask.Status != model.TaskStatusRunning || storedTask.ProcessingToken != "worker-token" || storedTask.LeaseKind != model.TaskLeaseKindProcessing || storedTask.LeaseVersion != claim.Version {
		t.Fatalf("claimed task = %+v", storedTask)
	}
	if job == nil || job.Status != model.TaskStatusRunning || job.ProcessingToken != "worker-token" || job.LeaseKind != model.TaskLeaseKindProcessing || job.LeaseVersion != claim.Version {
		t.Fatalf("claimed task job = %+v", job)
	}
}
