package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestKnowledgeBaseServiceCRUDIsOwnerSafeAndValidatesText(t *testing.T) {
	svc, _, _ := newKnowledgeBaseServiceTestEnv(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, 7, CreateKnowledgeBaseRequest{Name: "  面试资料库  ", Description: "Go 与 RAG"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name != "面试资料库" {
		t.Fatalf("Create() name = %q, want trimmed", created.Name)
	}

	ownerList, err := svc.List(ctx, 7)
	if err != nil || len(ownerList) != 1 || ownerList[0].ID != created.ID {
		t.Fatalf("owner List() = %+v, %v", ownerList, err)
	}
	otherList, err := svc.List(ctx, 8)
	if err != nil || len(otherList) != 0 {
		t.Fatalf("other List() = %+v, %v", otherList, err)
	}
	if _, err := svc.Get(ctx, 8, created.ID); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("cross-user Get() error = %v, want ErrKnowledgeBaseNotFound", err)
	}

	newName := "  更新后的库  "
	updated, err := svc.Update(ctx, 7, created.ID, UpdateKnowledgeBaseRequest{Name: &newName})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "更新后的库" || updated.Description != "Go 与 RAG" {
		t.Fatalf("Update() = %+v", updated)
	}
	if _, err := svc.Update(ctx, 8, created.ID, UpdateKnowledgeBaseRequest{Name: &newName}); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("cross-user Update() error = %v", err)
	}
	if err := svc.Delete(ctx, 8, created.ID); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("cross-user Delete() error = %v", err)
	}
	if _, err := svc.Get(ctx, 7, created.ID); err != nil {
		t.Fatalf("owner Get() after rejected delete error = %v", err)
	}

	invalidCreates := []struct {
		name string
		req  CreateKnowledgeBaseRequest
		err  error
	}{
		{name: "blank", req: CreateKnowledgeBaseRequest{Name: " \t\n "}, err: ErrKnowledgeBaseNameRequired},
		{name: "name too long", req: CreateKnowledgeBaseRequest{Name: strings.Repeat("知", 101)}, err: ErrKnowledgeBaseNameTooLong},
		{name: "description too long", req: CreateKnowledgeBaseRequest{Name: "ok", Description: strings.Repeat("述", 501)}, err: ErrKnowledgeBaseDescriptionTooLong},
	}
	for _, tc := range invalidCreates {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(ctx, 7, tc.req); !errors.Is(err, tc.err) {
				t.Fatalf("Create() error = %v, want %v", err, tc.err)
			}
		})
	}

	blank := "   "
	if _, err := svc.Update(ctx, 7, created.ID, UpdateKnowledgeBaseRequest{Name: &blank}); !errors.Is(err, ErrKnowledgeBaseNameRequired) {
		t.Fatalf("blank Update() error = %v", err)
	}
	longDescription := strings.Repeat("述", 501)
	if _, err := svc.Update(ctx, 7, created.ID, UpdateKnowledgeBaseRequest{Description: &longDescription}); !errors.Is(err, ErrKnowledgeBaseDescriptionTooLong) {
		t.Fatalf("long description Update() error = %v", err)
	}
}

func TestKnowledgeBaseServiceAddVideoRequiresOwnedRetrievableTaskAndIsIdempotent(t *testing.T) {
	ctx := context.Background()

	t.Run("missing default profile", func(t *testing.T) {
		svc, repos, _ := newKnowledgeBaseServiceTestEnv(t)
		kb := createKnowledgeBaseForServiceTest(t, svc, 7, "kb")
		task := createKnowledgeBaseTaskForServiceTest(t, repos, 7, "no-profile")
		if err := svc.AddVideo(ctx, 7, kb.ID, task.ID); !errors.Is(err, ErrKnowledgeBaseDefaultProfileRequired) {
			t.Fatalf("AddVideo() error = %v, want default-profile error", err)
		}
	})

	t.Run("task ownership and deletion are permission safe", func(t *testing.T) {
		svc, repos, db := newKnowledgeBaseServiceTestEnv(t)
		createKnowledgeBaseDefaultProfileForServiceTest(t, repos, 7, "embed-a")
		kb := createKnowledgeBaseForServiceTest(t, svc, 7, "kb")
		crossUser := createKnowledgeBaseTaskForServiceTest(t, repos, 8, "cross-user")
		if err := svc.AddVideo(ctx, 7, kb.ID, crossUser.ID); !errors.Is(err, ErrKnowledgeBaseTaskNotFound) {
			t.Fatalf("cross-user AddVideo() error = %v", err)
		}
		deleted := createKnowledgeBaseTaskForServiceTest(t, repos, 7, "deleted")
		if err := db.Delete(deleted).Error; err != nil {
			t.Fatal(err)
		}
		if err := svc.AddVideo(ctx, 7, kb.ID, deleted.ID); !errors.Is(err, ErrKnowledgeBaseTaskNotFound) {
			t.Fatalf("deleted AddVideo() error = %v", err)
		}
	})

	t.Run("current embedding model must be indexed", func(t *testing.T) {
		svc, repos, _ := newKnowledgeBaseServiceTestEnv(t)
		createKnowledgeBaseDefaultProfileForServiceTest(t, repos, 7, "embed-a")
		kb := createKnowledgeBaseForServiceTest(t, svc, 7, "kb")
		task := createKnowledgeBaseTaskForServiceTest(t, repos, 7, "rag")
		if err := svc.AddVideo(ctx, 7, kb.ID, task.ID); !errors.Is(err, ErrKnowledgeBaseTaskNotIndexed) {
			t.Fatalf("unindexed AddVideo() error = %v", err)
		}
		createKnowledgeBaseRAGIndexForServiceTest(t, repos, 7, task.ID, "embed-b", model.RAGIndexStatusIndexed)
		if err := svc.AddVideo(ctx, 7, kb.ID, task.ID); !errors.Is(err, ErrKnowledgeBaseTaskNotIndexed) {
			t.Fatalf("mismatched model AddVideo() error = %v", err)
		}
		createKnowledgeBaseRAGIndexForServiceTest(t, repos, 7, task.ID, "embed-a", model.RAGIndexStatusFailed)
		if err := svc.AddVideo(ctx, 7, kb.ID, task.ID); !errors.Is(err, ErrKnowledgeBaseTaskNotIndexed) {
			t.Fatalf("failed index AddVideo() error = %v", err)
		}
		createKnowledgeBaseRAGIndexForServiceTest(t, repos, 7, task.ID, "embed-a", model.RAGIndexStatusIndexed)
		if err := svc.AddVideo(ctx, 7, kb.ID, task.ID); err != nil {
			t.Fatalf("indexed AddVideo() error = %v", err)
		}
		if err := svc.AddVideo(ctx, 7, kb.ID, task.ID); err != nil {
			t.Fatalf("duplicate AddVideo() error = %v", err)
		}
		count, err := repos.KnowledgeBase.CountMembers(7, kb.ID)
		if err != nil || count != 1 {
			t.Fatalf("member count = %d, %v, want 1", count, err)
		}
	})
}

func TestKnowledgeBaseServiceAddVideoLocksBeforeEnforcingFiftyMemberLimit(t *testing.T) {
	svc, repos, _ := newKnowledgeBaseServiceTestEnv(t)
	ctx := context.Background()
	createKnowledgeBaseDefaultProfileForServiceTest(t, repos, 7, "embed-a")
	kb := createKnowledgeBaseForServiceTest(t, svc, 7, "full")

	var existing *model.VideoTask
	for i := 0; i < KnowledgeBaseMaxVideos; i++ {
		task := createKnowledgeBaseTaskForServiceTest(t, repos, 7, fmt.Sprintf("member-%02d", i))
		if i == 0 {
			existing = task
			createKnowledgeBaseRAGIndexForServiceTest(t, repos, 7, task.ID, "embed-a", model.RAGIndexStatusIndexed)
		}
		if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, task.ID); err != nil {
			t.Fatalf("seed member %d: %v", i, err)
		}
	}

	// Idempotent duplicate must still succeed when the KB is already full.
	if err := svc.AddVideo(ctx, 7, kb.ID, existing.ID); err != nil {
		t.Fatalf("duplicate at limit AddVideo() error = %v", err)
	}

	candidate := createKnowledgeBaseTaskForServiceTest(t, repos, 7, "candidate")
	createKnowledgeBaseRAGIndexForServiceTest(t, repos, 7, candidate.ID, "embed-a", model.RAGIndexStatusIndexed)
	if err := svc.AddVideo(ctx, 7, kb.ID, candidate.ID); !errors.Is(err, ErrKnowledgeBaseVideoLimit) {
		t.Fatalf("51st AddVideo() error = %v, want limit error", err)
	}
	count, err := repos.KnowledgeBase.CountMembers(7, kb.ID)
	if err != nil || count != KnowledgeBaseMaxVideos {
		t.Fatalf("member count after rejected insert = %d, %v", count, err)
	}
}

func TestKnowledgeBaseServiceDetailReportsCurrentModelWithoutLeakingTasks(t *testing.T) {
	svc, repos, db := newKnowledgeBaseServiceTestEnv(t)
	ctx := context.Background()
	createKnowledgeBaseDefaultProfileForServiceTest(t, repos, 7, "embed-a")
	kb := createKnowledgeBaseForServiceTest(t, svc, 7, "detail")
	withTitle := createKnowledgeBaseTaskForServiceTest(t, repos, 7, "with-title")
	withTitle.Title = "自定义标题"
	if err := db.Model(withTitle).Update("title", withTitle.Title).Error; err != nil {
		t.Fatal(err)
	}
	filenameOnly := createKnowledgeBaseTaskForServiceTest(t, repos, 7, "filename-only")
	foreign := createKnowledgeBaseTaskForServiceTest(t, repos, 8, "foreign-secret")
	for _, task := range []*model.VideoTask{withTitle, filenameOnly} {
		if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, task.ID); err != nil {
			t.Fatal(err)
		}
	}
	createKnowledgeBaseRAGIndexForServiceTest(t, repos, 7, withTitle.ID, "embed-a", model.RAGIndexStatusIndexed)
	createKnowledgeBaseRAGIndexForServiceTest(t, repos, 7, filenameOnly.ID, "embed-a", model.RAGIndexStatusFailed)
	// Corrupt/stale cross-user edge must not be exposed by detail assembly.
	if err := db.Create(&model.KnowledgeBaseVideo{KnowledgeBaseID: kb.ID, TaskID: foreign.ID}).Error; err != nil {
		t.Fatal(err)
	}

	detail, err := svc.Get(ctx, 7, kb.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if detail.EmbeddingModel != "embed-a" || len(detail.Videos) != 2 {
		t.Fatalf("detail = %+v", detail)
	}
	byID := map[int64]KnowledgeBaseVideoResponse{}
	for _, video := range detail.Videos {
		byID[video.TaskID] = video
	}
	if got := byID[withTitle.ID]; got.Title != "自定义标题" || !got.Retrievable || got.IndexStatus != model.RAGIndexStatusIndexed {
		t.Fatalf("title member = %+v", got)
	}
	if got := byID[filenameOnly.ID]; got.Title != filenameOnly.Filename || got.Retrievable || got.IndexStatus != model.RAGIndexStatusFailed {
		t.Fatalf("filename member = %+v", got)
	}
	if _, leaked := byID[foreign.ID]; leaked {
		t.Fatalf("foreign task leaked in detail: %+v", detail.Videos)
	}
}

func TestKnowledgeBaseServiceRemoveAndDeletePreserveVideoData(t *testing.T) {
	svc, repos, db := newKnowledgeBaseServiceTestEnv(t)
	ctx := context.Background()
	createKnowledgeBaseDefaultProfileForServiceTest(t, repos, 7, "embed-a")
	kb := createKnowledgeBaseForServiceTest(t, svc, 7, "lifecycle")
	task := createKnowledgeBaseTaskForServiceTest(t, repos, 7, "kept-task")
	createKnowledgeBaseRAGIndexForServiceTest(t, repos, 7, task.ID, "embed-a", model.RAGIndexStatusIndexed)
	chunk := &model.VideoChunk{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "source chunk", EmbeddingModel: "embed-a"}
	if err := db.Create(chunk).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.AddVideo(ctx, 7, kb.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveVideo(ctx, 8, kb.ID, task.ID); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("cross-user RemoveVideo() error = %v", err)
	}
	if err := svc.RemoveVideo(ctx, 7, kb.ID, task.ID); err != nil {
		t.Fatalf("RemoveVideo() error = %v", err)
	}
	assertKnowledgeBaseVideoDataPreserved(t, db, task.ID, chunk.ID)
	if err := svc.AddVideo(ctx, 7, kb.ID, task.ID); err != nil {
		t.Fatal(err)
	}

	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID, Title: "kb chat"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	message := &model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "assistant", Content: "answer"}
	if err := repos.Chat.CreateMessage(message); err != nil {
		t.Fatal(err)
	}
	if err := repos.Chat.CreateMessageSource(7, &model.ChatMessageSource{MessageID: message.ID, SessionID: session.ID, TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(ctx, 7, kb.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	assertKnowledgeBaseVideoDataPreserved(t, db, task.ID, chunk.ID)
	for table, target := range map[string]any{
		"knowledge_bases":       &model.KnowledgeBase{},
		"knowledge_base_videos": &model.KnowledgeBaseVideo{},
		"chat_sessions":         &model.ChatSession{},
		"chat_messages":         &model.ChatMessage{},
		"chat_message_sources":  &model.ChatMessageSource{},
	} {
		var count int64
		if err := db.Model(target).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
}

func newKnowledgeBaseServiceTestEnv(t *testing.T) (*KnowledgeBaseService, *repository.Repositories, *gorm.DB) {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	repos := repository.NewRepositories(db)
	return NewKnowledgeBaseService(repos), repos, db
}

func createKnowledgeBaseForServiceTest(t *testing.T, svc *KnowledgeBaseService, userID int64, name string) *KnowledgeBaseResponse {
	t.Helper()
	kb, err := svc.Create(context.Background(), userID, CreateKnowledgeBaseRequest{Name: name})
	if err != nil {
		t.Fatalf("create KB: %v", err)
	}
	return kb
}

func createKnowledgeBaseDefaultProfileForServiceTest(t *testing.T, repos *repository.Repositories, userID int64, embeddingModel string) *model.UserAIProfile {
	t.Helper()
	profile := &model.UserAIProfile{
		UserID: userID, Name: "default", LLMProvider: "openai_compatible", LLMBaseURL: "https://llm.example/v1", LLMModel: "chat",
		ASRProvider: "mimo", ASRBaseURL: "https://asr.example/v1", ASRModel: "asr",
		EmbeddingProvider: "openai_compatible", EmbeddingEndpoint: "https://embed.example/v1", EmbeddingModel: embeddingModel, EmbeddingDim: 3,
		IsDefault: true,
	}
	if err := repos.AIProfile.Create(profile); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	return profile
}

func createKnowledgeBaseTaskForServiceTest(t *testing.T, repos *repository.Repositories, userID int64, suffix string) *model.VideoTask {
	t.Helper()
	task := &model.VideoTask{UserID: userID, FileMD5: fmt.Sprintf("%032s", suffix), Filename: suffix + ".mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task %s: %v", suffix, err)
	}
	return task
}

func createKnowledgeBaseRAGIndexForServiceTest(t *testing.T, repos *repository.Repositories, userID, taskID int64, embeddingModel, status string) {
	t.Helper()
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: userID, TaskID: taskID, EmbeddingModel: embeddingModel, EmbeddingDim: 3, Status: status, ChunkCount: 1}); err != nil {
		t.Fatalf("upsert RAG index: %v", err)
	}
}

func assertKnowledgeBaseVideoDataPreserved(t *testing.T, db *gorm.DB, taskID, chunkID int64) {
	t.Helper()
	var taskCount, chunkCount, indexCount int64
	if err := db.Model(&model.VideoTask{}).Where("id = ?", taskID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.VideoChunk{}).Where("id = ?", chunkID).Count(&chunkCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.VideoRAGIndex{}).Where("task_id = ?", taskID).Count(&indexCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || chunkCount != 1 || indexCount != 1 {
		t.Fatalf("preserved counts task/chunk/index = %d/%d/%d", taskCount, chunkCount, indexCount)
	}
}
