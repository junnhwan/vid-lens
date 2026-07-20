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
	count, err := repo.CountMembersForUser(owner.ID, kb.ID)
	if err != nil || count != 2 {
		t.Fatalf("CountMembersForUser() = (%d, %v), want (2, nil)", count, err)
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
		if err := chatRepo.CreateMessageSource(user.ID, &model.ChatMessageSource{MessageID: message.ID, SessionID: kbSession.ID, TaskID: taskID}); err != nil {
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
	count, err := kbRepo.CountMembersForUser(user.ID, kb.ID)
	if err != nil || count != 1 {
		t.Fatalf("remaining memberships = (%d, %v), want (1, nil)", count, err)
	}
}

func TestCreateExchangeRevalidatesCurrentScopeBeforeWriting(t *testing.T) {
	t.Run("video task deleted with empty sources", func(t *testing.T) {
		db := newKnowledgeBaseTestDB(t)
		user := createKnowledgeBaseUser(t, db, "exchange-video-owner")
		task := createKnowledgeBaseTask(t, db, user.ID, "exchange-video-task")
		repo := NewChatRepository(db)
		session := &model.ChatSession{UserID: user.ID, ScopeType: model.ChatScopeVideo, TaskID: task.ID}
		if err := repo.CreateSession(session); err != nil {
			t.Fatal(err)
		}
		if err := db.Delete(&model.VideoTask{}, task.ID).Error; err != nil {
			t.Fatal(err)
		}
		err := repo.CreateExchange(user.ID,
			&model.ChatMessage{SessionID: session.ID, UserID: user.ID, Role: "user", Content: "q"},
			&model.ChatMessage{SessionID: session.ID, UserID: user.ID, Role: "assistant", Content: "a"},
			nil,
		)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("CreateExchange() error = %v, want gorm.ErrRecordNotFound", err)
		}
		assertChatMessageCount(t, db, session.ID, 0)
	})

	t.Run("knowledge base deleted with empty sources", func(t *testing.T) {
		db := newKnowledgeBaseTestDB(t)
		user := createKnowledgeBaseUser(t, db, "exchange-kb-owner")
		kb := &model.KnowledgeBase{UserID: user.ID, Name: "exchange-kb"}
		if err := NewKnowledgeBaseRepository(db).Create(kb); err != nil {
			t.Fatal(err)
		}
		repo := NewChatRepository(db)
		session := &model.ChatSession{UserID: user.ID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID}
		if err := repo.CreateSession(session); err != nil {
			t.Fatal(err)
		}
		if err := db.Delete(&model.KnowledgeBase{}, kb.ID).Error; err != nil {
			t.Fatal(err)
		}
		err := repo.CreateExchange(user.ID,
			&model.ChatMessage{SessionID: session.ID, UserID: user.ID, Role: "user", Content: "q"},
			&model.ChatMessage{SessionID: session.ID, UserID: user.ID, Role: "assistant", Content: "a"},
			nil,
		)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("CreateExchange() error = %v, want gorm.ErrRecordNotFound", err)
		}
		assertChatMessageCount(t, db, session.ID, 0)
	})

	t.Run("knowledge base source removed before write", func(t *testing.T) {
		db := newKnowledgeBaseTestDB(t)
		user := createKnowledgeBaseUser(t, db, "exchange-member-owner")
		task := createKnowledgeBaseTask(t, db, user.ID, "exchange-member-task")
		kbRepo := NewKnowledgeBaseRepository(db)
		kb := &model.KnowledgeBase{UserID: user.ID, Name: "exchange-member-kb"}
		if err := kbRepo.Create(kb); err != nil {
			t.Fatal(err)
		}
		if _, err := kbRepo.AddVideoForUser(user.ID, kb.ID, task.ID); err != nil {
			t.Fatal(err)
		}
		repo := NewChatRepository(db)
		session := &model.ChatSession{UserID: user.ID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID}
		if err := repo.CreateSession(session); err != nil {
			t.Fatal(err)
		}
		if err := kbRepo.RemoveVideoForUser(user.ID, kb.ID, task.ID); err != nil {
			t.Fatal(err)
		}
		err := repo.CreateExchange(user.ID,
			&model.ChatMessage{SessionID: session.ID, UserID: user.ID, Role: "user", Content: "q"},
			&model.ChatMessage{SessionID: session.ID, UserID: user.ID, Role: "assistant", Content: "a"},
			[]int64{task.ID},
		)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("CreateExchange() error = %v, want gorm.ErrRecordNotFound", err)
		}
		assertChatMessageCount(t, db, session.ID, 0)
	})
}

func assertChatMessageCount(t *testing.T, db *gorm.DB, sessionID, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.ChatMessage{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("chat message count = %d, want %d", count, want)
	}
}

func newKnowledgeBaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.VideoTask{}, &model.VideoChunk{}, &model.KnowledgeBase{}, &model.KnowledgeBaseVideo{},
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

func TestKnowledgeBaseMemberCountLocksOwnerRowAndDoesNotLeakCrossUserCount(t *testing.T) {
	db := newKnowledgeBaseTestDB(t)
	owner := createKnowledgeBaseUser(t, db, "count-owner")
	other := createKnowledgeBaseUser(t, db, "count-other")
	task1 := createKnowledgeBaseTask(t, db, owner.ID, "count-task-1")
	task2 := createKnowledgeBaseTask(t, db, owner.ID, "count-task-2")
	kb := &model.KnowledgeBase{UserID: owner.ID, Name: "count-kb"}
	if err := NewKnowledgeBaseRepository(db).Create(kb); err != nil {
		t.Fatalf("create KB: %v", err)
	}
	repo := NewKnowledgeBaseRepository(db)
	for _, taskID := range []int64{task1.ID, task2.ID} {
		if _, err := repo.AddVideoForUser(owner.ID, kb.ID, taskID); err != nil {
			t.Fatalf("add member %d: %v", taskID, err)
		}
	}
	if count, err := repo.CountMembersForUser(other.ID, kb.ID); err != nil || count != 0 {
		t.Fatalf("cross-user CountMembersForUser() = (%d, %v), want (0, nil)", count, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		txRepo := NewKnowledgeBaseRepository(tx)
		locked, count, err := txRepo.LockForUpdateAndCountMembers(owner.ID, kb.ID)
		if err != nil {
			return err
		}
		if locked == nil || locked.ID != kb.ID || count != 2 {
			t.Fatalf("LockForUpdateAndCountMembers() = (%+v, %d), want KB %d and count 2", locked, count, kb.ID)
		}
		wrongOwner, wrongCount, err := txRepo.LockForUpdateAndCountMembers(other.ID, kb.ID)
		if err != nil {
			return err
		}
		if wrongOwner != nil || wrongCount != 0 {
			t.Fatalf("cross-user lock/count = (%+v, %d), want (nil, 0)", wrongOwner, wrongCount)
		}
		return nil
	}); err != nil {
		t.Fatalf("transactional lock/count error = %v", err)
	}
}

func TestKnowledgeBaseDeleteCleansOnlyKnowledgeBaseChatDataAndKeepsTasksAndChunks(t *testing.T) {
	db := newKnowledgeBaseTestDB(t)
	owner := createKnowledgeBaseUser(t, db, "delete-owner")
	task := createKnowledgeBaseTask(t, db, owner.ID, "delete-task")
	chunk := &model.VideoChunk{
		UserID: owner.ID, TaskID: task.ID, ChunkIndex: 0, Content: "chunk", ContentHash: "01234567890123456789012345678901",
		EmbeddingModel: "embed", EmbeddingDim: 3, VectorID: "vector-delete",
	}
	if err := db.Create(chunk).Error; err != nil {
		t.Fatalf("create chunk: %v", err)
	}
	kb := &model.KnowledgeBase{UserID: owner.ID, Name: "delete-kb"}
	kbRepo := NewKnowledgeBaseRepository(db)
	if err := kbRepo.Create(kb); err != nil {
		t.Fatalf("create KB: %v", err)
	}
	if _, err := kbRepo.AddVideoForUser(owner.ID, kb.ID, task.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	chatRepo := NewChatRepository(db)
	kbSession := &model.ChatSession{UserID: owner.ID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID}
	videoSession := &model.ChatSession{UserID: owner.ID, ScopeType: model.ChatScopeVideo, TaskID: task.ID}
	if err := chatRepo.CreateSession(kbSession); err != nil {
		t.Fatalf("create KB session: %v", err)
	}
	if err := chatRepo.CreateSession(videoSession); err != nil {
		t.Fatalf("create video session: %v", err)
	}
	message := &model.ChatMessage{SessionID: kbSession.ID, UserID: owner.ID, Role: "assistant", Content: "answer"}
	if err := chatRepo.CreateMessage(message); err != nil {
		t.Fatalf("create KB message: %v", err)
	}
	if err := chatRepo.CreateMessageSource(owner.ID, &model.ChatMessageSource{MessageID: message.ID, SessionID: kbSession.ID, TaskID: task.ID}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	if err := kbRepo.DeleteForUser(owner.ID, kb.ID); err != nil {
		t.Fatalf("DeleteForUser() error = %v", err)
	}
	var kbCount, membershipCount, kbSessionCount, messageCount, sourceCount, videoSessionCount, taskCount, chunkCount int64
	db.Model(&model.KnowledgeBase{}).Where("id = ?", kb.ID).Count(&kbCount)
	db.Model(&model.KnowledgeBaseVideo{}).Where("knowledge_base_id = ?", kb.ID).Count(&membershipCount)
	db.Model(&model.ChatSession{}).Where("id = ?", kbSession.ID).Count(&kbSessionCount)
	db.Model(&model.ChatMessage{}).Where("id = ?", message.ID).Count(&messageCount)
	db.Model(&model.ChatMessageSource{}).Where("message_id = ?", message.ID).Count(&sourceCount)
	db.Model(&model.ChatSession{}).Where("id = ?", videoSession.ID).Count(&videoSessionCount)
	db.Model(&model.VideoTask{}).Where("id = ?", task.ID).Count(&taskCount)
	db.Model(&model.VideoChunk{}).Where("id = ?", chunk.ID).Count(&chunkCount)
	if kbCount != 0 || membershipCount != 0 || kbSessionCount != 0 || messageCount != 0 || sourceCount != 0 || videoSessionCount != 1 || taskCount != 1 || chunkCount != 1 {
		t.Fatalf("delete counts kb=%d membership=%d kb_session=%d message=%d source=%d video_session=%d task=%d chunk=%d; want 0,0,0,0,0,1,1,1", kbCount, membershipCount, kbSessionCount, messageCount, sourceCount, videoSessionCount, taskCount, chunkCount)
	}
}

func TestChatMessageSourceCreateIsOwnerSafeAndRequiresConsistentEdges(t *testing.T) {
	db := newKnowledgeBaseTestDB(t)
	owner := createKnowledgeBaseUser(t, db, "source-owner-safe")
	other := createKnowledgeBaseUser(t, db, "source-other-safe")
	ownerTask := createKnowledgeBaseTask(t, db, owner.ID, "source-owner-task")
	otherTask := createKnowledgeBaseTask(t, db, other.ID, "source-other-task")
	ownerSession := &model.ChatSession{UserID: owner.ID, ScopeType: model.ChatScopeVideo, TaskID: ownerTask.ID}
	otherSession := &model.ChatSession{UserID: other.ID, ScopeType: model.ChatScopeVideo, TaskID: otherTask.ID}
	chatRepo := NewChatRepository(db)
	if err := chatRepo.CreateSession(ownerSession); err != nil {
		t.Fatalf("create owner session: %v", err)
	}
	if err := chatRepo.CreateSession(otherSession); err != nil {
		t.Fatalf("create other session: %v", err)
	}
	ownerMessage := &model.ChatMessage{SessionID: ownerSession.ID, UserID: owner.ID, Role: "assistant", Content: "owner"}
	otherMessage := &model.ChatMessage{SessionID: otherSession.ID, UserID: other.ID, Role: "assistant", Content: "other"}
	if err := chatRepo.CreateMessage(ownerMessage); err != nil {
		t.Fatalf("create owner message: %v", err)
	}
	if err := chatRepo.CreateMessage(otherMessage); err != nil {
		t.Fatalf("create other message: %v", err)
	}
	valid := &model.ChatMessageSource{MessageID: ownerMessage.ID, SessionID: ownerSession.ID, TaskID: ownerTask.ID}
	if err := chatRepo.CreateMessageSource(owner.ID, valid); err != nil {
		t.Fatalf("valid source: %v", err)
	}
	invalid := []struct {
		name   string
		userID int64
		source model.ChatMessageSource
	}{
		{"wrong user", other.ID, *valid},
		{"wrong task owner", owner.ID, model.ChatMessageSource{MessageID: ownerMessage.ID, SessionID: ownerSession.ID, TaskID: otherTask.ID}},
		{"message/session mismatch", owner.ID, model.ChatMessageSource{MessageID: ownerMessage.ID, SessionID: otherSession.ID, TaskID: ownerTask.ID}},
		{"message owner mismatch", owner.ID, model.ChatMessageSource{MessageID: otherMessage.ID, SessionID: ownerSession.ID, TaskID: ownerTask.ID}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := chatRepo.CreateMessageSource(tc.userID, &tc.source); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("CreateMessageSource() error = %v, want gorm.ErrRecordNotFound", err)
			}
		})
	}
	var sourceCount int64
	db.Model(&model.ChatMessageSource{}).Count(&sourceCount)
	if sourceCount != 1 {
		t.Fatalf("source rows after rejected writes = %d, want 1", sourceCount)
	}
}

func TestKnowledgeBaseMemberQueriesExcludeDeletedTasksConsistently(t *testing.T) {
	db := newKnowledgeBaseTestDB(t)
	owner := createKnowledgeBaseUser(t, db, "deleted-member-owner")
	task1 := createKnowledgeBaseTask(t, db, owner.ID, "deleted-member-task-1")
	task2 := createKnowledgeBaseTask(t, db, owner.ID, "deleted-member-task-2")
	kb := &model.KnowledgeBase{UserID: owner.ID, Name: "deleted-member-kb"}
	repo := NewKnowledgeBaseRepository(db)
	if err := repo.Create(kb); err != nil {
		t.Fatalf("create KB: %v", err)
	}
	for _, taskID := range []int64{task1.ID, task2.ID} {
		if _, err := repo.AddVideoForUser(owner.ID, kb.ID, taskID); err != nil {
			t.Fatalf("add member %d: %v", taskID, err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		locked, count, err := NewKnowledgeBaseRepository(tx).LockForUpdateAndCountMembers(owner.ID, kb.ID)
		if err != nil {
			return err
		}
		if locked == nil || count != 2 {
			t.Fatalf("initial locked count = (%+v, %d), want KB and 2", locked, count)
		}
		return nil
	}); err != nil {
		t.Fatalf("initial locked count: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Where("id = ?", task2.ID).Delete(&model.VideoTask{}).Error
	}); err != nil {
		t.Fatalf("soft-delete member task: %v", err)
	}
	count, err := repo.CountMembersForUser(owner.ID, kb.ID)
	if err != nil || count != 1 {
		t.Fatalf("CountMembersForUser() = (%d, %v), want (1, nil)", count, err)
	}
	ids, err := repo.ListMemberTaskIDsForUser(owner.ID, kb.ID)
	if err != nil {
		t.Fatalf("ListMemberTaskIDsForUser() error = %v", err)
	}
	if want := []int64{task1.ID}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ListMemberTaskIDsForUser() = %v, want %v", ids, want)
	}
}

func TestChatMessageSourceCreateEnforcesSessionScopeAndKnowledgeBaseMembership(t *testing.T) {
	db := newKnowledgeBaseTestDB(t)
	owner := createKnowledgeBaseUser(t, db, "scope-owner")
	task1 := createKnowledgeBaseTask(t, db, owner.ID, "scope-task-1")
	task2 := createKnowledgeBaseTask(t, db, owner.ID, "scope-task-2")
	kb := &model.KnowledgeBase{UserID: owner.ID, Name: "scope-kb"}
	kbRepo := NewKnowledgeBaseRepository(db)
	if err := kbRepo.Create(kb); err != nil {
		t.Fatalf("create KB: %v", err)
	}
	if _, err := kbRepo.AddVideoForUser(owner.ID, kb.ID, task1.ID); err != nil {
		t.Fatalf("add KB member: %v", err)
	}
	chatRepo := NewChatRepository(db)
	videoSession := &model.ChatSession{UserID: owner.ID, ScopeType: model.ChatScopeVideo, TaskID: task1.ID}
	kbSession := &model.ChatSession{UserID: owner.ID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID}
	if err := chatRepo.CreateSession(videoSession); err != nil {
		t.Fatalf("create video session: %v", err)
	}
	if err := chatRepo.CreateSession(kbSession); err != nil {
		t.Fatalf("create KB session: %v", err)
	}
	videoMessage := &model.ChatMessage{SessionID: videoSession.ID, UserID: owner.ID, Role: "assistant", Content: "video"}
	kbMessage := &model.ChatMessage{SessionID: kbSession.ID, UserID: owner.ID, Role: "assistant", Content: "kb"}
	if err := chatRepo.CreateMessage(videoMessage); err != nil {
		t.Fatalf("create video message: %v", err)
	}
	if err := chatRepo.CreateMessage(kbMessage); err != nil {
		t.Fatalf("create KB message: %v", err)
	}
	if err := chatRepo.CreateMessageSource(owner.ID, &model.ChatMessageSource{MessageID: videoMessage.ID, SessionID: videoSession.ID, TaskID: task1.ID}); err != nil {
		t.Fatalf("valid video source: %v", err)
	}
	if err := chatRepo.CreateMessageSource(owner.ID, &model.ChatMessageSource{MessageID: kbMessage.ID, SessionID: kbSession.ID, TaskID: task1.ID}); err != nil {
		t.Fatalf("valid KB source: %v", err)
	}
	invalid := []struct {
		name   string
		source model.ChatMessageSource
	}{
		{"video task mismatch", model.ChatMessageSource{MessageID: videoMessage.ID, SessionID: videoSession.ID, TaskID: task2.ID}},
		{"knowledge-base non-member", model.ChatMessageSource{MessageID: kbMessage.ID, SessionID: kbSession.ID, TaskID: task2.ID}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := chatRepo.CreateMessageSource(owner.ID, &tc.source); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("CreateMessageSource() error = %v, want gorm.ErrRecordNotFound", err)
			}
		})
	}
	var sourceCount int64
	db.Model(&model.ChatMessageSource{}).Count(&sourceCount)
	if sourceCount != 2 {
		t.Fatalf("source rows after scope rejection = %d, want 2", sourceCount)
	}
}
