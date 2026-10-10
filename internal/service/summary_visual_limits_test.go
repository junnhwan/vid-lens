package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/summarydoc"
)

func threeTargetVisualFixture(t *testing.T) (*generationFixture, *summaryVisualFixture, []summaryVisualTarget) {
	t.Helper()
	f := newGenerationFixture(t, false)
	v := enableGenerationVisualFixture(t, f)
	var frozen processing.GenerationSnapshot
	_ = json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen)
	budget, _ := config.DefaultAgentBudgetConfig().Resolve(nil)
	frames := 2
	budget.Values.MaxVisualFrames = &frames
	budget.Values.MaxInputTokens = 24000
	budget.Values.MaxOutputTokens = 8192
	frozen.Intent.BudgetJSON = artifact.JSON(budget)
	f.job.InputSnapshotJSON = artifact.JSON(frozen)
	if err := f.db.Save(f.job).Error; err != nil {
		t.Fatal(err)
	}
	v.transformDocument = func(doc *summarydoc.Document) {
		cue := f.source.Cues[1]
		doc.Blocks[0].SourceRefs = append(doc.Blocks[0].SourceRefs, summarydoc.SourceRef{SourceID: f.source.ID, CueIDs: []string{cue.ID}, StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod})
	}
	targets := []summaryVisualTarget{{BlockID: "block-cue-a", CueID: "cue-a", Goal: "优先核对配置"}, {BlockID: "block-cue-a", CueID: "cue-b", Goal: "其次核对限制"}, {BlockID: "block-cue-b", CueID: "cue-b", Goal: "最后核对例子"}}
	return f, v, targets
}

func TestSummaryVisualThreeValidTargetsChooseTwoAuthorizedFrames(t *testing.T) {
	f, v, targets := threeTargetVisualFixture(t)
	v.planResponses = []string{artifact.JSON(summaryVisualPlan{Targets: targets})}
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if v.visualError != nil || v.inspectCalls != 2 || len(v.requests) != 2 {
		t.Fatalf("legal overflow rejected or exceeded frames: %v / %d", v.visualError, v.inspectCalls)
	}
	for i, req := range v.requests {
		if req.Goal != targets[i].Goal || req.Budget.MaxFrames != 1 {
			t.Fatal("priority order or per-target limit changed")
		}
	}
	var input struct {
		MaxTargets int `json:"max_targets"`
	}
	_ = json.Unmarshal([]byte(v.planInput), &input)
	if input.MaxTargets != 2 || !strings.Contains(v.planSystem, "max_targets=2") || strings.Contains(v.planSystem, "最多三个") || !strings.Contains(v.planSystem, "由高到低") {
		t.Fatal("planner did not receive frozen max_targets")
	}
	row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if row.GeneratedVersion != 2 {
		t.Fatal("bounded valid plan did not publish inspected image")
	}
}

func TestSummaryVisualEightFramesStillLimitsTargetsToThreeVisualCalls(t *testing.T) {
	f, v, targets := threeTargetVisualFixture(t)
	var frozen processing.GenerationSnapshot
	_ = json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen)
	budget, _ := config.DefaultAgentBudgetConfig().Resolve(nil)
	frames := 8
	budget.Values.MaxVisualFrames = &frames
	frozen.Intent.BudgetJSON = artifact.JSON(budget)
	f.job.InputSnapshotJSON = artifact.JSON(frozen)
	if err := f.db.Save(f.job).Error; err != nil {
		t.Fatal(err)
	}
	previous := v.transformDocument
	v.transformDocument = func(doc *summarydoc.Document) {
		previous(doc)
		cue := f.source.Cues[0]
		doc.Blocks[1].SourceRefs = append(doc.Blocks[1].SourceRefs, summarydoc.SourceRef{SourceID: f.source.ID, CueIDs: []string{cue.ID}, StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod})
	}
	targets = append(targets, summaryVisualTarget{BlockID: "block-cue-b", CueID: "cue-a", Goal: "第四优先级"})
	v.planResponses = []string{artifact.JSON(summaryVisualPlan{Targets: targets})}
	v.selectedBlockID = "block-cue-b"
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if v.visualError != nil || v.inspectCalls != 3 {
		t.Fatalf("frame allowance exceeded frozen visual call limit: %v / %d", v.visualError, v.inspectCalls)
	}
	var input struct {
		MaxTargets int `json:"max_targets"`
	}
	_ = json.Unmarshal([]byte(v.planInput), &input)
	if input.MaxTargets != 3 || !strings.Contains(v.planSystem, "max_targets=3") {
		t.Fatal("system/input disagree with visual call allowance")
	}
	row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if row.GeneratedVersion != 2 {
		t.Fatal("legal first three candidates failed to publish")
	}
}

func TestSummaryVisualInvalidOverflowTailCannotBeTruncatedAway(t *testing.T) {
	for _, mode := range []string{"identity", "duplicate", "goal", "facts", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			f, v, targets := threeTargetVisualFixture(t)
			switch mode {
			case "identity":
				targets[2].CueID = "foreign-cue"
			case "duplicate":
				targets[2] = targets[0]
			case "goal":
				targets[2].Goal = ""
			case "facts":
				targets[2].RequiredFacts = summaryVisualFacts{{Name: " "}}
			case "oversize":
				for len(targets) < 33 {
					targets = append(targets, targets[0])
				}
			}
			raw := artifact.JSON(summaryVisualPlan{Targets: targets})
			v.planResponses = []string{raw, raw}
			if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
				t.Fatal(err)
			}
			var domain *artifact.Error
			if !errors.As(v.visualError, &domain) || (domain.Code != "invalid_visual_plan" && domain.Code != "invalid_visual_response") || v.inspectCalls != 0 {
				t.Fatalf("invalid tail bypassed validation: %v / %d", v.visualError, v.inspectCalls)
			}
			row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
			if row.GeneratedVersion != 1 {
				t.Fatal("invalid tail published image")
			}
		})
	}
}

func TestSummaryVisualChatActualOverrunBlocksSelection(t *testing.T) {
	for _, mode := range []string{"input", "output"} {
		t.Run(mode, func(t *testing.T) {
			usage := &ai.ChatUsage{PromptTokens: 24001, CompletionTokens: 10}
			if mode == "output" {
				usage.PromptTokens = 20
				usage.CompletionTokens = 1097
			}
			client, _, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: `{"public_title":"合法规划","reason":"无需图","targets":[]}`, usage: usage})
			e, f := visualBudgetExecutionFixture(t, client)
			var plan summaryVisualPlan
			err := e.chatStep(context.Background(), "visual-plan", 1000, "检查画面", "返回合法JSON", "授权候选", &plan)
			var domain *artifact.Error
			if !errors.As(err, &domain) || domain.Code != "visual_budget_exhausted" || calls.Load() != 1 {
				t.Fatalf("actual overrun accepted: %v / %d", err, calls.Load())
			}
			run, _ := e.journal.GetRun(context.Background(), f.task.UserID, f.job.GenerationID)
			if run.PromptTokensUsed != usage.PromptTokens || run.CompletionTokensUsed != 3000+usage.CompletionTokens {
				t.Fatal("actual usage lost or capped during persistence")
			}
		})
	}
}

func TestSummaryVisualInspectionActualOverrunCannotPublish(t *testing.T) {
	for _, mode := range []string{"input", "output"} {
		t.Run(mode, func(t *testing.T) {
			f, base, _ := threeTargetVisualFixture(t)
			usage := &ai.ChatUsage{PromptTokens: 24001, CompletionTokens: 50}
			if mode == "output" {
				usage.PromptTokens = 20
				usage.CompletionTokens = 8193
			}
			client, _, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: `{"facts":["实际界面"],"gaps":[]}`, usage: usage})
			// Invoke the real provider usage callback through the decorated Vision port.
			vision := summaryChatAsVision{client: client}
			inspector := summaryBudgetInspector{base: base, path: "unused-local-fixture"}
			f.svc.WithVisualEnricher(NewSummaryVisualService(f.repos, summaryBudgetVisualFactory{base: base, vision: vision}, inspector))
			if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || base.chatCalls != 1 {
				t.Fatal("actual inspection overrun proceeded to selector")
			}
			row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
			if row.GeneratedVersion != 1 {
				t.Fatal("actual inspection overrun published image")
			}
			var records []model.AgentToolCall
			if err := f.db.Where("run_id=? AND tool_name=?", f.job.GenerationID, "inspect_summary_frame").Find(&records).Error; err != nil {
				t.Fatal(err)
			}
			if len(records) != 1 || records[0].PromptTokens != usage.PromptTokens || records[0].CompletionTokens != usage.CompletionTokens || records[0].UsageSource != model.AgentCallUsageActual {
				t.Fatal("actual inspection usage not preserved")
			}
		})
	}
}

type summaryChatAsVision struct{ client ai.ChatClient }

func (v summaryChatAsVision) CaptionImage(ctx context.Context, _ string, prompt string) (string, error) {
	return v.client.Chat(ctx, []ai.ChatMessage{{Role: "user", Content: prompt}})
}
