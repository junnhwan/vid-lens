package repository

import (
	"errors"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestKnowledgeBaseRepositoryIsOwnerScopedAndMembersAreIdempotent(t *testing.T) {
	db := newKnowledgeBaseTestDB(t)
	repo := NewKnowledgeBaseRepository(db)
	owner := createKnowledgeBaseUser(t, db, "kb-owner")
	other := createKnowledgeBaseUser(t, db, "kb-other")
	task3 := createKnowledgeBaseTask(t, db, owner.ID, "task-3")
	task5 := createKnowledgeBaseTask(t, db, owner.ID, "task-5")
	otherTask := createKnowledgeBaseTask(t, db, other.ID, "other-task")

	kb := &model.KnowledgeBase{UserID: owner.ID, Name: "  Course Notes  ", Description: "desc"}
	if err := repo.Create(kb); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if kb.Name != "Course Notes" {
		t.Fatalf("Create() name = %q, want trimmed name", kb.Name)
	}
	if got, err := repo.FindByIDForUser(other.ID, kb.ID); err != nil || got != nil {
		t.Fatalf("FindByIDForUser(other) = (%+v, %v), want (nil, nil)", got, err)
	}
	if err := repo.UpdateForUser(other.ID, &model.KnowledgeBase{ID: kb.ID, Name: "hijack"}); err == nil {
		t.Fatal("UpdateForUser(other) unexpectedly succeeded")
	}
	if _, err := repo.AddVideoForUser(owner.ID, kb.ID, task5.ID); err != nil {
		t.Fatalf("AddVideoForUser(task5) error = %v", err)
	}
	added, err := repo.AddVideoForUser(owner.ID, kb.ID, task5.ID)
	if err != nil {
		t.Fatalf("duplicate AddVideoForUser() error = %v", err)
	}
	if added {
		t.Fatal("duplicate AddVideoForUser() reported a new row")
	}
	if _, err := repo.AddVideoForUser(other.ID, kb.ID, otherTask.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user KB add error = %v, want gorm.ErrRecordNotFound", err)
	}
	if _, err := repo.AddVideoForUser(owner.ID, kb.ID, otherTask.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user task add error = %v, want gorm.ErrRecordNotFound", err)
	}
	if _, err := repo.AddVideoForUser(owner.ID, kb.ID, task3.ID); err != nil {
		t.Fatalf("AddVideoForUser(task3) error = %v", err)
	}

	ids, err := repo.ListMemberTaskIDsForUser(owner.ID, kb.ID)
	if err != nil {
		t.Fatalf("ListMemberTaskIDsForUser() error = %v", err)
	}
	if want := []int64{task3.ID, task5.ID}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("member task IDs = %v, want %v", ids, want)
	}
	count, err := repo.CountVideos(kb.ID)
	if err != nil || count != 2 {
		t.Fatalf("CountVideos() = (%d, %v), want (2, nil)", count, err)
	}
	if err := repo.RemoveVideoForUser(other.ID, kb.ID, task3.ID); err == nil {
		t.Fatal("RemoveVideoForUser(other) unexpectedly succeeded")
	}
	if err := repo.RemoveVideoForUser(owner.ID, kb.ID, task3.ID); err != nil {
		t.Fatalf("RemoveVideoForUser() error = %v", err)
	}
	if err := repo.DeleteForUser(other.ID, kb.ID); err == nil {
		t.Fatal("DeleteForUser(other) unexpectedly succeeded")
	}
	if err := repo.DeleteForUser(owner.ID, kb.ID); err != nil {
		t.Fatalf("DeleteForUser(owner) error = %v", err)
	}
	if got, err := repo.FindByIDForUser(owner.ID, kb.ID); err != nil || got != nil {
		t.Fatalf("deleted KB = (%+v, %v), want (nil, nil)", got, err)
	}
}

func TestChatMessageSourcesListAndTaskDeletionCleansKnowledgeBaseScope(t *testing.T) {
	db := newKnowledgeBaseTestDB(t)
	kbRepo := NewKnowledgeBaseRepository(db)
	chatRepo := NewChatRepository(db)
	user := createKnowledgeBaseUser(t, db, "source-owner")
	task1 := createKnowledgeBaseTask(t, db, user.ID, "source-task-1")
	task2 := createKnowledgeBaseTask(t, db, user.ID, "source-task-2")
	kb := &model.KnowledgeBase{UserID: user.ID, Name: "KB"}
	if err := kbRepo.Create(kb); err != nil {
		t.Fatalf("create KB: %v", err)
	}
	if _, err := kbRepo.AddVideoForUser(user.ID, kb.ID, task1.ID); err != nil {
		t.Fatalf("add task1: %v", err)
	}
	if _, err := kbRepo.AddVideoForUser(user.ID, kb.ID, task2.ID); err != nil {
		t.Fatalf("add task2: %v", err)
	}
	kbSession := &model.ChatSession{UserID: user.ID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID}
	videoSession := &model.ChatSession{UserID: user.ID, ScopeType: model.ChatScopeVideo, TaskID: task1.ID}
	if err := chatRepo.CreateSession(kbSession); err != nil {
		t.Fatalf("create KB session: %v", err)
	}
	if err := chatRepo.CreateSession(videoSession); err != nil {
		t.Fatalf("create video session: %v", err)
	}
	message := &model.ChatMessage{SessionID: kbSession.ID, UserID: user.ID, Role: "assistant", Content: "answer"}
	if err := chatRepo.CreateMessage(message); err != nil {
		t.Fatalf("create message: %v", err)
	}
	for _, taskID := range []int64{task2.ID, task1.ID, task1.ID} {
		if err := chatRepo.CreateMessageSource(&model.ChatMessageSource{MessageID: message.ID, SessionID: kbSession.ID, TaskID: taskID}); err != nil {
			t.Fatalf("create source task %d: %v", taskID, err)
		}
	}
	ids, err := chatRepo.ListSourceTaskIDsByMessageID(user.ID, message.ID)
	if err != nil {
		t.Fatalf("ListSourceTaskIDsByMessageID() error = %v", err)
	}
	if want := []int64{task1.ID, task2.ID}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("message source task IDs = %v, want %v", ids, want)
	}
	ids, err = chatRepo.ListSourceTaskIDsBySessionID(user.ID, kbSession.ID)
	if err != nil {
		t.Fatalf("ListSourceTaskIDsBySessionID() error = %v", err)
	}
	if want := []int64{task1.ID, task2.ID}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("session source task IDs = %v, want %v", ids, want)
	}
	if err := chatRepo.DeleteByTaskID(task1.ID); err != nil {
		t.Fatalf("DeleteByTaskID() error = %v", err)
	}
	var sourceCount, kbSessionCount, videoSessionCount, messageCount int64
	db.Model(&model.ChatMessageSource{}).Count(&sourceCount)
	db.Model(&model.ChatSession{}).Where("id = ?", kbSession.ID).Count(&kbSessionCount)
	db.Model(&model.ChatSession{}).Where("id = ?", videoSession.ID).Count(&videoSessionCount)
	db.Model(&model.ChatMessage{}).Where("id = ?", message.ID).Count(&messageCount)
	if sourceCount != 0 || kbSessionCount != 0 || videoSessionCount != 0 || messageCount != 0 {
		t.Fatalf("task cleanup counts source=%d kb_session=%d video_session=%d message=%d, want 0,0,0,0", sourceCount, kbSessionCount, videoSessionCount, messageCount)
	}
	if err := kbRepo.DeleteMembershipsByTaskID(task1.ID); err != nil {
		t.Fatalf("DeleteMembershipsByTaskID() error = %v", err)
	}
	count, err := kbRepo.CountVideos(kb.ID)
	if err != nil || count != 1 {
		t.Fatalf("remaining memberships = (%d, %v), want (1, nil)", count, err)
	}
}

func newKnowledgeBaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.VideoTask{}, &model.KnowledgeBase{}, &model.KnowledgeBaseVideo{},
		&model.ChatSession{}, &model.ChatMessage{}, &model.ChatMessageSource{},
	); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return db
}

func createKnowledgeBaseUser(t *testing.T, db *gorm.DB, username string) model.User {
	t.Helper()
	user := model.User{Username: username, PasswordHash: "hash"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func createKnowledgeBaseTask(t *testing.T, db *gorm.DB, userID int64, md5 string) model.VideoTask {
	t.Helper()
	task := model.VideoTask{UserID: userID, FileMD5: md5, Filename: md5 + ".mp4"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}
