package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

type summaryRepairChat struct {
	responses []string
	messages  [][]ai.ChatMessage
}

func (f *summaryRepairChat) Chat(_ context.Context, messages []ai.ChatMessage) (string, error) {
	f.messages = append(f.messages, append([]ai.ChatMessage(nil), messages...))
	index := len(f.messages) - 1
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	return f.responses[index], nil
}

func summaryRepairService(t *testing.T, base string, chat ai.ChatClient) (*SummaryRevisionService, *gorm.DB) {
	t.Helper()
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
	task := model.VideoTask{ID: 42, UserID: 7, FileMD5: "34343434343434343434343434343434", Filename: "course.mp4", Status: model.TaskStatusCompleted}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: base}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewSummaryRevisionService(repos, profiles, nil)
	svc.chat = chat
	return svc, db
}

func TestSummaryEditRepairsRepeatedTermAnchorsBeforePreview(t *testing.T) {
	base := "JEFF 是一个智能体框架。\n本节介绍 JEFF 的决策路由。"
	chat := &summaryRepairChat{responses: []string{
		artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "JEFF", NewText: "Jev"}}}),
		artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{
			{OldText: "JEFF 是一个智能体框架。", NewText: "Jev 是一个智能体框架。"},
			{OldText: "本节介绍 JEFF 的决策路由。", NewText: "本节介绍 Jev 的决策路由。"},
		}}),
	}}
	svc, _ := summaryRepairService(t, base, chat)
	ctx := context.Background()
	accepted, err := svc.Submit(ctx, 7, 42, "repair-repeated-term", SummaryEditInput{Instruction: "请将摘要中所有 JEFF 更正为 Jev", Mode: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteSummaryEdit(ctx, accepted.RunID); err != nil {
		t.Fatalf("repeated term correction should reach preview: %v", err)
	}
	preview, err := svc.Operation(ctx, 7, 42, accepted.ID)
	if err != nil || preview.Status != "proposed" || len(preview.Edits) != 2 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if effective, err := svc.Effective(ctx, 7, 42); err != nil || effective.Content != base || effective.Version != 0 {
		t.Fatalf("preview changed saved content: %+v, %v", effective, err)
	}
	if _, err = svc.Apply(ctx, 7, 42, accepted.ID, 0); err != nil {
		t.Fatal(err)
	}
	if effective, err := svc.Effective(ctx, 7, 42); err != nil || effective.Content != "Jev 是一个智能体框架。\n本节介绍 Jev 的决策路由。" || effective.Version != 1 {
		t.Fatalf("saved correction = %+v, %v", effective, err)
	}
	if len(chat.messages) != 2 || !strings.Contains(chat.messages[1][1].Content, "精确出现2次") {
		t.Fatalf("repair did not receive repeated anchor feedback: %+v", chat.messages)
	}
}

func TestSummaryEditRepairsCaseMismatchAndProtectsQuotes(t *testing.T) {
	for _, tc := range []struct {
		name, base, old, replacement, feedback, expected string
	}{
		{"case", "模型名称为 Jeff。", "jeff", "Jev", "精确出现0次", "模型名称为 Jev。"},
		{"quote", "模型名称为 Jeff。\n原话：“Jeff”。", "“Jeff”", "“Jev”", "protected_quote", "模型名称为 Jev。\n原话：“Jeff”。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat := &summaryRepairChat{responses: []string{
				artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(tc.base), Edits: []SummaryTextEdit{{OldText: tc.old, NewText: tc.replacement}}}),
				artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(tc.base), Edits: []SummaryTextEdit{{OldText: "模型名称为 Jeff。", NewText: "模型名称为 Jev。"}}}),
			}}
			svc, _ := summaryRepairService(t, tc.base, chat)
			view, err := svc.Edit(context.Background(), 7, 42, "repair-case-or-quote", SummaryEditInput{Instruction: "请将摘要叙述中的 Jeff 更正为 Jev，保留引文", Mode: "apply"})
			if err != nil || view.Status != "committed" {
				t.Fatalf("correction = %+v, %v", view, err)
			}
			if effective, err := svc.Effective(context.Background(), 7, 42); err != nil || effective.Content != tc.expected {
				t.Fatalf("wrong correction scope: %+v, %v", effective, err)
			}
			if len(chat.messages) != 2 || !strings.Contains(chat.messages[1][1].Content, tc.feedback) {
				t.Fatalf("missing feedback %q", tc.feedback)
			}
		})
	}
}

func TestSummaryEditRejectsInvalidRepairWithoutPublishing(t *testing.T) {
	base := "Jeff 是模型。Jeff 支持决策。"
	chat := &summaryRepairChat{responses: []string{artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "Jeff", NewText: "Jev"}}})}}
	svc, db := summaryRepairService(t, base, chat)
	_, err := svc.Edit(context.Background(), 7, 42, "repair-still-invalid", SummaryEditInput{Instruction: "请将摘要中的 Jeff 更正为 Jev", Mode: "apply"})
	var domain *artifact.Error
	if !errors.As(err, &domain) || domain.Code != "anchor_ambiguous" || len(chat.messages) != 2 {
		t.Fatalf("unbounded or unsafe repair: calls=%d err=%v", len(chat.messages), err)
	}
	if effective, err := svc.Effective(context.Background(), 7, 42); err != nil || effective.Content != base || effective.Version != 0 {
		t.Fatalf("failed repair published content: %+v, %v", effective, err)
	}
	var op model.SummaryEditOperation
	if err = db.Where("key=?", "repair-still-invalid").First(&op).Error; err != nil || op.Status != "failed" || op.ErrorCode != "anchor_ambiguous" {
		t.Fatalf("failure was not durable: %+v %v", op, err)
	}
}

func TestSummaryEditLiteralTermCorrectionIsCompleteWithoutModelRewriting(t *testing.T) {
	base := "Jeff 是模型。\nJeff 是模型。\nJEFF支持路由，Jefferson 保持原样。原话：“Jeff”。\n`Jeff` --flag\n> Jeff 原话\n"
	chat := &summaryChatFixture{fail: true}
	svc, db := summaryRepairService(t, base, chat)
	view, err := svc.Edit(context.Background(), 7, 42, "literal-scope", SummaryEditInput{Instruction: "jeff是Jev", Mode: "preview"})
	if err != nil || view.Status != "proposed" {
		t.Fatalf("correction = %+v, %v", view, err)
	}
	if _, err = svc.Apply(context.Background(), 7, 42, view.ID, 0); err != nil {
		t.Fatal(err)
	}
	effective, err := svc.Effective(context.Background(), 7, 42)
	if err != nil || effective.Content != "Jev 是模型。\nJev 是模型。\nJev支持路由，Jefferson 保持原样。原话：“Jeff”。\n`Jeff` --flag\n> Jeff 原话\n" || chat.calls != 0 {
		t.Fatalf("literal correction omitted a term or changed prose: %+v, calls=%d, err=%v", effective, chat.calls, err)
	}
	var run model.AgentRun
	if err = db.First(&run, "id=?", view.RunID).Error; err != nil || run.LLMCallsUsed != 0 {
		t.Fatalf("literal correction spent a model call: %+v, %v", run, err)
	}
	if _, err = svc.Undo(context.Background(), 7, 42, view.ID, 1); err != nil {
		t.Fatal(err)
	}
	if effective, err = svc.Effective(context.Background(), 7, 42); err != nil || effective.Content != base || effective.Version != 2 {
		t.Fatalf("literal correction undo = %+v, %v", effective, err)
	}
}

func TestSummaryEditRecoversValidatedRepairCheckpointWithoutNewCalls(t *testing.T) {
	base := "Jeff 是模型。Jeff 支持决策。"
	chat := &summaryRepairChat{responses: []string{
		artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "Jeff", NewText: "Jev"}}}),
		artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: base, NewText: "Jev 是模型。Jev 支持决策。"}}}),
	}}
	svc, _ := summaryRepairService(t, base, chat)
	ctx := context.Background()
	accepted, err := svc.Submit(ctx, 7, 42, "repair-checkpoint-recovery", SummaryEditInput{Instruction: "请将摘要中的 Jeff 更正为 Jev", Mode: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	op, err := svc.repos.SummaryRevision.Operation(ctx, 7, 42, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	var rules VideoTermRuleSet
	if err = json.Unmarshal([]byte(op.RuleSnapshotJSON), &rules); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.planSummaryPatch(ctx, op, rules, chat); err != nil {
		t.Fatal(err)
	}
	// The worker stops after durable model checkpoints but before publishing.
	if err = svc.ExecuteSummaryEdit(ctx, accepted.RunID); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Operation(ctx, 7, 42, accepted.ID)
	if err != nil || preview.Status != "proposed" || len(chat.messages) != 2 {
		t.Fatalf("repair recovery = %+v, calls=%d, err=%v", preview, len(chat.messages), err)
	}
}

func TestSummaryEditStillReadsLegacyPatchCheckpoints(t *testing.T) {
	base := "模型名称为 Jeff。"
	chat := &summaryRepairChat{responses: []string{artifact.JSON(SummaryTextPatch{BaseHash: artifact.Hash(base), Edits: []SummaryTextEdit{{OldText: "Jeff", NewText: "Jev"}}})}}
	svc, db := summaryRepairService(t, base, chat)
	ctx := context.Background()
	accepted, err := svc.Submit(ctx, 7, 42, "legacy-checkpoint-recovery", SummaryEditInput{Instruction: "jeff是Jev", Mode: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AgentRun{}).Where("id=?", accepted.RunID).Update("recipe_version", summaryEditRecipeV1).Error; err != nil {
		t.Fatal(err)
	}
	op, err := svc.repos.SummaryRevision.Operation(ctx, 7, 42, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	var rules VideoTermRuleSet
	if err = json.Unmarshal([]byte(op.RuleSnapshotJSON), &rules); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.planSummaryPatch(ctx, op, rules, chat); err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteSummaryEdit(ctx, accepted.RunID); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Operation(ctx, 7, 42, accepted.ID)
	if err != nil || preview.Status != "proposed" || len(chat.messages) != 1 {
		t.Fatalf("legacy recovery = %+v, calls=%d, err=%v", preview, len(chat.messages), err)
	}
}

func TestSummaryEditUsesFrozenAnchorIDsForDuplicateAndMarkdownLines(t *testing.T) {
	base := "  - *跨端与硬件控制*：CUA派生Jeff Use。\nJeff 是模型。\nJeff 是模型。\n原话：“Jeff”。"
	anchors := summaryEditAnchors(base)
	proposal := summaryAnchoredPatch{}
	for _, anchor := range anchors {
		if strings.Contains(anchor.Text, "Jeff") {
			proposal.Edits = append(proposal.Edits, struct {
				AnchorID string `json:"anchor_id"`
				NewText  string `json:"new_text"`
			}{anchor.ID, strings.ReplaceAll(anchor.Text, "Jeff", "Jev")})
		}
	}
	chat := &summaryRepairChat{responses: []string{`{"edits":[{"anchor_id":"unknown","new_text":"Jev"}]}`, artifact.JSON(proposal)}}
	svc, _ := summaryRepairService(t, base, chat)
	view, err := svc.Edit(context.Background(), 7, 42, "anchored-summary", SummaryEditInput{Instruction: "请将叙述中的 Jeff 更正为 Jev，保留原话引文", Mode: "apply"})
	if err != nil || view.Status != "committed" || len(chat.messages) != 2 {
		t.Fatalf("anchored patch = %+v, calls=%d, %v", view, len(chat.messages), err)
	}
	if effective, err := svc.Effective(context.Background(), 7, 42); err != nil || effective.Content != "  - *跨端与硬件控制*：CUA派生Jev Use。\nJev 是模型。\nJev 是模型。\n原话：“Jeff”。" {
		t.Fatalf("server anchor mapping changed formatting or protected evidence: %+v, %v", effective, err)
	}
}
