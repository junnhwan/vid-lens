package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

// Opt-in provider check. No credentials or raw provider payload are logged.
func TestSummaryEditRealModel(t *testing.T) {
	if os.Getenv("VIDLENS_SUMMARY_REAL_MODEL") != "1" {
		t.Skip("real model check is opt-in")
	}
	baseURL, key, modelName := os.Getenv("VIDLENS_LLM_BASE_URL"), os.Getenv("VIDLENS_LLM_API_KEY"), os.Getenv("VIDLENS_LLM_MODEL")
	if baseURL == "" || key == "" || modelName == "" {
		t.Skip("real model settings are unavailable")
	}
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	profiles := NewAIProfileService(repos.AIProfile, codec, nil)
	profile := validAIProfileRequest()
	profile.LLMBaseURL, profile.LLMAPIKey, profile.LLMModel, profile.LLMContextTokens = baseURL, key, modelName, 65536
	if _, err = profiles.Create(7, profile); err != nil {
		t.Fatal(err)
	}
	task := model.VideoTask{ID: 42, UserID: 7, FileMD5: "44444444444444444444444444444444", Filename: "test.mp4", Status: model.TaskStatusCompleted}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	base := "安装章节：讲师把工具名写成 BluePad。\n引文：讲师原话是“BluePad”。\n其他章节：保持不变。"
	if err = db.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: base}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewSummaryRevisionService(repos, profiles, ai.NewFactory())
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	accepted, err := svc.Submit(ctx, 7, 42, "real-model-summary", SummaryEditInput{Instruction: "只把安装章节叙述中的工具名 BluePad 改为 BluePi；引文保持原样，其他章节保持原样。", Mode: "apply"})
	if err != nil || accepted.Status != "running" {
		t.Fatalf("real model request was not accepted: %+v, %v", accepted, err)
	}
	if broker := os.Getenv("VIDLENS_SUMMARY_AMQP_URL"); broker != "" {
		workerCtx, stop := context.WithCancel(ctx)
		worker := mq.NewSummaryEditWorker(repos.SummaryRevision, svc, []string{broker})
		worker.Start(workerCtx)
		defer func() { stop(); worker.Wait() }()
		for {
			current, readErr := svc.Operation(ctx, 7, 42, accepted.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if current.Status != "running" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("summary MQ worker timed out")
			case <-time.After(500 * time.Millisecond):
			}
		}
	} else if err = svc.ExecuteSummaryEdit(ctx, accepted.RunID); err != nil {
		t.Fatalf("real model worker failed: %v", err)
	}
	view, err := svc.Operation(ctx, 7, 42, accepted.ID)
	if err != nil {
		t.Fatalf("real model edit failed: %v", err)
	}
	if view.Status != "committed" || view.ResultRevisionID == nil {
		t.Fatalf("real model did not commit: %+v", view)
	}
	effective, err := svc.Effective(ctx, 7, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(effective.Content, "安装章节：讲师把工具名写成 BluePi") || !strings.Contains(effective.Content, "讲师原话是“BluePad”") || !strings.Contains(effective.Content, "其他章节：保持不变。") {
		t.Fatalf("model changed wrong scope: %q", effective.Content)
	}
	var shared model.AISummary
	if err = db.Where("task_id = ?", 42).First(&shared).Error; err != nil || shared.Content != base {
		t.Fatalf("shared original changed: %v", err)
	}
	t.Logf("real model committed revision %s with %d local edits; shared generated cache unchanged", *view.ResultRevisionID, len(view.Edits))
}
