package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/glebarez/sqlite"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

func artifactFixture(t *testing.T, serve http.HandlerFunc) (*ArtifactService, *gorm.DB, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); serve(w, r) }))
	t.Cleanup(server.Close)
	var dialector gorm.Dialector = sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if dsn := os.Getenv("VIDLENS_ARTIFACT_TEST_DSN"); dsn != "" {
		parsed, e := url.Parse(dsn)
		if e != nil || parsed.Path != "/vidlens_artifact_test" {
			t.Fatal("artifact integration requires isolated vidlens_artifact_test database")
		}
		admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if e != nil {
			t.Fatal(e)
		}
		schema := fmt.Sprintf("artifact_test_%d", time.Now().UnixNano())
		if e = admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if e := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; e != nil {
				t.Error(e)
			}
			pool, _ := admin.DB()
			pool.Close()
		})
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dialector = postgres.Open(parsed.String())
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if db.Dialector.Name() == "postgres" {
		sqlDB.SetMaxOpenConns(8)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	profiles := NewAIProfileService(repos.AIProfile, codec, nil).WithAgentBudgetConfig(config.DefaultAgentBudgetConfig())
	p := validAIProfileRequest()
	p.LLMBaseURL = server.URL + "/v1"
	p.LLMContextTokens = 65536
	if _, err = profiles.Create(7, p); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.VideoTask{ID: 42, UserID: 7, FileMD5: "fixture", Filename: "课程", Status: model.TaskStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.VideoTranscription{TaskID: 42, FileMD5: "fixture", Content: "事务保证原子性。提交后修改生效，回滚撤销修改。"}).Error; err != nil {
		t.Fatal(err)
	}
	return NewArtifactService(repos, profiles, ai.NewFactory()), db, calls
}
func artifactModelResponse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []ai.ChatMessage `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	var input struct {
		Evidence []studyEvidence `json:"evidence"`
	}
	_ = json.Unmarshal([]byte(req.Messages[1].Content), &input)
	body := artifact.Body{SchemaVersion: 1, Kind: "study", Title: "事务", Blocks: []artifact.Block{{BlockID: "atomic", Type: "concept", Title: "原子性", Content: "提交或回滚。", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: input.Evidence[0].ID, Relation: "supports"}}}}, Warnings: []string{}}
	artifactStreamResponse(w, artifact.JSON(body), "stop")
}

func artifactStreamResponse(w http.ResponseWriter, content, finish string) {
	w.Header().Set("Content-Type", "text/event-stream")
	runes := []rune(content)
	for _, part := range []string{string(runes[:len(runes)/2]), string(runes[len(runes)/2:])} {
		fmt.Fprintf(w, "data: %s\n\n", artifact.JSON(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": part}}}}))
	}
	fmt.Fprintf(w, "data: %s\n\n", artifact.JSON(map[string]any{"choices": []any{map[string]any{"finish_reason": finish, "delta": map[string]any{}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 50}}))
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func artifactRequest() artifact.GenerationRequest {
	return artifact.GenerationRequest{Kind: "study", Scope: "video", SourceIDs: []int64{42}, Goal: "学习事务"}
}
func requireArtifactCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *artifact.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error=%v want %s", err, code)
	}
}

func TestArtifactHTTPModelPersistenceEditingEvidenceAndRetry(t *testing.T) {
	svc, db, calls := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "create", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	same, err := svc.Submit(ctx, 7, "create", artifactRequest(), nil)
	if err != nil || same.ID != run.ID {
		t.Fatalf("idempotency %v %v", same, err)
	}
	changed := artifactRequest()
	changed.Goal = "changed"
	_, err = svc.Submit(ctx, 7, "create", changed, nil)
	requireArtifactCode(t, err, "idempotency_conflict")
	_, err = svc.Run(ctx, 8, run.ID)
	requireArtifactCode(t, err, "not_found")
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate external calls %d", calls.Load())
	}
	final, err := svc.Run(ctx, 7, run.ID)
	if err != nil || final.Status != "completed" || final.Usage.TokenSource != "actual" || final.Usage.PromptTokens != 100 {
		t.Fatalf("run %+v err %v", final, err)
	}
	detail, err := svc.Get(ctx, 7, run.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.HeadVersion != 1 {
		t.Fatal(detail)
	}
	evidence, err := svc.Evidence(ctx, 7, detail.Version.ManifestID, detail.Version.Body.Blocks[0].EvidenceRefs[0].EvidenceID)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.TimeRangeStatus != "unknown" || evidence.StartMS != nil {
		t.Fatalf("invented timestamp %+v", evidence)
	}
	body := detail.Version.Body
	body.Blocks[0].Content = "我的学习笔记"
	saved, err := svc.Save(ctx, 7, run.ArtifactID, 1, &body, "")
	if err != nil {
		t.Fatal(err)
	}
	if saved.HeadVersion != 2 || saved.Version.Body.Blocks[0].ClaimOrigin != "user" {
		t.Fatal(saved)
	}
	_, err = svc.Save(ctx, 7, run.ArtifactID, 1, &body, "")
	requireArtifactCode(t, err, "version_conflict")
	old, err := svc.Version(ctx, 7, run.ArtifactID, detail.Version.ID)
	if err != nil || old.Body.Blocks[0].Content != "提交或回滚。" {
		t.Fatalf("old revision mutated %v %v", old, err)
	}
	retry, err := svc.Retry(ctx, 7, run.ID, "retry")
	if err != nil {
		t.Fatal(err)
	}
	if retry.ParentRunID == nil || *retry.ParentRunID != run.ID {
		t.Fatal(retry)
	}
	if err = svc.ExecuteArtifact(ctx, retry.ID); err != nil {
		t.Fatal(err)
	}
	again, err := svc.Retry(ctx, 7, run.ID, "retry")
	if err != nil || again.ID != retry.ID {
		t.Fatalf("retry idempotency %v %v", again, err)
	}
	var nullSession int64
	if err = db.Raw("SELECT COUNT(*) FROM agent_runs WHERE subject_kind='generation_request' AND session_id IS NULL").Scan(&nullSession).Error; err != nil || nullSession != 2 {
		t.Fatalf("session decoupling %d %v", nullSession, err)
	}
	events, err := svc.Events(ctx, 7, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatal(events)
		}
	}
}

func TestArtifactCandidatePreservesConcurrentEdit(t *testing.T) {
	svc, _, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	first, err := svc.Submit(ctx, 7, "first", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	req := artifactRequest()
	req.ArtifactID = &first.ArtifactID
	req.BaseVersion = 1
	run, err := svc.Submit(ctx, 7, "regenerate", req, nil)
	if err != nil {
		t.Fatal(err)
	}
	detail, _ := svc.Get(ctx, 7, first.ArtifactID)
	body := detail.Version.Body
	body.Blocks[0].Content = "保留人工修改"
	if _, err = svc.Save(ctx, 7, first.ArtifactID, 1, &body, ""); err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	result, _ := svc.Run(ctx, 7, run.ID)
	if result.Result == nil || !result.Result.IsCandidate {
		t.Fatal(result)
	}
	head, _ := svc.Get(ctx, 7, first.ArtifactID)
	if head.HeadVersion != 2 || head.Version.Body.Blocks[0].Content != "保留人工修改" {
		t.Fatal(head)
	}
	adopted, err := svc.Save(ctx, 7, first.ArtifactID, 2, nil, result.Result.VersionID)
	if err != nil || adopted.HeadVersion != 4 {
		t.Fatalf("adopt %v %v", adopted, err)
	}
}

func TestArtifactLeaseFencingRecoveryAndLateUsage(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "lease", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.repos.Artifact.Claim(ctx, run.ID, "old", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	call, err := svc.repos.Artifact.BeginCall(ctx, run.ID, "old", a.RunLeaseEpoch, "segment", artifact.Hash("input"), 200, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("run_lease_until", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err = svc.repos.Artifact.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := svc.repos.Artifact.Claim(ctx, run.ID, "new", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !a.ExecutionStartedAt.Equal(*b.ExecutionStartedAt) || b.LLMCallsUsed != 1 {
		t.Fatal("budget/start reset on recovery")
	}
	err = svc.repos.Artifact.Checkpoint(ctx, run.ID, "old", a.RunLeaseEpoch, call, "{}")
	if !errors.Is(err, artifact.ErrLease) {
		t.Fatalf("stale checkpoint %v", err)
	}
	err = svc.repos.Artifact.Finish(ctx, run.ID, "old", a.RunLeaseEpoch, "completed", "")
	if !errors.Is(err, artifact.ErrLease) {
		t.Fatal("stale terminal accepted")
	}
	if err = svc.repos.Artifact.Finish(ctx, run.ID, "new", b.RunLeaseEpoch, "cancelled", ""); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = svc.repos.Artifact.SettleCall(ctx, call.Call.ID, 125, 25, true); err != nil {
			t.Fatal(err)
		}
	}
	final, err := svc.Run(ctx, 7, run.ID)
	if err != nil || final.Status != "cancelled" || final.Usage.PromptTokens != 125 || final.Usage.CompletionTokens != 25 {
		t.Fatalf("late usage %v %v", final, err)
	}
	var step model.AgentStep
	db.First(&step, "id=?", call.Step.ID)
	if step.Status != "ambiguous" {
		t.Fatal(step.Status)
	}
}

func TestArtifactCancelAndSourceDeletionBlockLateCommits(t *testing.T) {
	svc, db, calls := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "cancel", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Cancel(ctx, 7, run.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("cancelled run called provider")
	}
	run, err = svc.Submit(ctx, 7, "delete", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	detail, _ := svc.Get(ctx, 7, run.ArtifactID)
	evidenceID := detail.Version.Body.Blocks[0].EvidenceRefs[0].EvidenceID
	if err = db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Delete(&model.VideoTask{}, 42).Error; e != nil {
			return e
		}
		return repository.NewRepositories(tx).Artifact.RevokeSource(42)
	}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Get(ctx, 7, run.ArtifactID)
	requireArtifactCode(t, err, "source_deleted")
	_, err = svc.Evidence(ctx, 7, detail.Version.ManifestID, evidenceID)
	requireArtifactCode(t, err, "source_deleted")
	var retained int64
	db.Model(&model.SourceSnapshotItem{}).Where("content<>''").Count(&retained)
	if retained != 0 {
		t.Fatal("deleted snapshot retained")
	}
}

func TestArtifactConcurrentSubmissionSingleRun(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := svc.Submit(context.Background(), 7, "same", artifactRequest(), nil)
			if e != nil {
				errs <- e
				return
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatal(unique)
	}
	var count int64
	db.Model(&model.GenerationRequest{}).Count(&count)
	if count != 1 {
		t.Fatal(count)
	}
}

func waitArtifact(t *testing.T, svc *ArtifactService, id, status string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		run, err := svc.Run(context.Background(), 7, id)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == status {
			return
		}
		if run.Status == "failed" || run.Status == "budget_exhausted" {
			t.Fatalf("unexpected terminal %+v", run)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("run did not reach %s", status)
}
func TestArtifactRabbitMQOutboxDuplicateAndWorkerRestart(t *testing.T) {
	broker := os.Getenv("VIDLENS_ARTIFACT_TEST_AMQP")
	if broker == "" || os.Getenv("VIDLENS_ARTIFACT_TEST_DSN") == "" {
		t.Skip("isolated PostgreSQL and RabbitMQ integration variables not set")
	}
	var requests atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	svc, db, calls := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
			select {
			case <-r.Context().Done():
				return
			case <-release:
				return
			}
		}
		artifactModelResponse(w, r)
	})
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "broker-restart", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Submission commits while no consumer exists. The outbox is the durable intent.
	var intents int64
	db.Model(&model.GenerationDispatch{}).Where("run_id=?", run.ID).Count(&intents)
	if intents != 1 {
		t.Fatal(intents)
	}
	workerCtx, stop := context.WithCancel(ctx)
	worker := mq.NewArtifactWorker(svc.repos.Artifact, svc, []string{broker})
	worker.Start(workerCtx)
	select {
	case <-started:
	case <-time.After(15 * time.Second):
		stop()
		worker.Wait()
		t.Fatal("provider not reached")
	}
	stop()
	worker.Wait()
	close(release)
	interrupted, err := svc.Run(ctx, 7, run.ID)
	if err != nil || interrupted.Status != "running" {
		t.Fatalf("shutdown incorrectly terminalized %+v %v", interrupted, err)
	}
	db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("run_lease_until", time.Now().Add(-time.Second))
	// Expire recent dispatch suppression to simulate elapsed recovery time.
	db.Model(&model.GenerationDispatch{}).Where("run_id=?", run.ID).Update("created_at", time.Now().Add(-time.Minute))
	restartCtx, restartStop := context.WithCancel(ctx)
	restart := mq.NewArtifactWorker(svc.repos.Artifact, svc, []string{broker})
	restart.Start(restartCtx)
	defer func() { restartStop(); restart.Wait() }()
	waitArtifact(t, svc, run.ID, "completed")
	conn, err := amqp.Dial(broker)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	raw, _ := json.Marshal(mq.ArtifactDispatch{SchemaVersion: 1, RunID: run.ID, DispatchID: "duplicate", TraceID: run.ID})
	if err = ch.PublishWithContext(ctx, "", mq.ArtifactQueue, true, false, amqp.Publishing{DeliveryMode: amqp.Persistent, Body: raw}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if calls.Load() != 2 {
		t.Fatalf("model calls %d want interrupted + recovered", calls.Load())
	}
	var versions int64
	db.Model(&model.ArtifactVersion{}).Where("run_id=?", run.ID).Count(&versions)
	if versions != 1 {
		t.Fatal(versions)
	}
	var ambiguous int64
	db.Model(&model.AgentStep{}).Where("run_id=? AND status='ambiguous'", run.ID).Count(&ambiguous)
	if ambiguous != 1 {
		t.Fatal("missing ambiguous attempt")
	}
}

func TestArtifactSourceChangeCannotCommitAndOutdatedRetainsSnapshot(t *testing.T) {
	var change func()
	svc, db, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if change != nil {
			change()
		}
		artifactModelResponse(w, r)
	})
	ctx := context.Background()
	first, err := svc.Submit(ctx, 7, "old", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	old, _ := svc.Get(ctx, 7, first.ArtifactID)
	run, err := svc.Submit(ctx, 7, "changed", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	change = func() {
		if e := db.Model(&model.VideoTranscription{}).Where("task_id=42").Update("content", "新的资料内容").Error; e != nil {
			t.Error(e)
		}
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	result, _ := svc.Run(ctx, 7, run.ID)
	if result.Status != "failed" || result.ErrorCode == nil || *result.ErrorCode != "source_changed" {
		t.Fatal(result)
	}
	detail, err := svc.Get(ctx, 7, first.ArtifactID)
	if err != nil || detail.Version.SourceStatus != "outdated" {
		t.Fatalf("outdated %v %v", detail, err)
	}
	evidence, err := svc.Evidence(ctx, 7, old.Version.ManifestID, old.Version.Body.Blocks[0].EvidenceRefs[0].EvidenceID)
	if err != nil || evidence.Content == "新的资料内容" {
		t.Fatalf("snapshot rebound %v %v", evidence, err)
	}
}

func TestArtifactPrecisionAndBoundedProviderRepair(t *testing.T) {
	var count atomic.Int32
	svc, db, calls := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch count.Add(1) {
		case 1:
			w.WriteHeader(503)
			return
		case 2:
			artifactStreamResponse(w, "{}", "stop")
			return
		default:
			artifactModelResponse(w, r)
		}
	})
	if err := db.Create(&model.VideoVisualFrame{TaskID: 42, FrameIndex: 0, TimeMs: 12000, OCRText: "事务", Status: model.VisualFrameStatusCompleted, Source: "interval"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	source, err := svc.Source(ctx, 7, 42)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range source.Evidence {
		if e.Modality == model.ChunkModalityVisualOCR {
			found = true
			if e.TimeRangeStatus != "precise" || e.StartMS == nil || *e.StartMS != 12000 {
				t.Fatalf("precision %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("missing visual evidence")
	}
	run, err := svc.Submit(ctx, 7, "repair", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(ctx, 7, run.ID)
	if err != nil || result.Status != "completed" || calls.Load() != 3 {
		t.Fatalf("bounded repair %+v calls=%d error=%v", result, calls.Load(), err)
	}
}

func TestArtifactCancellationWinsBeforeResultCommit(t *testing.T) {
	var cancelRun func()
	svc, db, _ := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) { cancelRun(); artifactModelResponse(w, r) })
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "cancel-race", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelRun = func() {
		if _, e := svc.Cancel(ctx, 7, run.ID); e != nil {
			t.Error(e)
		}
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, run.ID)
	if err != nil || final.Status != "cancelled" || final.Usage.PromptTokens != 100 {
		t.Fatalf("cancel race %+v %v", final, err)
	}
	var versions int64
	db.Model(&model.ArtifactVersion{}).Where("run_id=?", run.ID).Count(&versions)
	if versions != 0 {
		t.Fatal("cancel created a version")
	}
}

func TestArtifactRestartReusesCompletedSegmentCheckpoint(t *testing.T) {
	second := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	svc, db, calls := artifactFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 2 {
			close(second)
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		artifactModelResponse(w, r)
	})
	if err := db.Model(&model.VideoTranscription{}).Where("task_id=42").Update("content", strings.Repeat("事务原子性。", 1000)).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "checkpoint", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- svc.ExecuteArtifact(workerCtx, run.ID) }()
	select {
	case <-second:
	case <-time.After(10 * time.Second):
		stop()
		close(release)
		t.Fatal("second segment not reached")
	}
	stop()
	<-done
	close(release)
	if err = db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("run_lease_until", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, run.ID)
	if err != nil || final.Status != "completed" || calls.Load() != 3 {
		t.Fatalf("checkpoint replay %+v %v calls=%d", final, err, calls.Load())
	}
}

func TestArtifactQueueExpiryRetentionAndTaskPagination(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	expired, err := svc.Submit(ctx, 7, "expired", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.GenerationRequest{}).Where("run_id=?", expired.ID).Update("queue_deadline", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err = svc.repos.Artifact.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := svc.Run(ctx, 7, expired.ID)
	if err != nil || run.Status != "failed" || run.ErrorCode == nil || *run.ErrorCode != "queue_expired" {
		t.Fatalf("queue deadline %+v %v", run, err)
	}
	completed, err := svc.Submit(ctx, 7, "retention", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteArtifact(ctx, completed.ID); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		rows, total, e := svc.Tasks(ctx, 7, page, 1)
		if e != nil || total != 3 || len(rows) != 1 {
			t.Fatalf("page %d %+v %d %v", page, rows, total, e)
		}
		if seen[rows[0].ID] {
			t.Fatal("duplicate task page")
		}
		seen[rows[0].ID] = true
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", completed.ID).Update("finished_at", time.Now().Add(-8*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err = svc.repos.Artifact.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	var retained int64
	db.Model(&model.AgentStep{}).Where("run_id=? AND result_checkpoint<>''", completed.ID).Count(&retained)
	if retained != 0 {
		t.Fatal("expired checkpoint retained")
	}
	detail, err := svc.Get(ctx, 7, completed.ArtifactID)
	if err != nil || detail.Version == nil || len(detail.Version.Body.Blocks) == 0 {
		t.Fatalf("retention removed product version %v %v", detail, err)
	}
}

func TestArtifactRetryBackoffSurvivesLeaseRecovery(t *testing.T) {
	svc, db, _ := artifactFixture(t, artifactModelResponse)
	ctx := context.Background()
	run, err := svc.Submit(ctx, 7, "backoff", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.repos.Artifact.Claim(ctx, run.ID, "old", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	call, err := svc.repos.Artifact.BeginCall(ctx, run.ID, "old", a.RunLeaseEpoch, "segment", artifact.Hash("prompt"), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	next := time.Now().UTC().Add(time.Minute)
	if err = svc.repos.Artifact.FailCall(ctx, run.ID, "old", a.RunLeaseEpoch, call, "provider_error", &next); err != nil {
		t.Fatal(err)
	}
	db.Model(&model.AgentRun{}).Where("id=?", run.ID).Update("run_lease_until", time.Now().Add(-time.Second))
	b, err := svc.repos.Artifact.Claim(ctx, run.ID, "new", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.repos.Artifact.BeginCall(ctx, run.ID, "new", b.RunLeaseEpoch, "segment", artifact.Hash("prompt"), 100, 100)
	var wait *artifact.RetryWait
	if !errors.As(err, &wait) || time.Until(wait.Until) < 50*time.Second {
		t.Fatalf("lost persisted backoff %v", err)
	}
	result, _ := svc.Run(ctx, 7, run.ID)
	if result.Usage.LLMCalls != 1 {
		t.Fatal("waiting reserved another call")
	}
}
