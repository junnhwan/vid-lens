package service

import (
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestKnowledgeBaseReadinessUsesFixedBatchQueries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	kb := &model.KnowledgeBase{UserID: 7, Name: "batch"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	taskIDs := make([]int64, 0, 3)
	for i, md5 := range []string{"11111111111111111111111111111111", "22222222222222222222222222222222", "33333333333333333333333333333333"} {
		task := &model.VideoTask{UserID: 7, FileMD5: md5, Filename: md5[:1] + ".mp4", FileURL: "videos/batch", Title: string(rune('A' + i))}
		if err := repos.Task.Create(task); err != nil {
			t.Fatal(err)
		}
		taskIDs = append(taskIDs, task.ID)
		if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, task.ID); err != nil {
			t.Fatal(err)
		}
		if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: task.ID, EmbeddingModel: "embed-v1", EmbeddingDim: 3, Status: model.RAGIndexStatusIndexed}); err != nil {
			t.Fatal(err)
		}
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID, Title: kb.Name}
	var queries int64
	if err := db.Callback().Query().Before("gorm:query").Register("test:count_batch_readiness", func(*gorm.DB) { atomic.AddInt64(&queries, 1) }); err != nil {
		t.Fatal(err)
	}

	got, err := NewChatService(repos, nil, ChatConfig{}).sessionRetrievalTaskIDs(7, session, "embed-v1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, taskIDs) {
		t.Fatalf("ids=%v want=%v", got, taskIDs)
	}
	if queries > 4 {
		t.Fatalf("readiness queries=%d, want fixed owner+membership+tasks+indexes queries", queries)
	}
}

func TestKnowledgeBaseReadinessReportsMissingUnindexedAndModelSwitch(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	kb := &model.KnowledgeBase{UserID: 7, Name: "states"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	tasks := make([]*model.VideoTask, 0, 3)
	for _, md5 := range []string{"44444444444444444444444444444444", "55555555555555555555555555555555", "66666666666666666666666666666666"} {
		task := &model.VideoTask{UserID: 7, FileMD5: md5, Filename: "state.mp4", FileURL: "videos/state"}
		if err := repos.Task.Create(task); err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
		if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, task.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: tasks[0].ID, EmbeddingModel: "embed-v1", EmbeddingDim: 3, Status: model.RAGIndexStatusIndexed}); err != nil {
		t.Fatal(err)
	}
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: tasks[2].ID, EmbeddingModel: "embed-v1", EmbeddingDim: 3, Status: model.RAGIndexStatusFailed}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Task.Delete(tasks[1].ID); err != nil {
		t.Fatal(err)
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID}
	svc := NewChatService(repos, nil, ChatConfig{})

	_, err := svc.sessionRetrievalTaskIDs(7, session, "embed-v1")
	assertUnavailableTaskIDs(t, err, []int64{tasks[1].ID, tasks[2].ID})
	_, err = svc.sessionRetrievalTaskIDs(7, session, "embed-v2")
	assertUnavailableTaskIDs(t, err, []int64{tasks[0].ID, tasks[1].ID, tasks[2].ID})
}

func assertUnavailableTaskIDs(t *testing.T, err error, ids []int64) {
	t.Helper()
	if err == nil {
		t.Fatal("expected unavailable task IDs")
	}
	for _, id := range ids {
		if !containsInt64Text(err.Error(), id) {
			t.Fatalf("err=%q missing task id %d", err, id)
		}
	}
}

func containsInt64Text(text string, id int64) bool {
	return strings.Contains(text, strconv.FormatInt(id, 10))
}
