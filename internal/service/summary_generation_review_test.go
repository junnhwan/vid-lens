package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/textsource"
)

// Observe the real service boundary immediately before an independent review
// request. The delegate is a local HTTP provider, never a real model.
type groundingReviewProbe struct {
	client    ai.ChatClient
	f         *generationFixture
	messages  [][]ai.ChatMessage
	premature bool
}

func (p *groundingReviewProbe) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	p.messages = append(p.messages, append([]ai.ChatMessage(nil), messages...))
	if len(p.messages) > 1 {
		row, err := p.f.repos.Summary.FindByTaskID(p.f.task.ID)
		task, taskErr := p.f.repos.Task.FindByID(p.f.task.ID)
		if err != nil || taskErr != nil || row != nil || task == nil || task.Title != "" {
			p.premature = true
		}
	}
	return p.client.Chat(ctx, messages)
}

func TestSummaryGenerationRichSourceReviewPrecedesPublicationAndUsesFullSource(t *testing.T) {
	f, _, draft := minimalWireFixture(t)
	if utf8.RuneCountInString(f.source.CanonicalText) < 1500 {
		t.Fatal("not a rich source")
	}
	var reviewed map[string]any
	if err := json.Unmarshal([]byte(artifact.JSON(draft)), &reviewed); err != nil {
		t.Fatal(err)
	}
	reviewedDoc := reviewed["document"].(map[string]any)
	reviewedDoc["title"] = "全文审核后的机制与条件"
	reviewedDoc["blocks"].([]any)[1].(map[string]any)["body_markdown"] = "配置与执行需要分别维护；例子回流只能辅助归因，不能保证自动提高效果。"
	client, _, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: artifact.JSON(draft), usage: &ai.ChatUsage{PromptTokens: 2000, CompletionTokens: 1000}}, summaryBudgetResponse{content: artifact.JSON(reviewed), usage: &ai.ChatUsage{PromptTokens: 3000, CompletionTokens: 1000}})
	probe := &groundingReviewProbe{client: client, f: f}
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: probe})
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(probe.messages) != 2 || probe.premature {
		t.Fatalf("review did not run before publication: calls=%d premature=%v", calls.Load(), probe.premature)
	}
	reviewSystem := probe.messages[1][0].Content
	if !strings.Contains(reviewSystem, "全文") || (!strings.Contains(reviewSystem, "审核") && !strings.Contains(reviewSystem, "复核") && !strings.Contains(reviewSystem, "校验") && !strings.Contains(reviewSystem, "检查")) {
		t.Fatal("independent full-source review instructions absent from system prompt")
	}
	reviewInput := probe.messages[1][1].Content
	sourceStart := strings.Index(reviewInput, summaryCueInputPrefix)
	if sourceStart < 0 {
		t.Fatal("review did not receive complete source table")
	}
	beforeSource := reviewInput[:sourceStart]
	draftDoc := draft["document"].(map[string]any)
	if !strings.Contains(beforeSource, draftDoc["title"].(string)) || !strings.Contains(beforeSource, "先区分配置与执行，再核对案例的适用条件；反馈不能直接证明效果。") || !strings.Contains(beforeSource, `"blocks"`) || !strings.Contains(beforeSource, `"cue_ids"`) {
		t.Fatal("review omitted semantic draft before the source")
	}
	for _, field := range []string{`"source_id"`, `"source_digest"`, `"media_revision"`, `"document_id"`, `"schema_version"`, `"presentation_mode"`, `"source_refs"`, `"start_ms"`, `"end_ms"`, `"timing_method"`, `"figures"`} {
		if strings.Contains(beforeSource, field) {
			t.Fatalf("review duplicated immutable canonical metadata: %s", field)
		}
	}
	cues, ok := decodeSummaryGenerationCues(reviewInput)
	if !ok || len(cues) != 328 {
		t.Fatal("review omitted source cues")
	}
	for i, cue := range cues {
		if cue.ID != f.source.Cues[i].ID || cue.Text != f.source.Cues[i].Text {
			t.Fatal("review source changed or truncated")
		}
	}
	row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if row == nil {
		t.Fatal("reviewed document missing")
	}
	doc, err := summarydoc.Parse([]byte(row.DocumentJSON))
	if err != nil || doc.Title != reviewedDoc["title"] || doc.Blocks[1].BodyMarkdown != reviewedDoc["blocks"].([]any)[1].(map[string]any)["body_markdown"] {
		t.Fatalf("draft published instead of reviewed content: %v", err)
	}
	task, _ := f.repos.Task.FindByID(f.task.ID)
	if task.Title != doc.Title {
		t.Fatal("generated task title did not follow reviewed title")
	}
	var steps []model.AgentStep
	if err = f.db.Where("run_id=?", f.job.GenerationID).Order("sequence").Find(&steps).Error; err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[1].StepID != "summary-grounding-review" {
		t.Fatal("review bypassed durable generation journal")
	}
}

func TestSummaryGenerationRichSourceFailedReviewNeverPublishesOrSetsTitle(t *testing.T) {
	for _, mode := range []string{"invalid", "actual_budget"} {
		t.Run(mode, func(t *testing.T) {
			f, _, draft := minimalWireFixture(t)
			responses := []summaryBudgetResponse{{content: artifact.JSON(draft), usage: &ai.ChatUsage{PromptTokens: 2000, CompletionTokens: 1000}}, {content: `{}`, usage: &ai.ChatUsage{PromptTokens: 2000, CompletionTokens: 1000}}, {content: `{}`, usage: &ai.ChatUsage{PromptTokens: 2000, CompletionTokens: 1000}}}
			if mode == "actual_budget" {
				responses[1] = summaryBudgetResponse{content: artifact.JSON(draft), usage: &ai.ChatUsage{PromptTokens: 24001, CompletionTokens: 1000}}
				responses = responses[:2]
			}
			client, _, calls := summaryBudgetProvider(t, responses...)
			probe := &groundingReviewProbe{client: client, f: f}
			f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: probe})
			err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken)
			var domain *artifact.Error
			if !errors.As(err, &domain) || (domain.Code != "invalid_summary_document" && domain.Code != "budget_exhausted") {
				t.Fatalf("invalid review accepted: %v", err)
			}
			if calls.Load() < 2 || calls.Load() > 3 || probe.premature {
				t.Fatalf("review failure was unbounded or draft escaped: %d/%v", calls.Load(), probe.premature)
			}
			row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
			task, _ := f.repos.Task.FindByID(f.task.ID)
			if row != nil || task.Title != "" || task.TitleOrigin != "" {
				t.Fatal("failed review published draft or generated title")
			}
		})
	}
}

func TestSummaryGenerationShortSourceSkipsIndependentFullReview(t *testing.T) {
	f := newGenerationFixture(t, false)
	if utf8.RuneCountInString(f.source.CanonicalText) >= 1500 {
		t.Fatal("not a short source")
	}
	raw, err := f.chat.Chat(context.Background(), []ai.ChatMessage{{Content: artifact.JSON(f.source.Cues)}})
	if err != nil {
		t.Fatal(err)
	}
	client, _, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: raw, usage: &ai.ChatUsage{PromptTokens: 200, CompletionTokens: 300}})
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: nonStreamingGenerationClient{client}})
	if err = f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("short source introduced additional review call")
	}
	var count int64
	if err = f.db.Model(&model.AgentStep{}).Where("run_id=? AND step_id=?", f.job.GenerationID, "summary-grounding-review").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("short source introduced durable review step")
	}
}

func TestSummaryGenerationRichSourceRepairedDraftStillRequiresReview(t *testing.T) {
	f, _, draft := minimalWireFixture(t)
	var reviewed map[string]any
	_ = json.Unmarshal([]byte(artifact.JSON(draft)), &reviewed)
	reviewed["document"].(map[string]any)["title"] = "修复草稿后仍经过全文审核"
	client, _, calls := summaryBudgetProvider(t,
		summaryBudgetResponse{content: `{`, finish: "length", usage: &ai.ChatUsage{PromptTokens: 1000, CompletionTokens: 1000}},
		summaryBudgetResponse{content: artifact.JSON(draft), usage: &ai.ChatUsage{PromptTokens: 2000, CompletionTokens: 1000}},
		summaryBudgetResponse{content: artifact.JSON(reviewed), usage: &ai.ChatUsage{PromptTokens: 3000, CompletionTokens: 1000}},
	)
	probe := &groundingReviewProbe{client: client, f: f}
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: probe})
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || probe.premature {
		t.Fatal("repaired draft skipped review or was published before review")
	}
	row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if row == nil {
		t.Fatal("reviewed repaired draft not published")
	}
	doc, err := summarydoc.Parse([]byte(row.DocumentJSON))
	if err != nil || doc.Title != "修复草稿后仍经过全文审核" {
		t.Fatal("repaired draft was published instead of reviewed result")
	}
	var steps []model.AgentStep
	_ = f.db.Where("run_id=?", f.job.GenerationID).Order("sequence").Find(&steps).Error
	if len(steps) != 3 || steps[0].StepID != "summary-complete" || steps[1].StepID != "summary-complete-repair" || steps[2].StepID != "summary-grounding-review" {
		t.Fatal("review did not follow the successfully repaired draft")
	}
}

func TestSummaryGenerationRichSourceLowContextReviewsEveryLeafAndMerge(t *testing.T) {
	f := newGenerationFixture(t, true)
	probe := &groundingReviewProbe{client: f.chat, f: f}
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: probe})
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if probe.premature || f.profiles.profile.LLMContextTokens != 4096 {
		t.Fatal("low-context review published early or expanded its window")
	}
	reviewed := map[string]string{}
	merges := 0
	for i, input := range f.chat.calls {
		if strings.Contains(input, summaryGroundingReviewPrefix) && !strings.Contains(input, "\n校验反馈") {
			cues, ok := decodeSummaryGenerationCues(input)
			if !ok {
				t.Fatal("leaf review has no original source")
			}
			for _, cue := range cues {
				// Oversized cues retain their opaque identity across contiguous
				// spans. Reassemble every reviewed span in original order.
				reviewed[cue.ID] += cue.Text
			}
		} else if strings.Contains(input, summaryVerifiedPartsPrefix) && !strings.Contains(input, summaryReductionReviewPrefix) && !strings.Contains(input, "\n校验反馈") {
			merges++
			next := i + 1
			if next < len(f.chat.calls) && strings.Contains(f.chat.calls[next], "\n校验反馈") {
				next++ // A bounded repair must also be reviewed before publication.
			}
			if next >= len(f.chat.calls) || !strings.Contains(f.chat.calls[next], summaryReductionReviewPrefix) {
				t.Fatal("newly generated merge skipped independent fidelity review")
			}
			if strings.Contains(f.chat.calls[next], summaryCueInputPrefix) {
				t.Fatal("merge review incorrectly claims original full-source context")
			}
		}
	}
	if len(reviewed) != len(f.source.Cues) || merges == 0 {
		t.Fatal("low-context path skipped original source review or merge")
	}
	for _, cue := range f.source.Cues {
		if reviewed[cue.ID] != cue.Text {
			t.Fatal("leaf review omitted or changed original source text")
		}
	}
}

func TestSummaryGenerationReviewFencesReferencesToCurrentCall(t *testing.T) {
	f, execution, envelope := minimalWireFixture(t)
	source := *f.source
	source.Cues = append([]textsource.Cue(nil), source.Cues[:2]...)
	execution.source = &source
	makeDocument := func(ids ...string) summarydoc.Document {
		t.Helper()
		envelope["document"].(map[string]any)["blocks"] = []any{map[string]any{"id": "mechanism", "parent_id": nil, "order": 0, "title": "机制", "body_markdown": "机制成立需要满足条件。", "cue_ids": ids}}
		checkpoint := execution.validateResponse(artifact.JSON(envelope))
		if checkpoint.Invalid || checkpoint.Document == nil {
			t.Fatal("valid frozen source fixture rejected")
		}
		return *checkpoint.Document
	}
	first := makeDocument(source.Cues[0].ID)
	outside := makeDocument(source.Cues[1].ID)
	union := makeDocument(source.Cues[0].ID, source.Cues[1].ID)
	input := summaryGenerationSemanticCueInput([]summaryGenerationCue{{ID: source.Cues[0].ID, Text: source.Cues[0].Text}})
	for _, format := range []string{"wire", "canonical"} {
		t.Run(format, func(t *testing.T) {
			doc := outside
			if format == "canonical" {
				canonical := execution.validateResponse(artifact.JSON(map[string]any{"public_title": "核对机制", "public_summary": "核对来源条件", "document": outside}))
				if canonical.Invalid || canonical.Document == nil {
					t.Fatal("historical canonical fixture rejected")
				}
				doc = *canonical.Document
			}
			for _, callInput := range []string{input, summaryGenerationVerifiedPartsInput([]summarydoc.Document{first})} {
				result := summaryGenerationInputScope(doc, callInput)
				if !result.Invalid || result.ValidationCode != "source_cue_not_in_call" || result.ValidationPath != "document.blocks[0].cue_ids" {
					t.Fatal("unseen cue escaped source-call or reviewed-part fence")
				}
			}
		})
	}
	if summaryGenerationInputScope(first, input).Invalid || summaryGenerationInputScope(union, summaryGenerationVerifiedPartsInput([]summarydoc.Document{first, outside})).Invalid {
		t.Fatal("exact input cue or reviewed-parts union rejected")
	}
}
