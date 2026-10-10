package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/textsource"
)

func minimalWireFixture(t *testing.T) (*generationFixture, *summaryGenerationExecution, map[string]any) {
	t.Helper()
	f, _ := subtitleGenerationBudgetFixture(t)
	var frozen processing.GenerationSnapshot
	if err := json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	execution := &summaryGenerationExecution{task: f.task, source: f.source, run: &model.AgentRun{ID: f.job.GenerationID}, snapshot: frozen, profile: f.profiles.profile}
	blocks := []any{map[string]any{"id": "mechanism", "parent_id": nil, "order": 0, "title": "配置与执行机制", "body_markdown": "", "cue_ids": []string{}}}
	for start := 0; start < len(f.source.Cues); start += 64 {
		ids := []string{}
		for _, cue := range f.source.Cues[start:min(start+64, len(f.source.Cues))] {
			ids = append(ids, cue.ID)
		}
		blocks = append(blocks, map[string]any{"id": fmt.Sprintf("detail-%d", start), "parent_id": "mechanism", "order": start / 64, "title": "步骤与适用条件", "body_markdown": "先区分配置与执行，再核对案例的适用条件；反馈不能直接证明效果。", "cue_ids": ids})
	}
	envelope := map[string]any{"public_title": "整理机制及条件", "public_summary": "保留具体联系和案例边界。", "tag_candidates": []any{}, "document": map[string]any{"title": "配置与执行的联系", "overview": "机制及条件导航。", "blocks": blocks}}
	return f, execution, envelope
}

func TestSummaryGenerationMinimalWireNativeAndUnknownTimingStayServerOwned(t *testing.T) {
	f, execution, envelope := minimalWireFixture(t)
	copySource := *f.source
	start, end := int64(101), int64(901)
	copySource.Cues = []textsource.Cue{{ID: "asr-window-0:0:17", Text: "机制与适用条件", StartMS: &start, EndMS: &end, TimingMethod: "asr_window"}, {ID: "opaque-unknown", Text: "无时间文本", TimingMethod: "unknown"}}
	execution.source = &copySource
	envelope["document"].(map[string]any)["blocks"] = []any{map[string]any{"id": "mechanism", "parent_id": nil, "order": 0, "title": "机制", "body_markdown": "机制取决于适用条件。", "cue_ids": []string{"asr-window-0:0:17", "opaque-unknown"}}}
	result := execution.validateResponse(artifact.JSON(envelope))
	if result.Invalid || result.Document == nil {
		t.Fatalf("legal opaque cues rejected: %+v", result)
	}
	refs := result.Document.Blocks[0].SourceRefs
	if len(refs) != 2 || refs[0].TimingMethod != "asr_window" || refs[0].StartMS == nil || *refs[0].StartMS != 101 || refs[0].EndMS == nil || *refs[0].EndMS != 901 || refs[1].TimingMethod != "unknown" || refs[1].StartMS != nil || refs[1].EndMS != nil {
		t.Fatal("server altered precise native or unknown timing")
	}
}

func TestSummaryGenerationMinimalWire328CuesCanonicalAssembly(t *testing.T) {
	f, execution, envelope := minimalWireFixture(t)
	checkpoint := execution.validateResponse(artifact.JSON(envelope))
	if checkpoint.Invalid || checkpoint.Document == nil {
		t.Fatalf("minimal wire rejected: %+v", checkpoint)
	}
	doc := checkpoint.Document
	if doc.SchemaVersion != summarydoc.SchemaVersion || doc.DocumentID != f.job.GenerationID || doc.SourceID != f.source.ID || doc.SourceDigest != f.source.SourceDigest || doc.MediaRevision != f.source.Identity.MediaFingerprint || doc.PresentationMode != "text" {
		t.Fatal("canonical identity was not assembled from frozen source")
	}
	if len(doc.Blocks) != 7 || doc.Title != "配置与执行的联系" || doc.Blocks[0].Title != "配置与执行机制" {
		t.Fatal("semantic hierarchy changed")
	}
	seen := map[string]bool{}
	registry := map[string]summarydoc.Cue{}
	for _, cue := range f.source.Cues {
		registry[cue.ID] = summarydoc.Cue{StartMS: cue.StartMS, EndMS: cue.EndMS, TimingMethod: cue.TimingMethod}
	}
	for _, block := range doc.Blocks[1:] {
		if block.ParentID == nil || *block.ParentID != "mechanism" || block.BodyMarkdown != "先区分配置与执行，再核对案例的适用条件；反馈不能直接证明效果。" || len(block.Figures) != 0 {
			t.Fatal("hierarchy, concrete content or figure boundary changed")
		}
		for _, ref := range block.SourceRefs {
			if len(ref.CueIDs) != 1 || ref.SourceID != f.source.ID {
				t.Fatal("noncanonical source ref")
			}
			cue, ok := registry[ref.CueIDs[0]]
			if !ok || ref.StartMS == nil || ref.EndMS == nil || *ref.StartMS != *cue.StartMS || *ref.EndMS != *cue.EndMS || ref.TimingMethod != cue.TimingMethod {
				t.Fatal("frozen exact timing changed")
			}
			if seen[ref.CueIDs[0]] {
				t.Fatal("duplicate generated reference")
			}
			seen[ref.CueIDs[0]] = true
		}
	}
	if len(seen) != 328 {
		t.Fatalf("lost cue selections: %d", len(seen))
	}
	if err := summarydoc.ValidateGeneratedContent(*doc, summarydoc.ValidationContext{SourceID: f.source.ID, SourceDigest: f.source.SourceDigest, MediaRevision: f.source.Identity.MediaFingerprint, GenerationID: f.job.GenerationID, Cues: registry}); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryGenerationMinimalWireRejectsUntrustedFieldsAndCues(t *testing.T) {
	for _, field := range []string{"source_id", "source_digest", "media_revision", "document_id", "schema_version", "presentation_mode", "figures", "source_refs", "start_ms", "end_ms", "timing_method", "extra"} {
		t.Run(field, func(t *testing.T) {
			_, execution, envelope := minimalWireFixture(t)
			doc := envelope["document"].(map[string]any)
			if field == "figures" || field == "source_refs" || field == "start_ms" || field == "end_ms" || field == "timing_method" {
				doc["blocks"].([]any)[1].(map[string]any)[field] = "provider-value"
			} else {
				doc[field] = "provider-value"
			}
			result := execution.validateResponse(artifact.JSON(envelope))
			if !result.Invalid || result.Document != nil {
				t.Fatal("provider-owned field accepted or transmitted")
			}
		})
	}
	for _, mode := range []string{"unknown_cue", "duplicate_cue", "duplicate_key", "unknown_block_field"} {
		t.Run(mode, func(t *testing.T) {
			f, execution, envelope := minimalWireFixture(t)
			block := envelope["document"].(map[string]any)["blocks"].([]any)[1].(map[string]any)
			switch mode {
			case "unknown_cue":
				block["cue_ids"] = []string{"private-model-cue"}
			case "duplicate_cue":
				block["cue_ids"] = []string{f.source.Cues[0].ID, f.source.Cues[0].ID}
			case "unknown_block_field":
				block["invented"] = "private-model-value"
			}
			raw := artifact.JSON(envelope)
			if mode == "duplicate_key" {
				raw = strings.Replace(raw, `"title":"配置与执行的联系"`, `"title":"伪造","title":"配置与执行的联系"`, 1)
			}
			result := execution.validateResponse(raw)
			if !result.Invalid || result.Document != nil {
				t.Fatal("invalid minimal wire accepted")
			}
			safe := artifact.JSON(result)
			if strings.Contains(safe, "private-model") {
				t.Fatal("unsafe validation diagnostic")
			}
		})
	}
}

func TestSummaryGenerationMinimalWireActualBudgetOverrunNeverPublishes(t *testing.T) {
	for _, mode := range []string{"input", "output", "cumulative_input"} {
		t.Run(mode, func(t *testing.T) {
			f, _, envelope := minimalWireFixture(t)
			usage := &ai.ChatUsage{PromptTokens: 6000, CompletionTokens: 1800}
			if mode == "input" {
				usage.PromptTokens = 24001
			} else if mode == "output" {
				usage.CompletionTokens = 8193
			}
			responses := []summaryBudgetResponse{{content: artifact.JSON(envelope), usage: usage}}
			wantCalls := int32(1)
			wantPrompt, wantCompletion := usage.PromptTokens, usage.CompletionTokens
			if mode == "cumulative_input" {
				usage.PromptTokens = 18001
				responses = append([]summaryBudgetResponse{{content: `{}`, usage: &ai.ChatUsage{PromptTokens: 6000, CompletionTokens: 1000}}}, responses...)
				wantCalls = 2
				wantPrompt = 24001
				wantCompletion = 2800
			}
			client, requests, calls := summaryBudgetProvider(t, responses...)
			f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: nonStreamingGenerationClient{client}})
			err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken)
			var domain *artifact.Error
			if !errors.As(err, &domain) || domain.Code != "budget_exhausted" || calls.Load() != wantCalls {
				t.Fatalf("actual usage overrun was ignored: %v / %d", err, calls.Load())
			}
			if row, _ := f.repos.Summary.FindByTaskID(f.task.ID); row != nil {
				t.Fatal("over-budget summary published")
			}
			store := repository.NewSummaryGenerationExecutionStore(f.repos, f.task.UserID, f.task.ID, f.job.GenerationID)
			saved, _ := store.GetRun(context.Background(), f.task.UserID, f.job.GenerationID)
			if saved.MaxPromptTokens != 24000 || saved.MaxCompletionTokens != 8192 || saved.PromptTokensUsed != wantPrompt || saved.CompletionTokensUsed != wantCompletion {
				t.Fatal("actual usage hidden or frozen budget expanded")
			}
			request := <-requests
			var cap int64
			_ = json.Unmarshal(request["max_tokens"], &cap)
			if cap <= 0 || cap > 8192 {
				t.Fatal("provider output cap exceeded frozen total")
			}
		})
	}
}

func TestSummaryGenerationMinimalWireProviderInputAndDurablePublication(t *testing.T) {
	f, _, envelope := minimalWireFixture(t)
	client, requests, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: artifact.JSON(envelope), usage: &ai.ChatUsage{PromptTokens: 8000, CompletionTokens: 2000}}, summaryBudgetResponse{content: artifact.JSON(envelope), usage: &ai.ChatUsage{PromptTokens: 8000, CompletionTokens: 2000}})
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: nonStreamingGenerationClient{client}})
	if err := f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("full 328 cue track did not run exactly composition plus independent review")
	}
	request := <-requests
	var messages []ai.ChatMessage
	if err := json.Unmarshal(request["messages"], &messages); err != nil || len(messages) != 2 {
		t.Fatalf("invalid provider messages: %v", err)
	}
	var table struct {
		Fields []string            `json:"fields"`
		Rows   [][]json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal([]byte(summaryGenerationInputData(messages[1].Content)), &table); err != nil {
		t.Fatal(err)
	}
	if strings.Join(table.Fields, ",") != "cue_id,text" || len(table.Rows) != 328 {
		t.Fatal("minimal input retained immutable per-cue metadata or lost rows")
	}
	for i, row := range table.Rows {
		var id, text string
		if len(row) != 2 || json.Unmarshal(row[0], &id) != nil || json.Unmarshal(row[1], &text) != nil || id != f.source.Cues[i].ID || text != f.source.Cues[i].Text {
			t.Fatal("provider input changed source ID/text")
		}
	}
	if studyPromptTokens(messages) >= 12000 {
		t.Fatal("one complete input leaves insufficient bounded repair headroom")
	}
	row, _ := f.repos.Summary.FindByTaskID(f.task.ID)
	if row == nil {
		t.Fatal("canonical summary not published")
	}
	doc, err := summarydoc.Parse([]byte(row.DocumentJSON))
	if err != nil || len(doc.Blocks) != 7 || doc.Blocks[1].ParentID == nil || *doc.Blocks[1].ParentID != "mechanism" {
		t.Fatalf("publication lost semantic hierarchy: %v", err)
	}
	store := repository.NewSummaryGenerationExecutionStore(f.repos, f.task.UserID, f.task.ID, f.job.GenerationID)
	run, _ := store.GetRun(context.Background(), f.task.UserID, f.job.GenerationID)
	if run.PromptTokensUsed != 16000 || run.CompletionTokensUsed != 4000 || run.MaxPromptTokens != 24000 || run.MaxCompletionTokens != 8192 {
		t.Fatal("actual usage or frozen cap changed")
	}
}
