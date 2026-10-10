package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
)

func TestSummaryGenerationCueTablePreservesExactOpaqueFacts(t *testing.T) {
	start, end := int64(1160), int64(3400)
	cues := []summaryGenerationCue{{"asr-window-0:0:17", "原文\n含引号\"", &start, &end, "asr_window"}, {"unknown", "未知", nil, nil, "unknown"}, {"asr-window-0:0:17", "同一cue下一段", &start, &end, "asr_window"}}
	input := summaryGenerationCueInput(cues)
	decoded, ok := decodeSummaryGenerationCues(input)
	if !ok || !reflect.DeepEqual(cues, decoded) {
		t.Fatal("cue table changed frozen facts")
	}
	if strings.Count(input, `"start_ms"`) != 1 || strings.Count(input, `"asr_window"`) != 1 {
		t.Fatal("immutable labels repeated for each cue")
	}
	if summaryGenerationOutputDemand(input) != summaryGenerationOutputDemand(summaryCueInputPrefix+artifact.JSON(cues)) {
		t.Fatal("compact table changed output allocation")
	}
}

func TestSummaryGenerationInvalidReferenceCheckpointHasSafePath(t *testing.T) {
	f := newGenerationFixture(t, false)
	raw, err := f.chat.Chat(context.Background(), []ai.ChatMessage{{Content: artifact.JSON(f.source.Cues)}})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &envelope)
	var doc summarydoc.Document
	_ = json.Unmarshal(envelope["document"], &doc)
	doc.Blocks[0].SourceRefs[0].CueIDs = []string{"sk-private-provider-text"}
	envelope["document"] = json.RawMessage(artifact.JSON(doc))
	invalid := artifact.JSON(envelope)
	client, _, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: invalid}, summaryBudgetResponse{content: raw})
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, summaryQualityFactory{chat: nonStreamingGenerationClient{client}})
	if err = f.svc.Generate(context.Background(), f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	var steps []model.AgentStep
	if err = f.db.Where("run_id=?", f.job.GenerationID).Order("sequence").Find(&steps).Error; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(steps) != 2 {
		t.Fatal("bounded repair did not execute")
	}
	var checkpoint summaryGenerationCheckpoint
	_ = json.Unmarshal([]byte(steps[0].ResultCheckpoint), &checkpoint)
	if checkpoint.ValidationCode != "unknown_source_cue" || checkpoint.ValidationPath != "document.blocks[0].source_refs[0].cue_ids" || strings.Contains(steps[0].ResultCheckpoint, "sk-private") {
		t.Fatalf("unsafe or opaque validation diagnostic: %+v", checkpoint)
	}
}
