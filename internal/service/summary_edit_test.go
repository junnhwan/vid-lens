package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

type summaryChatFixture struct {
	response string
	calls    int
	fail     bool
	messages []ai.ChatMessage
}

type interruptibleSummaryChat struct {
	started  chan struct{}
	response string
	calls    int
}

func (f *interruptibleSummaryChat) Chat(ctx context.Context, _ []ai.ChatMessage) (string, error) {
	f.calls++
	if f.calls == 1 {
		close(f.started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.response, nil
}

func (f *summaryChatFixture) Chat(_ context.Context, messages []ai.ChatMessage) (string, error) {
	f.calls++
	f.messages = messages
	if f.fail {
		return "", errors.New("temporary provider failure")
	}
	return f.response, nil
}

func TestSummaryEditPreviewApplyUndoAndFailureRetry(t *testing.T) {
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
	if _, err = profiles.Create(7, validAIProfileRequest()); err != nil {
		t.Fatal(err)
	}
	task := model.VideoTask{ID: 42, UserID: 7, FileMD5: "33333333333333333333333333333333", Filename: "course.mp4", Status: model.TaskStatusCompleted}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	base := "安装章节使用旧名称。保留后续内容。"
	if err = db.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: base}).Error; err != nil {
		t.Fatal(err)
	}
	chat := &summaryChatFixture{response: artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "安装章节使用旧名称。", NewText: "安装章节使用新名称。"}}})}
	svc := NewSummaryRevisionService(repos, profiles, nil)
	svc.chat = chat
	ctx := context.Background()
	chat.fail = true
	if _, err = svc.Edit(ctx, 7, 42, "failure-key", SummaryEditInput{Instruction: "纠正安装章节", Mode: "preview"}); err == nil {
		t.Fatal("provider failure was accepted")
	}
	var failed model.SummaryEditOperation
	if err = db.Where("user_id = ? AND key = ?", 7, "failure-key").First(&failed).Error; err != nil || failed.Status != "failed" {
		t.Fatalf("failure was not durable: %+v, %v", failed, err)
	}
	chat.fail = false
	input := SummaryEditInput{Instruction: "纠正安装章节", Mode: "preview"}
	accepted, err := svc.Submit(ctx, 7, 42, "preview-key", input)
	if err != nil || accepted.Status != "running" {
		t.Fatalf("submit = %+v, %v", accepted, err)
	}
	if err = svc.ExecuteSummaryEdit(ctx, accepted.RunID); err != nil {
		t.Fatalf("background execute: %v", err)
	}
	preview, err := svc.Operation(ctx, 7, 42, accepted.ID)
	if err != nil || preview.Status != "proposed" || len(preview.Edits) != 1 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if effective, err := svc.Effective(ctx, 7, 42); err != nil || effective.Version != 0 || effective.Content != base {
		t.Fatalf("preview published: %+v, %v", effective, err)
	}
	if again, err := svc.Submit(ctx, 7, 42, "preview-key", input); err != nil || again.ID != preview.ID || chat.calls != 2 {
		t.Fatalf("preview replay = %+v, calls=%d, err=%v", again, chat.calls, err)
	}
	committed, err := svc.Apply(ctx, 7, 42, preview.ID, 0)
	if err != nil || committed.Status != "committed" || committed.ResultRevisionID == nil {
		t.Fatalf("apply = %+v, %v", committed, err)
	}
	if _, err := svc.Apply(ctx, 7, 42, preview.ID, 0); err != nil {
		t.Fatalf("apply replay: %v", err)
	}
	effective, err := svc.Effective(ctx, 7, 42)
	if err != nil || effective.Version != 1 || effective.Content != "安装章节使用新名称。保留后续内容。" {
		t.Fatalf("effective = %+v, %v", effective, err)
	}
	detail, err := (&MediaService{repo: repos}).GetTaskDetail(ctx, 7, 42)
	if err != nil || detail.Summary == nil || detail.Summary.Content != effective.Content || detail.SummaryRevision == nil || detail.SummaryRevision.Version != 1 {
		t.Fatalf("page detail did not read effective revision: %+v, %v", detail, err)
	}
	contextText, err := (&ChatService{repos: repos}).videoContextText(7, 42)
	if err != nil || !strings.Contains(contextText, "安装章节使用新名称") || strings.Contains(contextText, "安装章节使用旧名称") {
		t.Fatalf("agent context did not read effective revision: %q, %v", contextText, err)
	}
	undone, err := svc.Undo(ctx, 7, 42, preview.ID, 1)
	if err != nil || undone.UndoRevisionID == nil {
		t.Fatalf("undo = %+v, %v", undone, err)
	}
	effective, err = svc.Effective(ctx, 7, 42)
	if err != nil || effective.Version != 2 || effective.Content != base {
		t.Fatalf("undo effective = %+v, %v", effective, err)
	}
	if chat.calls != 2 {
		t.Fatal(fmt.Sprintf("unexpected extra model calls: %d", chat.calls))
	}
	ruleInput := VideoTermRuleInput{ExpectedVersion: 0, LinkedOperationID: committed.ID, From: "旧名称", To: "新名称", Context: "安装章节中的产品名称", Exclusions: []string{"原话引用", "命令参数"}}
	rules, err := SaveVideoTermRule(ctx, repos, 7, 42, ruleInput)
	if err != nil || rules.Version != 1 || len(rules.Rules) != 1 || rules.Rules[0].Basis != "user_asserted" {
		t.Fatalf("save rule = %+v, %v", rules, err)
	}
	ruleInput.ExpectedVersion = 1
	replay, err := SaveVideoTermRule(ctx, repos, 7, 42, ruleInput)
	if err != nil || replay.Version != 1 {
		t.Fatalf("linked rule retry was not idempotent: %+v, %v", replay, err)
	}
	disabled, err := DisableVideoTermRule(ctx, repos, 7, 42, rules.Rules[0].ID, 1)
	if err != nil || disabled.Version != 2 || disabled.Rules[0].Enabled {
		t.Fatalf("disable = %+v, %v", disabled, err)
	}
	frozen, err := repos.VideoTermRule.Version(ctx, 7, 42, 1)
	if err != nil || frozen == nil {
		t.Fatalf("frozen rule unavailable: %+v, %v", frozen, err)
	}
	if _, err = SaveVideoTermRule(ctx, repos, 7, 42, ruleInput); err == nil {
		t.Fatal("disabled linked rule was re-enabled by retry")
	}
}

func TestSummaryEditWorkerInterruptionResumesFrozenOperation(t *testing.T) {
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
	if _, err = profiles.Create(7, validAIProfileRequest()); err != nil {
		t.Fatal(err)
	}
	task := model.VideoTask{ID: 45, UserID: 7, FileMD5: "99999999999999999999999999999999", Filename: "course.mp4", Status: model.TaskStatusCompleted}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	base := "安装章节使用旧名称。保留后续内容。"
	if err = db.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: base}).Error; err != nil {
		t.Fatal(err)
	}
	rules, err := SaveVideoTermRule(context.Background(), repos, 7, 45, VideoTermRuleInput{ExpectedVersion: 0, From: "旧名称", To: "新名称", Context: "安装章节"})
	if err != nil {
		t.Fatal(err)
	}
	chat := &interruptibleSummaryChat{started: make(chan struct{}), response: artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "安装章节使用旧名称。", NewText: "安装章节使用新名称。"}}})}
	svc := NewSummaryRevisionService(repos, profiles, nil)
	svc.chat = chat
	accepted, err := svc.Submit(context.Background(), 7, 45, "interrupt-key", SummaryEditInput{Instruction: "修改安装章节名称", ExpectedRevision: 0, Mode: "apply"})
	if err != nil || accepted.Status != "running" {
		t.Fatalf("submit = %+v, %v", accepted, err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- svc.ExecuteSummaryEdit(workerCtx, accepted.RunID) }()
	select {
	case <-chat.started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never reached model")
	}
	cancel()
	select {
	case err = <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted worker: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	still, err := svc.Operation(context.Background(), 7, 45, accepted.ID)
	if err != nil || still.Status != "running" {
		t.Fatalf("interrupted operation became terminal: %+v, %v", still, err)
	}
	if _, err = DisableVideoTermRule(context.Background(), repos, 7, 45, rules.Rules[0].ID, rules.Version); err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteSummaryEdit(context.Background(), accepted.RunID); err != nil {
		t.Fatalf("recovery execution: %v", err)
	}
	complete, err := svc.Operation(context.Background(), 7, 45, accepted.ID)
	if err != nil || complete.Status != "committed" || complete.RuleVersion != rules.Version || chat.calls != 2 {
		t.Fatalf("recovered operation = %+v, calls=%d, err=%v", complete, chat.calls, err)
	}
	var count int64
	if err = db.Model(&model.SummaryRevision{}).Where("user_id = ? AND task_id = ?", 7, 45).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("revision count = %d, %v", count, err)
	}
}

func TestSummaryEditResumesWithFrozenRulesAfterDisable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	ctx := context.Background()
	task := model.VideoTask{ID: 43, UserID: 7, FileMD5: "55555555555555555555555555555555", Filename: "course.mp4", Status: model.TaskStatusCompleted}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	base := "安装章节使用旧名称。"
	if err = db.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: base}).Error; err != nil {
		t.Fatal(err)
	}
	rules, err := SaveVideoTermRule(ctx, repos, 7, 43, VideoTermRuleInput{ExpectedVersion: 0, From: "旧名称", To: "新名称", Context: "安装章节"})
	if err != nil || rules.Version != 1 {
		t.Fatalf("initial rule = %+v, %v", rules, err)
	}
	now := time.Now().UTC()
	op := &model.SummaryEditOperation{ID: "frozen-summary-op", UserID: 7, TaskID: 43, Key: "frozen-summary-key", RequestHash: artifact.Hash("frozen-request"), RunID: "frozen-summary-run", Mode: "apply", Status: "running", Instruction: "改正名称", BaseVersion: 0, BaseContentHash: artifact.Hash(base), BaseContent: base, BaseGeneratedHash: artifact.Hash(base), RuleVersion: rules.Version, RuleDigest: rules.Digest, RuleSnapshotJSON: artifact.JSON(rules), PatchJSON: "{}", CreatedAt: now, UpdatedAt: now}
	run := &model.AgentRun{ID: op.RunID, UserID: 7, SubjectKind: model.AgentRunSubjectSummaryEdit, SubjectID: op.ID, ExecutionKind: "artifact", RecipeVersion: summaryEditRecipe, ScopeType: "video", TaskID: 43, Goal: op.Instruction, Mode: op.Mode, AgentProfile: "default", ProfileSnapshot: "{}", PolicySnapshot: "{}", BudgetSnapshot: "{}", Status: model.AgentRunStatusRunning, Version: 1, MaxSteps: 1, MaxLLMCalls: 1, MaxAttemptsPerStep: 2, MaxDurationMs: 60_000, CreatedAt: now, UpdatedAt: now}
	if _, err = repos.SummaryRevision.Begin(ctx, op, run); err != nil {
		t.Fatal(err)
	}
	if _, err = DisableVideoTermRule(ctx, repos, 7, 43, rules.Rules[0].ID, 1); err != nil {
		t.Fatal(err)
	}
	chat := &summaryChatFixture{response: artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: base, NewText: "安装章节使用新名称。"}}})}
	svc := NewSummaryRevisionService(repos, nil, nil)
	svc.chat = chat
	view, err := svc.execute(ctx, op)
	if err != nil || view.Status != "committed" || view.RuleVersion != 1 {
		t.Fatalf("frozen edit = %+v, %v", view, err)
	}
	if len(chat.messages) < 2 || !strings.Contains(chat.messages[1].Content, `"enabled":true`) {
		t.Fatal("frozen rule not passed to model")
	}
	latest, err := EffectiveTermRules(ctx, repos, 7, 43)
	if err != nil || latest.Version != 2 || latest.Rules[0].Enabled {
		t.Fatalf("new run would reuse disabled rule: %+v, %v", latest, err)
	}
}

func TestTermRuleEvidenceDowngradesWhenSourceCannotBeVerified(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	if err = db.Create(&model.VideoTask{ID: 44, UserID: 7, FileMD5: "77777777777777777777777777777777", Filename: "course.mp4"}).Error; err != nil {
		t.Fatal(err)
	}
	stored := []VideoTermRule{{ID: "evidence-rule", From: "旧名", To: "新名", Context: "画面标题", Exclusions: []string{}, Basis: "evidence_supported", TranscriptEvidenceID: "transcript-1", VisualEvidenceID: "visual-1", SourceHash: "old-source", Enabled: true, EvidenceStatus: "current"}}
	if _, err = repos.VideoTermRule.Save(context.Background(), 7, 44, 0, artifact.Hash(artifact.JSON(stored)), artifact.JSON(stored), nil); err != nil {
		t.Fatal(err)
	}
	effective, err := EffectiveTermRules(context.Background(), repos, 7, 44)
	if err != nil || effective.Rules[0].EvidenceStatus != "pending_review" || effective.Rules[0].Basis != "evidence_supported" {
		t.Fatalf("stale evidence was still presented as current: %+v, %v", effective, err)
	}
}
