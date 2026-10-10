package repository

import (
	"context"
	"errors"
	"testing"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

func TestTextSourceRefreshCancelsOnlyReplacedSummaryGenerationsAtomically(t *testing.T) {
	db := summaryRevisionDB(t)
	repos := NewRepositories(db)
	ctx := context.Background()
	task := createSourceTask(t, db, 551, 17)
	source, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: 17, TaskID: task.ID, Snapshot: sourceFixture(t, "旧来源。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{ID: "old-generation", UserID: 17, TaskID: task.ID, SubjectKind: model.AgentRunSubjectSummaryGeneration, SubjectID: "old-generation", ExecutionKind: "artifact", Status: model.AgentRunStatusRunning, PolicySnapshot: artifact.JSON(map[string]any{"source_id": source.ID}), Version: 1}
	if err = db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	chat := model.AgentRun{ID: "chat-generation", UserID: 17, TaskID: task.ID, SubjectKind: "chat_session", SubjectID: "chat", ExecutionKind: "chat", Status: model.AgentRunStatusRunning, Version: 1}
	if err = db.Create(&chat).Error; err != nil {
		t.Fatal(err)
	}
	req := PublishTextSourceRequest{UserID: 17, TaskID: task.ID, ExpectedActiveSourceID: source.ID, Snapshot: sourceFixture(t, "新来源。", 3000)}
	if _, err = repos.PublishTextSource(ctx, req, func(*Repositories, *model.VideoTextSource) error { return errors.New("fail downstream transaction") }); err == nil {
		t.Fatal("expected rollback")
	}
	db.First(&run, "id=?", run.ID)
	if run.Status != model.AgentRunStatusRunning {
		t.Fatal("rollback cancelled old run")
	}
	if _, err = repos.PublishTextSource(ctx, req, nil); err != nil {
		t.Fatal(err)
	}
	db.First(&run, "id=?", run.ID)
	db.First(&chat, "id=?", chat.ID)
	if run.Status != model.AgentRunStatusCancelled || run.StopReason != "source_changed" || chat.Status != model.AgentRunStatusRunning {
		t.Fatalf("cancel scope: summary=%+v chat=%+v", run, chat)
	}
	var count int64
	db.Model(&model.RunEvent{}).Where("run_id=? AND type=?", run.ID, "run.cancelled").Count(&count)
	if count != 1 {
		t.Fatalf("cancellation events=%d", count)
	}
}
