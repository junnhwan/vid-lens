package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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

// A provider may refer to opaque cue IDs but must choose independent legal
// block IDs. The existing subtitle fixture derives block IDs from cue IDs.
type nativeASRGenerationClient struct{ fixture *generationFixtureChat }

func (c nativeASRGenerationClient) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	raw, err := c.fixture.Chat(ctx, messages)
	if err != nil {
		return "", err
	}
	var envelope struct {
		PublicTitle   string                    `json:"public_title"`
		PublicSummary string                    `json:"public_summary"`
		Document      summarydoc.Document       `json:"document"`
		Candidates    []repository.TagCandidate `json:"tag_candidates"`
	}
	if err = json.Unmarshal([]byte(raw), &envelope); err != nil {
		return "", err
	}
	for i := range envelope.Document.Blocks {
		envelope.Document.Blocks[i].ID = fmt.Sprintf("asr-block-%d", i)
	}
	return artifact.JSON(envelope), nil
}

type nativeASRGenerationFactory struct{ client nativeASRGenerationClient }

func (f nativeASRGenerationFactory) NewChatClient(ai.Profile) (ai.ChatClient, error) {
	return f.client, nil
}

func TestSummaryGenerationNativeASRCueProviderJSONPublishesDurably(t *testing.T) {
	f := newGenerationFixture(t, false)
	ctx := context.Background()
	const cueID, words = "asr-window-0:0:17", "Native ASR facts."
	start, end, windowEnd := int64(1000), int64(2500), int64(10000)
	textStart, textEnd, join := 0, 17, ""
	canonical, err := textsource.Canonicalize(textsource.Snapshot{
		Kind: textsource.KindASR, Identity: f.source.Identity, ParserVersion: "asr-provenance-v1",
		Cues: []textsource.Cue{{ID: cueID, Order: 1, Text: words, RawText: words, StartMS: &start, EndMS: &end, TimingMethod: "asr_native", JoinBefore: &join,
			RawRefs: []textsource.RawCueRef{{ID: cueID, Order: 1, RawText: words, ObservationID: "asr-window-0", ObservationOrder: 1, TextStart: &textStart, TextEnd: &textEnd, StartMS: &start, EndMS: &windowEnd, TimingMethod: "asr_window",
				NativeTimings: []textsource.NativeTiming{{SegmentIndex: 0, TextStart: 0, TextEnd: 17, StartMS: start, EndMS: end, Method: "asr_native"}}}}}},
	}, textsource.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	published, err := f.repos.PublishTextSource(ctx, repository.PublishTextSourceRequest{UserID: f.task.UserID, TaskID: f.task.ID, ExpectedActiveSourceID: f.source.ID, Snapshot: canonical}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.source, err = f.repos.TextSource.Read(ctx, f.task.UserID, f.task.ID, published.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.task, err = f.repos.Task.FindByID(f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var frozen processing.GenerationSnapshot
	if err = json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	frozen.SourceID, frozen.SourceDigest = f.source.ID, f.source.SourceDigest
	f.job.InputSourceID, f.job.InputText, f.job.InputSnapshotJSON = f.source.ID, f.source.CanonicalText, artifact.JSON(frozen)
	if err = f.db.Model(f.job).Updates(map[string]any{"input_source_id": f.job.InputSourceID, "input_text": f.job.InputText, "input_snapshot_json": f.job.InputSnapshotJSON}).Error; err != nil {
		t.Fatal(err)
	}
	f.chat.source = f.source
	f.svc = NewSummaryGenerationService(f.repos, f.profiles, nativeASRGenerationFactory{client: nativeASRGenerationClient{fixture: f.chat}})
	if err = f.svc.Generate(ctx, f.task, f.job, f.job.ProcessingToken); err != nil {
		t.Fatal(err)
	}
	if len(f.chat.calls) != 1 || len(f.chat.covered) != 1 || f.chat.covered[0].ID != cueID {
		t.Fatal("ASR cue was omitted from provider input or needed a repair call")
	}
	summary, err := f.repos.Summary.FindByTaskID(f.task.ID)
	if err != nil || summary == nil {
		t.Fatalf("summary not durably published: %v", err)
	}
	doc, err := summarydoc.Parse([]byte(summary.DocumentJSON))
	if err != nil {
		t.Fatal(err)
	}
	validation, err := f.repos.SummaryValidationContext(ctx, f.task.UserID, f.task.ID, f.source.ID, f.source.SourceDigest, f.job.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = summarydoc.Validate(doc, validation); err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) != 1 || len(doc.Blocks[0].SourceRefs) != 1 {
		t.Fatal("missing canonical ASR block/source reference")
	}
	ref := doc.Blocks[0].SourceRefs[0]
	if doc.Blocks[0].ID != "asr-block-0" || !reflect.DeepEqual(ref.CueIDs, []string{cueID}) || ref.StartMS == nil || *ref.StartMS != start || ref.EndMS == nil || *ref.EndMS != end || ref.TimingMethod != "asr_native" {
		t.Fatal("canonical summary lost the exact authorized native ASR reference")
	}
	markdown, err := summarydoc.Markdown(doc)
	if err != nil || markdown != summary.Content || !strings.Contains(markdown, "00:01.000–00:02.500（asr_native）") {
		t.Fatal("durable Markdown projection diverged from native timing")
	}
	digest, err := summarydoc.Digest(doc)
	if err != nil || summary.ContentDigest != digest || summary.ContentHashKind != summarydoc.HashKind || summary.SourceID != f.source.ID || summary.GeneratedVersion != 1 {
		t.Fatal("canonical summary publication metadata diverged")
	}
	run, err := repository.NewSummaryGenerationExecutionStore(f.repos, f.task.UserID, f.task.ID, f.job.GenerationID).GetRun(ctx, f.task.UserID, f.job.GenerationID)
	if err != nil || run == nil || run.Status != model.AgentRunStatusCompleted || run.LLMCallsUsed != 1 {
		t.Fatalf("generation not completed after durable publication: %v", err)
	}
	if err = f.svc.Generate(ctx, f.task, f.job, f.job.ProcessingToken); err != nil || len(f.chat.calls) != 1 {
		t.Fatalf("durable ASR publication replay repeated provider call: %v", err)
	}
}
