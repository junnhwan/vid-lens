package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func indexedFixture() *session {
	s := &session{profile: config.DefaultProfile(), snapshot: domain.Snapshot{ID: "snapshot", DurationMS: 3600000}, cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV4}}}
	for i := 0; i < 80; i++ {
		s.transcript = append(s.transcript, domain.Segment{ID: fmt.Sprint("segment-", i), Ordinal: i * 2, StartMS: int64(i) * 45000, EndMS: int64(i+1) * 45000, Text: strings.Repeat(fmt.Sprint("完整原文条件", i, "。"), 160), Origin: "asr"})
	}
	return s
}

func TestSourceIndexPreservesEveryRuneAndCoarseTimeIncludingGiantCues(t *testing.T) {
	text := strings.Repeat("完整条件与中文😀", 14000)
	segment := domain.Segment{ID: "coarse", Ordinal: 70, StartMS: 1000, EndMS: 61000, Text: text, Origin: "asr"}
	chunks := buildSourceChunks([]domain.Segment{segment}, 6000)
	var restored strings.Builder
	for _, chunk := range chunks {
		for _, p := range chunk.Passages {
			if p.ID != segment.ID || p.StartMS != segment.StartMS || p.EndMS != segment.EndMS || p.Alias != "t1" || !utf8.ValidString(p.Text) {
				t.Fatal("cue identity, timing or Unicode changed", p)
			}
			restored.WriteString(p.Text)
		}
	}
	if restored.String() != text || len(chunks) < 2 {
		t.Fatal("index silently omitted or duplicated original text")
	}
	changed := segment
	changed.Text += "changed"
	if a, b := buildSourceChunks([]domain.Segment{segment}, 6000), buildSourceChunks([]domain.Segment{changed}, 6000); a[len(a)-1].ID == b[len(b)-1].ID {
		t.Fatal("changed source retained its chunk identity")
	}
}

func TestSourcePagesHaveCompleteStableCursorsAndKeepTemporalNeighbors(t *testing.T) {
	s := indexedFixture()
	all := []string{}
	for _, p := range s.transcript {
		all = append(all, p.ID)
	}
	in := indexedReadInput{readInput: readInput{SegmentIDs: all}}
	restored := map[string]string{}
	pages := 0
	for {
		page := s.sourcePage(in)
		pages++
		for _, p := range page["segments"].([]sourcePassage) {
			restored[p.ID] += p.Text
		}
		if !page["has_more"].(bool) {
			break
		}
		next := page["next_offset"].(int)
		if next <= in.Offset {
			t.Fatal("pagination does not advance", page)
		}
		in.Offset = next
	}
	for _, seg := range s.transcript {
		if restored[seg.ID] != seg.Text {
			t.Fatal("paged source lost text for non-contiguous original ordinal", seg.ID)
		}
	}
	if pages < 2 {
		t.Fatal("unbounded page")
	}
	neighbor := s.sourcePage(indexedReadInput{readInput: readInput{SegmentIDs: []string{"t41"}}})
	if len(neighbor["segments"].([]sourcePassage)) != 3 {
		t.Fatal("temporal neighbors missing", neighbor)
	}
	if s.sourcePage(indexedReadInput{ChunkIDs: []string{"invented"}})["error"] == nil {
		t.Fatal("unknown chunk silently looked complete")
	}
}

func TestSourceRetrievalRanksDistinctiveLateEvidenceAndChineseKeywords(t *testing.T) {
	transcript := []domain.Segment{}
	for i := 0; i < 40; i++ {
		transcript = append(transcript, domain.Segment{ID: fmt.Sprint(i), Text: "The system introduces a common model example."})
	}
	transcript = append(transcript, domain.Segment{ID: "late", Text: "The model uses the distinctive zephyr algorithm."}, domain.Segment{ID: "zh", Text: "长视频的分层摘要需要保留真实证据。"})
	if found := sourceSearch(transcript, "model zephyr", nil, nil); len(found) == 0 || found[0].ID != "late" {
		t.Fatal("search retained first-12 bias", found)
	}
	if found := sourceSearch(transcript, "怎么进行分层摘要", nil, nil); len(found) == 0 || found[0].ID != "zh" {
		t.Fatal("Chinese query became an unmatched whole sentence", found)
	}
}

func TestSourceSurveyKeepsEveryChunkAndRequiresDeliveredOriginalReferences(t *testing.T) {
	s := indexedFixture()
	if !s.sourceSurveyPending() {
		t.Fatal("long source was sent as one prompt")
	}
	for i, chunk := range s.sourceChunks() {
		payload := s.sourceContext()
		if payload["current_chunk"].(map[string]any)["id"] != chunk.ID || len(s.cp.SourceNotes) != i {
			t.Fatal("coverage order changed")
		}
		note := sourceNote{ChunkID: chunk.ID, Summary: strings.Repeat("保留本段原文的主要条件、数字与转折。", 4), EvidenceIDs: []string{chunk.Passages[0].Alias}}
		if err := validateSourceNote(note, chunk); err != nil {
			t.Fatal(err)
		}
		bad := note
		bad.EvidenceIDs = []string{"invented"}
		if validateSourceNote(bad, chunk) == nil {
			t.Fatal("a note cited a foreign source")
		}
		s.cp.SourceNotes = append(s.cp.SourceNotes, note)
	}
	if s.sourceSurveyPending() {
		t.Fatal("complete survey did not unlock planning")
	}
	if s.sourceContext()["current_chunk"] != nil {
		t.Fatal("old source page remained after completed survey")
	}
	if utf8.RuneCount(domain.JSON(s.sourceContext())) >= utf8.RuneCount(domain.JSON(s.transcript))/2 {
		t.Fatal("source survey did not reduce ongoing context")
	}
	bad := *s
	bad.cp = s.cp
	bad.cp.SourceNotes = append([]sourceNote{}, s.cp.SourceNotes...)
	bad.cp.SourceNotes[0].ChunkID = "stale"
	if err := bad.validateExecution(); err == nil {
		t.Fatal("stale source cards survived execution validation")
	}
}

func TestIndexedReadProjectionKeepsProtocolAndDoesNotPretendTextIsStillPresent(t *testing.T) {
	msgs := []*schema.Message{schema.SystemMessage("rules")}
	for i := 0; i < 4; i++ {
		id := fmt.Sprint(i)
		msgs = append(msgs, &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: id, Function: schema.FunctionCall{Name: "read_source", Arguments: "{}"}}}}, &schema.Message{Role: schema.Tool, ToolCallID: id, ToolName: "read_source", Content: fmt.Sprint("original-", i)})
	}
	projected := projectIndexedSourceReads(msgs)
	if err := validateProtocol(projected); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(projected[2].Content, "original-0") || !strings.Contains(projected[2].Content, "archived_source_read") || projected[len(projected)-1].Content != "original-3" {
		t.Fatal("raw reads were accumulated or fabricated")
	}
}

func TestSourceNotesPersistAndCannotSkipUndeliveredChunks(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 16
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV4, RunID: s.claim.Run.ID}
	for i := range s.transcript {
		s.transcript[i].Text = strings.Repeat("原文重要条件与结论。", 350)
	}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	_ = s.sourceContext()
	chunks := s.sourceChunks()
	if len(chunks) < 2 {
		t.Fatal("fixture not chunked")
	}
	note := sourceNote{ChunkID: chunks[0].ID, Summary: strings.Repeat("本段讨论主要结论与适用条件，需要按原文引用。", 4), EvidenceIDs: []string{chunks[0].Passages[0].Alias}}
	if result, err := planCall(s, "notes-1", "record_source_notes", note); err != nil || result["saved"] != true {
		t.Fatal(result, err)
	}
	if result, err := planCall(s, "notes-replay", "record_source_notes", note); err != nil || result["saved"] != true || len(s.cp.SourceNotes) != 1 {
		t.Fatal("note replay duplicated progress", result, err)
	}
	next := note
	next.ChunkID = chunks[1].ID
	next.EvidenceIDs = []string{chunks[1].Passages[0].Alias}
	if result, err := planCall(s, "notes-skip", "record_source_notes", next); err != nil || result["error"] == nil {
		t.Fatal("accepted material never delivered", result, err)
	}
	run, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var cp checkpoint
	if err = json.Unmarshal(run.Checkpoint, &cp); err != nil || len(cp.SourceNotes) != 1 {
		t.Fatal("notes did not survive checkpoint", err)
	}
}

func TestSourceSurveyHasItsOwnOutputBudgetAndLeavesFrozenAdaptersAlone(t *testing.T) {
	s := indexedFixture()
	s.cp.HarnessRevision = 16
	s.cp.Composing = true
	s.profile.InputCharsLimit = 500000
	s.profile.MaxOutputTokens = 32768
	policy, _ := domain.InterpretationPolicy("standard")
	s.spec.DetailPolicy = &policy
	budget, err := s.budget(context.Background(), nil, 0)
	if err != nil || budget.Phase != "source_survey" || budget.OutputTokens != 8192 {
		t.Fatal("source card inherited dense whole-article output reservation", budget, err)
	}
	s.cp.ExecutionContract.Version = domain.ExecutionContractV3
	budget, err = s.budget(context.Background(), nil, 0)
	if err != nil || budget.Phase != "compose" || budget.OutputTokens != 32768 {
		t.Fatal("frozen adapter received the new source budget", budget, err)
	}
}

func TestV4WriterDoesNotReserveWholeTranscriptOutputForEveryBatch(t *testing.T) {
	s := indexedFixture()
	s.cp.HarnessRevision = 16
	s.cp.Composing = true
	s.profile.InputCharsLimit = 2000000
	s.profile.MaxOutputTokens = 393216
	s.profile.ContextTokens = 1048576
	policy, _ := domain.InterpretationPolicy("standard")
	s.spec.DetailPolicy = &policy
	for _, chunk := range s.sourceChunks() {
		s.cp.SourceNotes = append(s.cp.SourceNotes, sourceNote{ChunkID: chunk.ID})
	}
	budget, err := s.budget(context.Background(), nil, 0)
	if err != nil || budget.Phase != "compose" || budget.OutputTokens != policy.CompositionOutputTokens {
		t.Fatal("long source enlarged a single chapter-batch output reservation", budget, err)
	}
	s.cp.ExecutionContract.Version = domain.ExecutionContractV3
	legacy, err := s.budget(context.Background(), nil, 0)
	if err != nil || legacy.OutputTokens <= budget.OutputTokens {
		t.Fatal("frozen dense-writer reservation changed", legacy, err)
	}
}
