package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// 内容+目标级去重的服务层验收（docs/$1 seam）。
// 复用 media_file_upload_test.go 的 fake repos + recordingMediaProducer 范式：
// 内存 SQLite + 录制型 mq producer，断言"第二次不 enqueue / 秒传到 Completed /
// 结果按 file_md5 关联已有行 / 部分命中只跑缺失 job / 失败结果不被复用 /
// force=true 仍可重跑"。

const dedupTestMD5 = "dedupdedupdedupdedupdedupdedupde01"

// newContentDedupTestService 装配一个最小 MediaService：内存 repos + 录制型 mq
// producer + miniredis（SETNX 内容锁可被真实验证）。
func newContentDedupTestService(t *testing.T) (*MediaService, *repository.Repositories, *recordingMediaProducer, *redis.Client) {
	t.Helper()
	repos := newMediaTestRepositories(t)
	producer := &recordingMediaProducer{}
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc := &MediaService{repo: repos, mq: producer, rdb: rdb, cfg: config.UploadConfig{MaxFileSize: 100 << 20}}
	return svc, repos, producer, rdb
}

// seedCompletedResults 在 file_md5 对应的内容上预置成功转写+摘要行（模拟
// 用户 A 已经处理过同一视频）。行属一个旧 task，file_md5 直接写入结果表
// （内容+目标级去重键），跨 task/跨用户可被复用。
func seedCompletedResults(t *testing.T, repos *repository.Repositories, fileMD5 string, userID int64) (*model.VideoTask, *model.VideoAsset) {
	t.Helper()
	asset := createMediaTestAsset(t, repos, fileMD5, "videos/already-processed.mp4")
	task := &model.VideoTask{
		UserID:   userID,
		AssetID:  &asset.ID,
		FileMD5:  asset.FileMD5,
		Filename: "original.mp4",
		FileURL:  asset.ObjectName,
		Status:   model.TaskStatusCompleted,
		Stage:    model.TaskStageNone,
		TraceID:  "trace-original",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create original task: %v", err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{
		TaskID: task.ID, FileMD5: fileMD5, Content: "已完成的转写", Words: 6,
	}); err != nil {
		t.Fatalf("seed transcription: %v", err)
	}
	if err := repos.Summary.Create(&model.AISummary{
		TaskID: task.ID, FileMD5: fileMD5, Content: "已完成的摘要", ModelName: "mimo-v2.5",
	}); err != nil {
		t.Fatalf("seed summary: %v", err)
	}
	return task, asset
}

// TestContentDedupFullHitShortCircuitsToCompleted 重复上传同一内容（已全部处理过）
// 时，新 task 秒传到 Completed，不 enqueue 任何 job，零 AI 调用（见当前任务处理与去重约束）。
func TestContentDedupFullHitShortCircuitsToCompleted(t *testing.T) {
	svc, repos, producer, _ := newContentDedupTestService(t)
	seedCompletedResults(t, repos, dedupTestMD5, 7)

	// 用户 B 再传同一内容：Asset 复用（文件层），createTaskFromAsset 命中
	// 已有转写+摘要（内容+目标层）→ 秒传 Completed。
	asset, err := repos.Asset.FindByMD5(dedupTestMD5)
	if err != nil || asset == nil {
		t.Fatalf("reuse asset: %v", err)
	}
	result, err := svc.createTaskFromAsset(8, "repeat.mp4", asset, model.TaskStatusPending)
	if err != nil {
		t.Fatalf("createTaskFromAsset: %v", err)
	}
	if result.Status != model.TaskStatusCompleted {
		t.Fatalf("dedup-hit task status = %d, want Completed(3)", result.Status)
	}
	created, err := repos.Task.FindByID(result.TaskID)
	if err != nil {
		t.Fatalf("find created task: %v", err)
	}
	if created.Status != model.TaskStatusCompleted || created.Stage != model.TaskStageNone {
		t.Fatalf("created task status/stage = %d/%q, want Completed/none", created.Status, created.Stage)
	}
	// 零 AI 调用的可观察证据：没有任何 transcribe/analyze enqueue。
	if len(producer.transcribes) != 0 || len(producer.analyzes) != 0 {
		t.Fatalf("dedup-hit enqueued jobs: transcribe=%v analyze=%v, want none", producer.transcribes, producer.analyzes)
	}
}

// TestContentDedupFullHitDetailJoinsResultsByFileMD5 秒传到 Completed 的新 task 自己
// 没有结果行，详情页按 file_md5 关联任意 task 的已有行展示（当前实现约束，跨用户复用）。
func TestContentDedupFullHitDetailJoinsResultsByFileMD5(t *testing.T) {
	svc, repos, _, _ := newContentDedupTestService(t)
	seedCompletedResults(t, repos, dedupTestMD5, 7)

	asset, _ := repos.Asset.FindByMD5(dedupTestMD5)
	result, err := svc.createTaskFromAsset(8, "repeat.mp4", asset, model.TaskStatusPending)
	if err != nil {
		t.Fatalf("createTaskFromAsset: %v", err)
	}

	detail, err := svc.GetTaskDetail(context.Background(), 8, result.TaskID)
	if err != nil {
		t.Fatalf("GetTaskDetail: %v", err)
	}
	if detail.Transcription == nil || detail.Transcription.Content != "已完成的转写" {
		t.Fatalf("detail transcription = %+v, want reuse of seeded row by file_md5", detail.Transcription)
	}
	if detail.Summary == nil || detail.Summary.Content != "已完成的摘要" {
		t.Fatalf("detail summary = %+v, want reuse of seeded row by file_md5", detail.Summary)
	}
	if !detail.HasTranscription || !detail.HasSummary {
		t.Fatalf("detail flags = tx=%v sum=%v, want both true", detail.HasTranscription, detail.HasSummary)
	}
	// 跨用户复用：结果行属于用户 7 的旧 task，但用户 8 能看到。
	if detail.Transcription.TaskID == result.TaskID {
		t.Fatalf("detail transcription TaskID = new task %d, want reuse from old task", result.TaskID)
	}
}

// TestContentDedupPartialHitTranscriptionOnlyEnqueuesSummary 部分命中：转写已有但
// 摘要没有 → 只跑摘要，不重跑转写（分析目标级而非视频级去重）。
func TestContentDedupPartialHitTranscriptionOnlyEnqueuesSummary(t *testing.T) {
	svc, repos, producer, _ := newContentDedupTestService(t)
	// 只预置转写，不预置摘要。
	asset := createMediaTestAsset(t, repos, dedupTestMD5, "videos/partial.mp4")
	original := &model.VideoTask{UserID: 7, AssetID: &asset.ID, FileMD5: asset.FileMD5, Filename: "o.mp4", FileURL: asset.ObjectName, Status: model.TaskStatusCompleted, TraceID: "t-o"}
	if err := repos.Task.Create(original); err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := repos.Transcription.Create(&model.VideoTranscription{TaskID: original.ID, FileMD5: dedupTestMD5, Content: "转写", Words: 2}); err != nil {
		t.Fatalf("seed transcription: %v", err)
	}

	// 用户 8 上传同内容：转写已有但摘要没有 → 不秒传 Completed（部分命中），保持 Pending。
	result, err := svc.createTaskFromAsset(8, "repeat.mp4", asset, model.TaskStatusPending)
	if err != nil {
		t.Fatalf("createTaskFromAsset: %v", err)
	}
	if result.Status != model.TaskStatusPending {
		t.Fatalf("partial-hit task status = %d, want Pending(0) — only summary should rerun", result.Status)
	}

	// RequestTranscribe 命中已有转写 → 不重跑 ASR，返回"已完成可直接查看结果"。
	// 分析目标级独立：转写命中不替 task 做整体完成判定（摘要可能仍缺，
	// 用户可继续 RequestAnalysis，当前实现约束）。
	err = svc.RequestTranscribe(context.Background(), 8, result.TaskID, false)
	if err == nil {
		t.Fatal("RequestTranscribe on dedup-hit should return '已完成可直接查看结果', got nil")
	}
	if len(producer.transcribes) != 0 {
		t.Fatalf("partial-hit re-ran transcribe = %v, want 0 (reuse existing transcription)", producer.transcribes)
	}
	// 转写复用后 task 仍 Pending（摘要未就绪，不替 task 做整体完成判定）。
	transcribedTask, _ := repos.Task.FindByID(result.TaskID)
	if transcribedTask.Status != model.TaskStatusPending {
		t.Fatalf("after RequestTranscribe dedup, status = %d, want Pending (summary still missing)", transcribedTask.Status)
	}
}

// TestContentDedupRequestAnalysisReusesSummaryByFileMD5 RequestAnalysis force=false 命中
// 任意 task 的成功摘要 → 不重跑 LLM，返回"已完成可直接查看结果"（当前实现约束，
// 查询从当前 task 提到 file_md5；与原单 task 短路语义一致）。
func TestContentDedupRequestAnalysisReusesSummaryByFileMD5(t *testing.T) {
	svc, repos, producer, _ := newContentDedupTestService(t)
	seedCompletedResults(t, repos, dedupTestMD5, 7)

	asset, _ := repos.Asset.FindByMD5(dedupTestMD5)
	result, err := svc.createTaskFromAsset(8, "repeat.mp4", asset, model.TaskStatusPending)
	if err != nil {
		t.Fatalf("createTaskFromAsset: %v", err)
	}

	// 上传已秒传 Completed（全命中）。force=false RequestAnalysis 应命中已有摘要、不重跑。
	err = svc.RequestAnalysis(context.Background(), 8, result.TaskID, false)
	if err == nil {
		t.Fatal("RequestAnalysis on dedup-hit should return '已完成可直接查看结果', got nil")
	}
	if len(producer.analyzes) != 0 {
		t.Fatalf("dedup-hit re-ran analyze = %v, want 0 (reuse existing summary by file_md5)", producer.analyzes)
	}
}

// TestContentDedupFailedResultNotReused 失败结果不被复用：只复用成功结果行
// （转写/摘要表行存在即成功；失败不会落行）（当前实现约束）。
func TestContentDedupFailedResultNotReused(t *testing.T) {
	svc, repos, producer, _ := newContentDedupTestService(t)
	// 用户 7 的 task 处于 Failed，且没有 transcription/summary 行（失败不落行）。
	asset := createMediaTestAsset(t, repos, dedupTestMD5, "videos/failed.mp4")
	failed := &model.VideoTask{UserID: 7, AssetID: &asset.ID, FileMD5: asset.FileMD5, Filename: "f.mp4", FileURL: asset.ObjectName, Status: model.TaskStatusFailed, TraceID: "t-f"}
	if err := repos.Task.Create(failed); err != nil {
		t.Fatalf("create failed task: %v", err)
	}

	result, err := svc.createTaskFromAsset(8, "repeat.mp4", asset, model.TaskStatusPending)
	if err != nil {
		t.Fatalf("createTaskFromAsset: %v", err)
	}
	if result.Status != model.TaskStatusPending {
		t.Fatalf("failed-result task status = %d, want Pending — failed result must not be reused", result.Status)
	}
	// RequestTranscribe 不应秒传（没有成功转写行），应走正常 enqueue。
	if err := svc.RequestTranscribe(context.Background(), 8, result.TaskID, false); err != nil {
		t.Fatalf("RequestTranscribe: %v", err)
	}
	if len(producer.transcribes) != 1 {
		t.Fatalf("failed-result should enqueue transcribe = %d calls, want 1", len(producer.transcribes))
	}
}

// TestContentDedupForceOverridesReuse force=true 仍可强制重跑，去重不破坏重新分析能力
// （当前实现约束）。
func TestContentDedupForceOverridesReuse(t *testing.T) {
	svc, repos, producer, _ := newContentDedupTestService(t)
	seedCompletedResults(t, repos, dedupTestMD5, 7)

	asset, _ := repos.Asset.FindByMD5(dedupTestMD5)
	result, err := svc.createTaskFromAsset(8, "repeat.mp4", asset, model.TaskStatusPending)
	if err != nil {
		t.Fatalf("createTaskFromAsset: %v", err)
	}
	// 上传时秒传了 Completed（全命中）。force=true RequestAnalysis 应重跑而非复用。
	task, _ := repos.Task.FindByID(result.TaskID)
	if task.Status != model.TaskStatusCompleted {
		t.Fatalf("precondition: dedup-hit task should be Completed, got %d", task.Status)
	}
	if err := svc.RequestAnalysis(context.Background(), 8, result.TaskID, true); err != nil {
		t.Fatalf("RequestAnalysis force: %v", err)
	}
	if len(producer.analyzes) != 1 {
		t.Fatalf("force=true should re-run analyze = %d calls, want 1", len(producer.analyzes))
	}
}

// TestContentDedupLockSerializesConcurrentSameContent SETNX 内容锁在并发上传同内容时
// 只让一个跑 AI、其余走复用（见 docs/architecture/reliability.md）。用 miniredis 验证第二个并发请求被锁挡住。
func TestContentDedupLockSerializesConcurrentSameContent(t *testing.T) {
	svc, _, _, rdb := newContentDedupTestService(t)
	ctx := context.Background()
	// 第一个请求先抢到 transcribe 锁。
	acquired, err := svc.acquireContentDedupLock(ctx, dedupTestMD5, model.TaskJobTypeTranscribe)
	if err != nil || !acquired {
		t.Fatalf("first acquire: acquired=%v err=%v, want true", acquired, err)
	}
	// 第二个并发请求同键应拿不到锁。
	acquired2, err := svc.acquireContentDedupLock(ctx, dedupTestMD5, model.TaskJobTypeTranscribe)
	if err != nil || acquired2 {
		t.Fatalf("second acquire: acquired=%v err=%v, want false (locked)", acquired2, err)
	}
	// 键确实落库。
	exists, _ := rdb.Exists(ctx, contentDedupKey(dedupTestMD5, model.TaskJobTypeTranscribe)).Result()
	if exists != 1 {
		t.Fatalf("dedup lock key not present in redis after acquire")
	}
}

// TestContentDedupHitCountIsObservable 命中计数可观测（当前实现约束）：每次去重
// 命中 +1，是去重指标的可跑统计来源。验收命令 = 跑本测试套件后
// ContentDedupHits() 返回真实命中数（非估算）。
func TestContentDedupHitCountIsObservable(t *testing.T) {
	// 重置进程内计数器到已知基线，隔离本测试。
	resetContentDedupHitsForTest()
	svc, repos, _, _ := newContentDedupTestService(t)
	seedCompletedResults(t, repos, dedupTestMD5, 7)

	if got := ContentDedupHits(); got != 0 {
		t.Fatalf("baseline hits = %d, want 0", got)
	}

	// 重复上传同内容（全命中）→ +1（转写+摘要复用）。
	asset, _ := repos.Asset.FindByMD5(dedupTestMD5)
	if _, err := svc.createTaskFromAsset(8, "repeat.mp4", asset, model.TaskStatusPending); err != nil {
		t.Fatalf("createTaskFromAsset: %v", err)
	}
	if got := ContentDedupHits(); got != 1 {
		t.Fatalf("after upload full hit, hits = %d, want 1", got)
	}

	// 第二次重复上传（仍全命中）→ +1。
	if _, err := svc.createTaskFromAsset(9, "repeat2.mp4", asset, model.TaskStatusPending); err != nil {
		t.Fatalf("second createTaskFromAsset: %v", err)
	}
	if got := ContentDedupHits(); got != 2 {
		t.Fatalf("after second upload full hit, hits = %d, want 2", got)
	}

	// RequestAnalysis 命中已有摘要 → +1（LLM 复用），返回"已完成可直接查看结果"。
	result2, err := svc.createTaskFromAsset(10, "repeat3.mp4", asset, model.TaskStatusPending)
	if err != nil {
		t.Fatalf("third createTaskFromAsset: %v", err)
	}
	if got := ContentDedupHits(); got != 3 {
		t.Fatalf("after third upload full hit, hits = %d, want 3", got)
	}
	if err := svc.RequestAnalysis(context.Background(), 10, result2.TaskID, false); err == nil {
		t.Fatal("RequestAnalysis on dedup-hit should return '已完成可直接查看结果', got nil")
	}
	if got := ContentDedupHits(); got != 4 {
		t.Fatalf("after RequestAnalysis hit, hits = %d, want 4", got)
	}
}
