package service

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/model"
)

func TestUpdateTaskTitleOwnerScopedAndSanitized(t *testing.T) {
	repos := newMediaTestRepositories(t)
	svc := &MediaService{repo: repos}
	ctx := context.Background()
	task := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "raw.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.UpdateTaskTitle(ctx, 7, task.ID, "  \"ASR 错词标题\" \n ")
	if err != nil {
		t.Fatalf("UpdateTaskTitle() error = %v", err)
	}
	if updated.Title != "ASR 错词标题" {
		t.Fatalf("title = %q, want sanitized user title", updated.Title)
	}

	if _, err := svc.UpdateTaskTitle(ctx, 8, task.ID, "偷改"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-user error = %v, want ErrTaskNotFound", err)
	}
	if _, err := svc.UpdateTaskTitle(ctx, 7, task.ID+99, "不存在"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task error = %v, want ErrTaskNotFound", err)
	}
	if _, err := svc.UpdateTaskTitle(ctx, 7, task.ID, "   "); !errors.Is(err, ErrTaskTitleRequired) {
		t.Fatalf("blank title error = %v, want ErrTaskTitleRequired", err)
	}

	fresh, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Title != "ASR 错词标题" {
		t.Fatalf("persisted title = %q after rejected edits", fresh.Title)
	}
}
