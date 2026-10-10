package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/textsource"
)

type ungroundedGenerationChat struct {
	fixture *generationFixtureChat
	mode    string
	once    bool
}

type nonStreamingGenerationClient struct{ client ai.ChatClient }

func (c nonStreamingGenerationClient) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	return c.client.Chat(ctx, messages)
}

func (c *ungroundedGenerationChat) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	raw, err := c.fixture.Chat(ctx, messages)
	if err != nil || c.once && len(c.fixture.calls) > 1 {
		return raw, err
	}
	var envelope map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &envelope)
	var doc summarydoc.Document
	_ = json.Unmarshal(envelope["document"], &doc)
	for i := range doc.Blocks {
		if c.mode == "empty_refs" {
			doc.Blocks[i].SourceRefs = nil
		}
		if c.mode == "empty_shell" {
			doc.Blocks[i].BodyMarkdown = ""
			doc.Blocks[i].SourceRefs = nil
		}
		if c.mode == "marker" {
			doc.Blocks[i].BodyMarkdown += "[" + c.fixture.source.Cues[0].ID + "]"
		}
	}
	envelope["document"] = json.RawMessage(artifact.JSON(doc))
	return artifact.JSON(envelope), nil
}

func TestSummaryGenerationRejectsUngroundedBodyAndInternalCueMarkers(t *testing.T) {
	for _, mode := range []string{"empty_refs", "marker", "empty_shell"} {
		t.Run(mode, func(t *testing.T) {
			f := newGenerationFixture(t, false)
			f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: &ungroundedGenerationChat{fixture: f.chat, mode: mode}})
			err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken)
			var domain *artifact.Error
			if !errors.As(err, &domain) || domain.Code != "invalid_summary_document" {
				t.Fatalf("unreferenced/marker body was not rejected: %v", err)
			}
			if len(f.chat.calls) != 2 {
				t.Fatalf("repair not bounded: %d", len(f.chat.calls))
			}
			if row, _ := f.repos.Summary.FindByTaskID(f.task.ID); row != nil {
				t.Fatal("invalid body published")
			}
			saved, _ := f.repos.Task.FindByID(f.task.ID)
			if saved.Title != "" {
				t.Fatal("invalid body published a title")
			}
		})
	}
}

func TestSummaryGenerationRepairsMissingReferencesBeforePublication(t *testing.T) {
	f := newGenerationFixture(t, false)
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: &ungroundedGenerationChat{fixture: f.chat, mode: "empty_refs", once: true}})
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if len(f.chat.calls) != 2 || !strings.Contains(f.chat.calls[1], "source_refs") {
		t.Fatal("missing-reference feedback did not reach the bounded repair")
	}
	row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	doc, err := summarydoc.Parse([]byte(row.DocumentJSON))
	if err != nil || len(doc.Blocks[0].SourceRefs) == 0 {
		t.Fatal("repair did not publish actual frozen references", err)
	}
}

// Synthetic native adapter provenance has the same 46 timed-window shape and
// text density as the real failure. It contains no saved provider response.
func generationTimedWindowFixture(t *testing.T) *generationFixture {
	t.Helper()
	f := newGenerationFixture(t, false)
	ctx := context.Background()
	var cues []textsource.Cue
	for i := 0; i < 46; i++ {
		words := strings.Repeat("配置与执行分别维护，案例说明适用条件。", 5)
		start, end := int64(i*15000), int64((i+1)*15000)
		begin, finish, join := 0, len([]rune(words)), "\n"
		id := fmt.Sprintf("asr-window-%d:0:%d", i, finish)
		cues = append(cues, textsource.Cue{ID: id, Order: i + 1, Text: words, RawText: words, StartMS: &start, EndMS: &end, TimingMethod: "asr_window", JoinBefore: &join, RawRefs: []textsource.RawCueRef{{ID: id, Order: i + 1, RawText: words, ObservationID: fmt.Sprintf("asr-window-%d", i), ObservationOrder: i + 1, TextStart: &begin, TextEnd: &finish, StartMS: &start, EndMS: &end, TimingMethod: "asr_window"}}})
	}
	canonical, err := textsource.Canonicalize(textsource.Snapshot{Kind: textsource.KindASR, Identity: f.source.Identity, ParserVersion: "asr-provenance-v1", Cues: cues}, textsource.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	pub, err := f.repos.PublishTextSource(ctx, repository.PublishTextSourceRequest{UserID: f.task.UserID, TaskID: f.task.ID, ExpectedActiveSourceID: f.source.ID, Snapshot: canonical}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.source, err = f.repos.TextSource.Read(ctx, f.task.UserID, f.task.ID, pub.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.task, _ = f.repos.Task.FindByID(f.task.ID)
	f.profiles.profile.LLMContextTokens = 32768
	var frozen processing.GenerationSnapshot
	_ = json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen)
	frozen.SourceID, frozen.SourceDigest = f.source.ID, f.source.SourceDigest
	frozen.Intent.ProfileFingerprint = processing.FingerprintProfile(f.profiles.profile)
	budget, _ := config.DefaultAgentBudgetConfig().Resolve(nil)
	budget.Values.MaxOutputTokens = 24576
	frozen.Intent.BudgetJSON = artifact.JSON(budget)
	f.job.InputSourceID, f.job.InputText, f.job.InputSnapshotJSON = f.source.ID, f.source.CanonicalText, artifact.JSON(frozen)
	if err = f.db.Model(f.job).Updates(map[string]any{"input_source_id": f.source.ID, "input_text": f.source.CanonicalText, "input_snapshot_json": f.job.InputSnapshotJSON}).Error; err != nil {
		t.Fatal(err)
	}
	f.chat.source = f.source
	return f
}

func TestSummaryGenerationTimedWindowsGetsBoundedJSONOutputHeadroom(t *testing.T) {
	f := generationTimedWindowFixture(t)
	raw, err := (nativeASRGenerationClient{fixture: f.chat}).Chat(context.Background(), []ai.ChatMessage{{Content: artifact.JSON(f.source.Cues)}})
	if err != nil {
		t.Fatal(err)
	}
	f.chat.calls = nil
	client, requests, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: raw, usage: &ai.ChatUsage{PromptTokens: 4807, CompletionTokens: 3000}})
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: nonStreamingGenerationClient{client}})
	if err = f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	request := <-requests
	var cap int64
	_ = json.Unmarshal(request["max_tokens"], &cap)
	if cap <= 2048 || cap > 8192 {
		t.Fatalf("JSON references still squeezed into fixed output cap: %d", cap)
	}
	var messages []ai.ChatMessage
	_ = json.Unmarshal(request["messages"], &messages)
	if cap+studyPromptTokens(messages)+256 > int64(f.profiles.profile.LLMContextTokens) || calls.Load() != 1 {
		t.Fatal("output cap violated context or unbounded repair")
	}
	store := repository.NewSummaryGenerationExecutionStore(f.repos, f.task.UserID, f.task.ID, f.job.GenerationID)
	saved, _ := store.GetRun(context.Background(), f.task.UserID, f.job.GenerationID)
	if saved.CompletionTokensUsed != 3000 || saved.MaxCompletionTokens != 24576 {
		t.Fatal("actual usage or frozen total changed")
	}
	row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	doc, _ := summarydoc.Parse([]byte(row.DocumentJSON))
	if len(doc.Blocks) != 46 {
		t.Fatal("timed windows were omitted")
	}
	savedTask, _ := f.repos.Task.FindByID(f.task.ID)
	if savedTask.Title != doc.Title || savedTask.TitleOrigin != "auto" {
		t.Fatal("body and blank task title were not published together")
	}
}

type remainingVisualBudgetProbe struct {
	f       *generationFixture
	called  bool
	failure error
}

func (p *remainingVisualBudgetProbe) Enrich(ctx context.Context, _ *model.VideoTask, _ *model.TaskJob, _ processing.GenerationSnapshot, _ ai.Profile, _ *textsource.Snapshot, _ *model.AISummary, _ string) error {
	p.called = true
	store := repository.NewSummaryGenerationExecutionStore(p.f.repos, p.f.task.UserID, p.f.task.ID, p.f.job.GenerationID)
	run, err := store.GetRun(ctx, p.f.task.UserID, p.f.job.GenerationID)
	if err != nil {
		p.failure = err
		return err
	}
	if run.MaxCompletionTokens != 8192 || run.MaxPromptTokens != 24000 || run.MaxDurationMs != 90000 || run.MaxFrames != 2 || run.MaxCompletionTokens-run.CompletionTokensUsed < 2048 {
		p.failure = errors.New("frozen budget expanded or visual reserve consumed")
		return p.failure
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 90*time.Second {
		p.failure = errors.New("absolute frozen duration expanded")
		return p.failure
	}
	return artifact.Err("visual_not_beneficial", 422)
}

func subtitleGenerationBudgetFixture(t *testing.T) (*generationFixture, string) {
	t.Helper()
	f := newGenerationFixture(t, false)
	var cues []textsource.Cue
	for i := 0; i < 328; i++ {
		words := "配置与执行分离后核对条件"
		if i < 43 {
			words += "。"
		}
		start, end := int64(i*1000), int64((i+1)*1000)
		cues = append(cues, textsource.Cue{ID: fmt.Sprintf("subtitle-cue-%d", i+1), Order: i + 1, Text: words, RawText: words, StartMS: &start, EndMS: &end, TimingMethod: "subtitle_cue"})
	}
	canonical, err := textsource.Canonicalize(textsource.Snapshot{Kind: textsource.KindSubtitle, Identity: f.source.Identity, ParserVersion: "fixture-srt", Language: "zh", TrackKey: "zh", SubtitleKind: "manual", KindBasis: "fixture", Cues: cues}, textsource.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	pub, err := f.repos.PublishTextSource(context.Background(), repository.PublishTextSourceRequest{UserID: f.task.UserID, TaskID: f.task.ID, ExpectedActiveSourceID: f.source.ID, Snapshot: canonical}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.source, err = f.repos.TextSource.Read(context.Background(), f.task.UserID, f.task.ID, pub.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.task, _ = f.repos.Task.FindByID(f.task.ID)
	f.profiles.profile.LLMContextTokens = 1000000
	var frozen processing.GenerationSnapshot
	_ = json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen)
	frozen.SourceID, frozen.SourceDigest = f.source.ID, f.source.SourceDigest
	frozen.Intent.ProfileFingerprint = processing.FingerprintProfile(f.profiles.profile)
	frozen.Intent.Options.SummaryVisualEnabled = true
	frozen.Intent.Options.OutputMode = "auto"
	budget, _ := config.DefaultAgentBudgetConfig().Resolve(nil)
	budget.Values.MaxToolCalls = 8
	budget.Values.MaxInputTokens = 24000
	budget.Values.MaxOutputTokens = 8192
	budget.Values.MaxDurationSeconds = 90
	frames := 2
	budget.Values.MaxVisualFrames = &frames
	budget.FinalAnswerReserve.OutputTokens = 2048
	budget.FinalAnswerReserve.InputTokens = 4096
	budget.FinalAnswerReserve.DurationSeconds = 45
	frozen.Intent.BudgetJSON = artifact.JSON(budget)
	frozen.Intent.PolicyJSON = artifact.JSON(map[string]any{"recipe": processing.Recipe, "options": frozen.Intent.Options})
	f.job.InputSourceID, f.job.InputText, f.job.InputSnapshotJSON = f.source.ID, f.source.CanonicalText, artifact.JSON(frozen)
	if err = f.db.Model(f.job).Updates(map[string]any{"input_source_id": f.source.ID, "input_text": f.source.CanonicalText, "input_snapshot_json": f.job.InputSnapshotJSON}).Error; err != nil {
		t.Fatal(err)
	}
	f.chat.source = f.source
	doc := summarydoc.Document{SchemaVersion: summarydoc.SchemaVersion, DocumentID: f.job.GenerationID, SourceID: f.source.ID, SourceDigest: f.source.SourceDigest, MediaRevision: f.source.Identity.MediaFingerprint, PresentationMode: "text", Title: "配置与执行的关系", Overview: "机制、案例与条件。"}
	for i := 0; i < 6; i++ {
		cue := f.source.Cues[i*50]
		id := fmt.Sprintf("chapter-%d", i)
		doc.Blocks = append(doc.Blocks, summarydoc.Block{ID: id, Order: i, Title: "机制与条件"}, summarydoc.Block{ID: id + "-detail", ParentID: &id, Order: 0, Title: "实际配置", BodyMarkdown: "配置与执行分离，并核对适用条件。", SourceRefs: []summarydoc.SourceRef{{SourceID: f.source.ID, CueIDs: []string{cue.ID}, StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod}}})
	}
	return f, artifact.JSON(map[string]any{"public_title": "整理具体机制及条件", "public_summary": "章节已保留合法来源。", "document": doc, "tag_candidates": []any{}})
}

func TestSummaryGeneration328SubtitleCuesFitsFrozenBudgetAndPreservesVisualReserve(t *testing.T) {
	for _, mode := range []string{"normal", "repair", "truncated", "exhausted"} {
		t.Run(mode, func(t *testing.T) {
			f, raw := subtitleGenerationBudgetFixture(t)
			responses := []summaryBudgetResponse{{content: raw, usage: &ai.ChatUsage{PromptTokens: 6000, CompletionTokens: 2500}}}
			if mode == "repair" || mode == "truncated" || mode == "exhausted" {
				first := summaryBudgetResponse{content: `{}`, usage: &ai.ChatUsage{PromptTokens: 6000, CompletionTokens: 3000}}
				if mode == "truncated" {
					first.finish = "length"
				}
				if mode == "exhausted" {
					first.usage.CompletionTokens = 6144
				}
				responses = append([]summaryBudgetResponse{first}, responses...)
			}
			client, requests, calls := summaryBudgetProvider(t, responses...)
			probe := &remainingVisualBudgetProbe{f: f}
			f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: nonStreamingGenerationClient{client}}).WithVisualEnricher(probe)
			err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken)
			if mode == "exhausted" {
				var domain *artifact.Error
				if !errors.As(err, &domain) || domain.Code != "budget_exhausted" || calls.Load() != 1 || probe.called {
					t.Fatalf("repair consumed the reserve or called again: %v, %d", err, calls.Load())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if probe.failure != nil {
				t.Fatal(probe.failure)
			}
			wantCalls := int32(1)
			if mode != "normal" {
				wantCalls = 2
			}
			if calls.Load() != wantCalls || !probe.called {
				t.Fatalf("unneeded segmentation or visual omitted: %d", calls.Load())
			}
			first := <-requests
			if string(first["max_tokens"]) != "6144" {
				t.Fatalf("insufficient JSON headroom or consumed reserve: %s", first["max_tokens"])
			}
			var messages []ai.ChatMessage
			_ = json.Unmarshal(first["messages"], &messages)
			if !strings.Contains(messages[1].Content, "subtitle-cue-328") || studyPromptTokens(messages) >= 24000 {
				t.Fatal("full short-cue track omitted or frozen input budget exceeded")
			}
			for _, constraint := range []string{"具体机制", "案例及其适用条件", "parent_id必须指向本次返回的父块", "过于宽泛的上位领域标签", "不能擅自写成自动更新或效果保证"} {
				if !strings.Contains(messages[0].Content, constraint) {
					t.Fatalf("provider lost task6 quality boundary %q", constraint)
				}
			}
			if mode != "normal" {
				second := <-requests
				if string(second["max_tokens"]) != "3144" {
					t.Fatalf("repair did not shrink against actual usage plus reserve: %s", second["max_tokens"])
				}
			}
		})
	}
}

func TestSummaryGenerationVisualMissingRefsHasTruthfulReason(t *testing.T) {
	f := newGenerationFixture(t, false)
	raw, _ := f.chat.Chat(context.Background(), []ai.ChatMessage{{Content: artifact.JSON(f.source.Cues)}})
	var envelope struct {
		Document summarydoc.Document `json:"document"`
	}
	_ = json.Unmarshal([]byte(raw), &envelope)
	for i := range envelope.Document.Blocks {
		envelope.Document.Blocks[i].SourceRefs = nil
	}
	base := &model.AISummary{DocumentJSON: artifact.JSON(envelope.Document)}
	err := summaryGenerationVisualReferenceFailure(artifact.Err("visual_location_missing", 422), f.source, base)
	state, reason := summaryVisualFailure(err)
	if state != "failed" || reason != "visual_source_refs_missing" {
		t.Fatalf("timed source mislabeled: %s %s", state, reason)
	}
	copySource := *f.source
	copySource.Cues = append([]textsource.Cue(nil), f.source.Cues...)
	for i := range copySource.Cues {
		copySource.Cues[i].StartMS = nil
		copySource.Cues[i].EndMS = nil
		copySource.Cues[i].TimingMethod = "unknown"
	}
	_, reason = summaryVisualFailure(summaryGenerationVisualReferenceFailure(artifact.Err("visual_location_missing", 422), &copySource, base))
	if reason != "visual_location_missing" {
		t.Fatal("truly untimed source mislabeled")
	}
}
