package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
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

func TestOneSourceResponseCannotSpendSixCorrectionTurnsOnOldCards(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 16
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV4, RunID: s.claim.Run.ID}
	for i := range s.transcript {
		s.transcript[i].Text = strings.Repeat("原文重要条件与结论。", 650)
	}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_ = s.sourceContext()
		chunk := s.sourceChunks()[i]
		note := sourceNote{ChunkID: chunk.ID, Summary: strings.Repeat("本段材料保留主要结论和原始条件，原文继续可读取。", 3), EvidenceIDs: []string{chunk.Passages[0].Alias}}
		if body, err := planCall(s, fmt.Sprint("prime-card-", i), "record_source_notes", note); err != nil || body["saved"] != true {
			t.Fatal(body, err)
		}
	}
	_ = s.sourceContext()
	s.cp.Calls = 3
	chunk := s.sourceChunks()[2]
	bad := sourceNote{ChunkID: chunk.ID, Summary: strings.Repeat("本段有充分文字材料需要整理并保持原文引用。", 3), EvidenceIDs: []string{}}
	for i := 0; i < 16; i++ {
		bad.EvidenceIDs = append(bad.EvidenceIDs, chunk.Passages[0].Alias)
	}
	if body, err := planCall(s, "current-card-too-many-references", "record_source_notes", bad); err != nil || body["error"] == nil {
		t.Fatal(body, err)
	}
	for i := 0; i < 6; i++ {
		old := s.cp.SourceNotes[i%2]
		old.Summary += "再次改写不应覆盖已保存材料。"
		if _, err := planCall(s, fmt.Sprint("old-card-rewrite-", i), "record_source_notes", old); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.cp.SourceNotes) != 2 || s.cp.Progress.Stalled != 1 {
		t.Fatalf("one response exhausted independent correction turns: saved=%d stalled=%d", len(s.cp.SourceNotes), s.cp.Progress.Stalled)
	}
	s.cp.Calls++
	if _, err := planCall(s, "next-response-still-invalid", "record_source_notes", bad); err != nil || s.cp.Progress.Stalled != 2 {
		t.Fatal("a new ineffective response must still consume a correction turn", s.cp.Progress, err)
	}
	s.cp.Calls++
	bad.EvidenceIDs = bad.EvidenceIDs[:1]
	if body, err := planCall(s, "correct-current-card", "record_source_notes", bad); err != nil || body["saved"] != true {
		t.Fatal(body, err)
	}
	old := s.cp.SourceNotes[0]
	old.Summary += "不能覆盖的旧材料。"
	if body, err := planCall(s, "duplicate-after-progress", "record_source_notes", old); err != nil || body["ignored_update"] != true || s.cp.Progress.Stalled != 0 || len(s.cp.SourceNotes) != 3 {
		t.Fatal("duplicate calls after real progress spent a new correction turn", body, s.cp.Progress, err)
	}
}

func TestSourceNoteSchemaAndFeedbackIdentifyTheCurrentCardAndActualLimits(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 16
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV4, RunID: s.claim.Run.ID}
	for i := range s.transcript {
		s.transcript[i].Text = strings.Repeat("原文重要条件与结论。", 650)
	}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	_ = s.sourceContext()
	chunk := s.sourceChunks()[0]
	info, err := s.tools["record_source_notes"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(mustSchemaJSON(sc), &root); err != nil {
		t.Fatal(err)
	}
	props := root["properties"].(map[string]any)
	text := props["summary"].(map[string]any)
	refs := props["evidence_ids"].(map[string]any)
	ids := props["chunk_id"].(map[string]any)["enum"].([]any)
	if text["minLength"] != float64(30) || text["maxLength"] != float64(1600) || refs["minItems"] != float64(1) || refs["maxItems"] != float64(12) || refs["uniqueItems"] != true || len(ids) != 1 || ids[0] != chunk.ID || len(refs["items"].(map[string]any)["enum"].([]any)) == 0 {
		t.Fatal("provider did not receive executable card constraints", root)
	}
	bad := sourceNote{ChunkID: chunk.ID, Summary: strings.Repeat("重要细节", 401), EvidenceIDs: []string{chunk.Passages[0].Alias}}
	feedback, err := planCall(s, "card-actual-limits", "record_source_notes", bad)
	if err != nil || feedback["error"] == nil || feedback["current_chunk_id"] != chunk.ID || feedback["summary_characters"] != float64(1604) || feedback["evidence_count"] != float64(1) || feedback["allowed_evidence_ids"] == nil {
		t.Fatal("rejection did not explain the actual error and correction", feedback, err)
	}
}

func TestSourceSurveyDoesNotRepeatDraftCatalogAndKeepsCurrentOriginals(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 16
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV4, RunID: s.claim.Run.ID}
	for i := range s.transcript {
		s.transcript[i].Text = strings.Repeat("原文重要条件与结论。", 650)
	}
	s.forceDraftSubmit = true
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{schema.SystemMessage("frozen"), {Role: schema.User, Name: sourceContextName, Content: string(domain.JSON(s.sourceContext()))}, {Role: schema.User, Name: "vidlens_draft_state", Content: "stale draft catalog"}}}
	if _, _, err := (&middleware{s: s}).BeforeModelRewriteState(context.Background(), state, nil); err != nil {
		t.Fatal(err)
	}
	var source map[string]json.RawMessage
	for _, message := range state.Messages {
		if message.Name == "vidlens_draft_state" {
			t.Fatal("source survey repeated an irrelevant draft/citation catalog")
		}
		if message.Name == sourceContextName {
			if err := json.Unmarshal([]byte(message.Content), &source); err != nil {
				t.Fatal(err)
			}
		}
	}
	var current struct {
		Passages []sourcePassage `json:"passages"`
	}
	if json.Unmarshal(source["current_chunk"], &current) != nil || len(current.Passages) == 0 || string(domain.JSON(current.Passages)) != string(domain.JSON(s.sourceChunks()[0].Passages)) || s.forceDraftSubmit {
		t.Fatal("survey lost exact current source or bypassed its completion gate")
	}
}

func TestSourceCardProviderSchemaAdvancesAfterTheAgentBindsItsTools(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	s.cp.HarnessRevision = 16
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV4, RunID: s.claim.Run.ID}
	for i := range s.transcript {
		s.transcript[i].Text = strings.Repeat("原文重要条件与结论。", 650)
	}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	_ = s.sourceContext()
	boundInfo, err := s.tools["record_source_notes"].Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := s.sourceChunks()[0]
	s.cp.SourceNotes = []sourceNote{{ChunkID: first.ID, Summary: strings.Repeat("已保存的主要事实与必要条件。", 4), EvidenceIDs: []string{first.Passages[0].Alias}}}
	_ = s.sourceContext()
	current := s.sourceChunks()[1]
	var received string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name       string `json:"name"`
					Parameters struct {
						Properties map[string]struct {
							Enum []string `json:"enum"`
						} `json:"properties"`
					} `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, tool := range body.Tools {
			if tool.Function.Name == "record_source_notes" {
				received = tool.Function.Parameters.Properties["chunk_id"].Enum[0]
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"complete result\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	model := verifiedFixtureRoute(t, s.engine.Store, "source-schema-fixture", upstream.URL, "glm-5.3-flash")
	var configID string
	if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT config_id FROM model_revisions WHERE id=$1", model.RevisionID).Scan(&configID); err != nil {
		t.Fatal(err)
	}
	s.claim.Run.ConfigID = configID
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE job_runs SET config_id=$2 WHERE id=$1", s.claim.Run.ID, configID); err != nil {
		t.Fatal(err)
	}
	s.profile, err = s.engine.Store.Profile(ctx, configID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&meteredModel{session: s, tools: []*schema.ToolInfo{boundInfo}}).Generate(ctx, []*schema.Message{schema.UserMessage("Current original source delivered.")}); err != nil {
		t.Fatal(err)
	}
	if received != current.ID {
		t.Fatalf("provider received stale bound chunk %s instead of %s", received, current.ID)
	}
}
