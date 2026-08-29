package repository

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

// 内容+目标级去重的持久兜底验收（docs/$1 seam）。
// 验证 (file_md5, job_type) 唯一约束：同内容同分析目标并发写入时，
// 只允许一个成功结果行落库（DB 兜底 Redis SETNX 失效后的语义）。
// 范式参考 task_lease_test.go 的 CAS 并发断言。

const dedupRepoMD5 = "dedupdedupdedupdedupdedupdeduprp01"

func newDedupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return db
}

// TestTranscriptionFileMD5UniqueAllowsOnlyOneCompletedRow 同 file_md5 的转写行
// 唯一约束：第二次插入（即使属不同 task）必须失败。这保证同内容重复 ASR
// 只能有一个成功结果行，DB 兜底 Redis 失效后的去重语义（见 docs/architecture/data-model.md）。
func TestTranscriptionFileMD5UniqueAllowsOnlyOneCompletedRow(t *testing.T) {
	db := newDedupTestDB(t)
	repos := NewRepositories(db)

	taskA := &model.VideoTask{UserID: 7, FileMD5: dedupRepoMD5, Filename: "a.mp4", Status: model.TaskStatusCompleted, TraceID: "t-a"}
	taskB := &model.VideoTask{UserID: 8, FileMD5: dedupRepoMD5, Filename: "b.mp4", Status: model.TaskStatusCompleted, TraceID: "t-b"}
	if err := repos.Task.Create(taskA); err != nil {
		t.Fatalf("create taskA: %v", err)
	}
	if err := repos.Task.Create(taskB); err != nil {
		t.Fatalf("create taskB: %v", err)
	}

	// 第一个 task 落转写结果行。
	if err := repos.Transcription.Create(&model.VideoTranscription{
		TaskID: taskA.ID, FileMD5: dedupRepoMD5, Content: "转写A", Words: 3,
	}); err != nil {
		t.Fatalf("first transcription insert: %v", err)
	}

	// 第二个 task 用同一 file_md5 插入：必须被唯一约束拒绝。
	err := repos.Transcription.Create(&model.VideoTranscription{
		TaskID: taskB.ID, FileMD5: dedupRepoMD5, Content: "转写B", Words: 3,
	})
	if err == nil {
		t.Fatal("second transcription insert with same file_md5 must fail unique constraint, got nil")
	}

	// FindByMD5 只回那个唯一成功行（跨 task/跨用户复用）。
	got, err := repos.Transcription.FindByMD5(dedupRepoMD5)
	if err != nil || got == nil {
		t.Fatalf("FindByMD5: got=%v err=%v, want the single existing row", got, err)
	}
	if got.TaskID != taskA.ID || got.Content != "转写A" {
		t.Fatalf("FindByMD5 returned %+v, want taskA's row", got)
	}
}

// TestSummaryFileMD5UniqueAllowsOnlyOneCompletedRow 摘要表同 file_md5 唯一约束。
func TestSummaryFileMD5UniqueAllowsOnlyOneCompletedRow(t *testing.T) {
	db := newDedupTestDB(t)
	repos := NewRepositories(db)

	taskA := &model.VideoTask{UserID: 7, FileMD5: dedupRepoMD5, Filename: "a.mp4", Status: model.TaskStatusCompleted, TraceID: "t-a"}
	taskB := &model.VideoTask{UserID: 9, FileMD5: dedupRepoMD5, Filename: "b.mp4", Status: model.TaskStatusCompleted, TraceID: "t-b"}
	if err := repos.Task.Create(taskA); err != nil {
		t.Fatalf("create taskA: %v", err)
	}
	if err := repos.Task.Create(taskB); err != nil {
		t.Fatalf("create taskB: %v", err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: taskA.ID, FileMD5: dedupRepoMD5, Content: "摘要A", ModelName: "mimo"}); err != nil {
		t.Fatalf("first summary insert: %v", err)
	}
	err := repos.Summary.Create(&model.AISummary{TaskID: taskB.ID, FileMD5: dedupRepoMD5, Content: "摘要B", ModelName: "mimo"})
	if err == nil {
		t.Fatal("second summary insert with same file_md5 must fail unique constraint, got nil")
	}
	got, err := repos.Summary.FindByMD5(dedupRepoMD5)
	if err != nil || got == nil {
		t.Fatalf("FindByMD5: got=%v err=%v, want the single existing row", got, err)
	}
	if got.TaskID != taskA.ID {
		t.Fatalf("FindByMD5 returned task %d, want taskA %d", got.TaskID, taskA.ID)
	}
}

// TestRAGIndexFileMD5AndModelUniqueAllowsDifferentModelsSameContent 同一 file_md5 +
// 不同 embedding_model 允许各一行（索引按模型独立去重）；同 file_md5 + 同 model
// 第二个必须失败。索引重建换模型不被旧索引挡。
func TestRAGIndexFileMD5AndModelUniqueAllowsDifferentModelsSameContent(t *testing.T) {
	db := newDedupTestDB(t)
	repos := NewRepositories(db)

	taskA := &model.VideoTask{UserID: 7, FileMD5: dedupRepoMD5, Filename: "a.mp4", Status: model.TaskStatusCompleted, TraceID: "t-a"}
	taskB := &model.VideoTask{UserID: 7, FileMD5: dedupRepoMD5, Filename: "b.mp4", Status: model.TaskStatusCompleted, TraceID: "t-b"}
	if err := repos.Task.Create(taskA); err != nil {
		t.Fatalf("create taskA: %v", err)
	}
	if err := repos.Task.Create(taskB); err != nil {
		t.Fatalf("create taskB: %v", err)
	}

	// 同内容 + model-A：第一行成功。
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID: 7, TaskID: taskA.ID, FileMD5: dedupRepoMD5,
		EmbeddingModel: "embed-a", EmbeddingDim: 1536, Status: model.RAGIndexStatusIndexed, ChunkCount: 1,
	}); err != nil {
		t.Fatalf("first rag index (model-a): %v", err)
	}
	// 同内容 + model-B（不同模型）：必须成功——换模型不挡。
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID: 7, TaskID: taskB.ID, FileMD5: dedupRepoMD5,
		EmbeddingModel: "embed-b", EmbeddingDim: 1536, Status: model.RAGIndexStatusIndexed, ChunkCount: 1,
	}); err != nil {
		t.Fatalf("second rag index (model-b, different model) must succeed: %v", err)
	}

	// 同内容 + model-A 第二行（taskB）：必须失败（同 file_md5 + 同 model 唯一）。
	err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID: 7, TaskID: taskB.ID, FileMD5: dedupRepoMD5,
		EmbeddingModel: "embed-a", EmbeddingDim: 1536, Status: model.RAGIndexStatusIndexed, ChunkCount: 1,
	})
	if err == nil {
		t.Fatal("rag index with same (file_md5, embedding_model) second row must fail unique constraint")
	}

	// FindByMD5AndModel 只命中 status=indexed 的成功行；把旧索引改 failed 后不挡重索引。
	if err := db.Model(&model.VideoRAGIndex{}).Where("task_id = ?", taskA.ID).
		Update("status", model.RAGIndexStatusFailed).Error; err != nil {
		t.Fatalf("mark old index failed: %v", err)
	}
	got, err := repos.RAGIndex.FindByMD5AndModel(dedupRepoMD5, "embed-a")
	if err != nil {
		t.Fatalf("FindByMD5AndModel: %v", err)
	}
	if got != nil {
		t.Fatalf("FindByMD5AndModel on failed index = %+v, want nil (only indexed reused)", got)
	}
}

// TestDedupLookupErrorsReturnWrapped 转写/摘要 FindByMD5 在 DB 错误时返回错误
// （非静默 nil），保证去重判定不因 DB 故障误判命中。
func TestDedupLookupErrorsReturnWrapped(t *testing.T) {
	db := newDedupTestDB(t)
	repos := NewRepositories(db)
	// Drop transcription table to force a query error.
	if err := db.Migrator().DropTable(&model.VideoTranscription{}); err != nil {
		t.Fatalf("drop transcription table: %v", err)
	}
	_, err := repos.Transcription.FindByMD5(dedupRepoMD5)
	if err == nil {
		t.Fatal("FindByMD5 on missing table: got nil, want error")
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("FindByMD5 should return raw error on DB failure, not ErrRecordNotFound: %v", err)
	}
}
