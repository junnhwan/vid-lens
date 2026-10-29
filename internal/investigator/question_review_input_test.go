package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestCurrentQuestionReviewSelectsEvidenceWithinFrozenBudget(t *testing.T) {
	p := config.DefaultProfile()
	p.InputCharsLimit, p.MaxOutputTokens, p.ContextTokens = 140000, 10000, 0
	s := &session{profile: p, snapshot: domain.Snapshot{ID: "snapshot", DurationMS: 1880000}, turn: &domain.Turn{Question: "What is the key point?"}, cp: checkpoint{HarnessRevision: 16, ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: domain.QuestionInputRevision(domain.ArticleLoopV2)}}}
	catalog := map[string]domain.Citation{}
	for i := 0; i < 940; i++ {
		seg := domain.Segment{ID: fmt.Sprintf("original-%032d", i), StartMS: int64(i * 2000), EndMS: int64((i + 1) * 2000), Text: fmt.Sprintf("Original passage %03d keeps every word.", i), Origin: "platform_subtitle"}
		s.transcript = append(s.transcript, seg)
		alias := fmt.Sprint("t", i+1)
		catalog[alias] = domain.Citation{ID: alias, Kind: "video_text", SegmentIDs: []string{seg.ID}, Range: &domain.Range{StartMS: seg.StartMS, EndMS: seg.EndMS}}
	}
	candidate := map[string]any{"summary": "The first passage makes the key point.", "blocks": []domain.Block{{ID: "answer", Type: "paragraph", Content: []domain.Inline{{Type: "text", Text: "The key point."}, {Type: "citation", CitationID: "t1"}}}}}
	units, err := candidateReviewUnits(candidate, catalog)
	if err != nil {
		t.Fatal(err)
	}
	task := reviewTask{Mode: "source_text", Units: units}
	input, err := s.reviewUnitInput(context.Background(), "review", candidate, nil, catalog, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.makeTools(); err != nil {
		t.Fatal(err)
	}
	info, err := s.tools[reviewUnitsTool].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bound, err := (&meteredModel{session: s}).WithTools([]*schema.ToolInfo{info})
	if err != nil {
		t.Fatal(err)
	}
	s.reviewing = true
	s.reviewStep = &reviewStep{RequestID: "review", Units: len(units), WorkTokens: estimateTextTokens(domain.JSON(units))}
	budget, err := s.budget(context.Background(), input, bound.(*meteredModel).toolsChars)
	if err != nil {
		t.Fatalf("short answer cannot reach review with the complete source: input=%d limit=%d: %v", budget.InputChars, budget.InputCharsLimit, err)
	}
	assertQuestionReviewOriginals(t, input[1], s.transcript, "t1", "t2")
	var payload struct {
		Catalog map[string]any `json:"citation_catalog"`
	}
	if err = json.Unmarshal([]byte(input[2].Content), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Catalog) != 2 || payload.Catalog["t940"] != nil {
		t.Fatal("unrelated full-video citation catalog leaked into a focused review")
	}
	if budget.InputChars > 20000 {
		t.Fatalf("focused review still pays for the full source: %d", budget.InputChars)
	}
	if budget.InputCharsLimit != 140000 || !reflect.DeepEqual(s.profile, p) {
		t.Fatal("review silently enlarged the frozen model budget")
	}
	// Compaction cannot bypass admission when the actual complete input is too big.
	s.profile.InputCharsLimit = budget.InputChars - 1
	if _, err = s.budget(context.Background(), input, bound.(*meteredModel).toolsChars); err == nil {
		t.Fatal("compacted review bypassed the input limit")
	}
}

func assertQuestionReviewOriginals(t *testing.T, message *schema.Message, transcript []domain.Segment, selected ...string) {
	t.Helper()
	var source struct {
		Transcript struct {
			Columns []string            `json:"columns"`
			Rows    [][]json.RawMessage `json:"rows"`
		} `json:"transcript"`
	}
	if err := json.Unmarshal([]byte(message.Content), &source); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.Transcript.Columns, []string{"alias", "start_ms", "end_ms", "text", "origin"}) || len(source.Transcript.Rows) != len(selected) {
		t.Fatal("review lost the full ordered original source or its field definitions")
	}
	for i, row := range source.Transcript.Rows {
		alias := selected[i]
		var ordinal int
		if _, err := fmt.Sscanf(alias, "t%d", &ordinal); err != nil || ordinal < 1 || ordinal > len(transcript) {
			t.Fatal("invalid expected original alias")
		}
		segment := transcript[ordinal-1]
		want := []any{alias, segment.StartMS, segment.EndMS, segment.Text, segment.Origin}
		if string(domain.JSON(row)) != string(domain.JSON(want)) {
			t.Fatalf("original %d text, identity, timestamps or provenance changed", i)
		}
	}
}

func TestQuestionReviewEncodingPreservesFrozenVersionsAndCitationBindings(t *testing.T) {
	for _, revision := range []string{domain.QuestionReadingV1, domain.QuestionReadingV2, domain.QuestionReadingV3, domain.QuestionReadingV4} {
		t.Run(revision, func(t *testing.T) {
			s := &session{snapshot: domain.Snapshot{ID: "snapshot", DurationMS: 200}, turn: &domain.Turn{Question: "A focused question"}, cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: revision}}, transcript: []domain.Segment{
				{ID: "one", StartMS: 0, EndMS: 100, Text: "原文\n\"quoted\" \\ </source> 😀", Origin: "asr"},
				{ID: "two", StartMS: 100, EndMS: 250, Text: "Last complete original.", Origin: "platform_subtitle"},
			}}
			catalog := map[string]domain.Citation{
				"t2":     {ID: "t2", Kind: "video_text", SegmentIDs: []string{"two"}, Range: &domain.Range{StartMS: 100, EndMS: 200}},
				"custom": {ID: "custom", Kind: "video_text", SegmentIDs: []string{"one", "two"}, Range: &domain.Range{StartMS: 50, EndMS: 150}, Quote: domain.Ptr("original")},
				"f1":     {ID: "f1", Kind: "video_frame", FrameID: "actual-pixels", TimeMS: domain.Ptr(int64(125))},
			}
			source, payload := map[string]any{}, map[string]any{}
			if revision == domain.QuestionReadingV3 || revision == domain.QuestionReadingV4 {
				passages := []sourcePassage{}
				for i, segment := range s.transcript {
					passages = append(passages, sourcePassage{Segment: segment, Alias: fmt.Sprint("t", i+1)})
				}
				source["transcript"] = passages
			}
			if err := s.questionReviewReading(source, payload, catalog); err != nil {
				t.Fatal(err)
			}
			bindings := payload["citation_catalog"].(map[string]any)
			if bindings["t2"] != "video_text" || !reflect.DeepEqual(bindings["f1"], catalog["f1"]) {
				t.Fatal("review lost canonical clipped-window or pixel binding")
			}
			custom := bindings["custom"].(map[string]any)
			if !reflect.DeepEqual(custom["originals"], []string{"t1", "t2"}) || custom["range"] != catalog["custom"].Range || custom["verbatim_quote"] != catalog["custom"].Quote {
				t.Fatal("custom source binding was changed by compaction")
			}
			if revision == domain.QuestionReadingV3 || revision == domain.QuestionReadingV4 {
				assertQuestionReviewOriginals(t, schema.UserMessage(string(domain.JSON(source))), s.transcript, "t1", "t2")
				if source["question"] != s.turn.Question || s.boundedReviewProtocol() == "evidence-review-2026-10-21-v12" {
					t.Fatal("new review lost question scope or reused a historical review identity")
				}
			} else if !reflect.DeepEqual(source["transcript"], readingOriginals(s.transcript, nil)) || s.boundedReviewProtocol() != "evidence-review-2026-10-21-v12" {
				t.Fatal("historical review input or identity was silently upgraded")
			}
		})
	}
}

func TestScopedQuestionReviewKeepsDistantCitationsAndFrameNeighbors(t *testing.T) {
	s := indexedFixture()
	s.turn = &domain.Turn{Question: "Compare both stages"}
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV3}
	catalog := map[string]domain.Citation{}
	for i, segment := range s.transcript {
		alias := fmt.Sprint("t", i+1)
		catalog[alias] = domain.Citation{ID: alias, Kind: "video_text", SegmentIDs: []string{segment.ID}, Range: &domain.Range{StartMS: segment.StartMS, EndMS: segment.EndMS}}
	}
	catalog["f1"] = domain.Citation{ID: "f1", Kind: "video_frame", FrameID: "late-frame", TimeMS: domain.Ptr(s.transcript[65].StartMS + 1)}
	// A summary can be reviewed separately from its body; its inherited source
	// dependencies must survive even though the summary value has no inline IDs.
	task := reviewTask{Mode: "multimodal", Units: []reviewUnit{{ID: "summary", Value: "Compare both stages", CitationAliases: []string{"t2", "t50", "f1"}, FrameIDs: []string{"late-frame"}}}}
	source, err := s.indexedReviewSource(task, catalog)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := questionReviewCatalog(source, catalog)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"t1", "t2", "t3", "t49", "t50", "t51", "t65", "t66", "t67"}
	payload := map[string]any{}
	if err = s.questionReviewReading(source, payload, catalog); err != nil {
		t.Fatal(err)
	}
	assertQuestionReviewOriginals(t, schema.UserMessage(string(domain.JSON(source))), s.transcript, want...)
	if len(scoped) != len(want)+1 || scoped["f1"].FrameID != "late-frame" || scoped["t20"].ID != "" {
		t.Fatal("scoped review lost needed evidence or admitted unread originals")
	}
	if !strings.Contains(source["text_access"].(string), "not_assessed") {
		t.Fatal("missing evidence could silently count as reviewed")
	}
	report := reviewUnitReport{RequestID: "r", Results: []reviewUnitResult{{UnitID: "summary", Verdict: "contradicted", Issues: []reviewUnitIssue{{Claim: "Compare", Reason: "Original contradicts this claim", CitationIDs: []string{"t20"}}}}}}
	if err = validateReviewUnits(report, "r", task, scoped); err == nil {
		t.Fatal("reviewer cited a known but undelivered original")
	}
	report.Results[0].Verdict, report.Results[0].Issues = "not_assessed", []reviewUnitIssue{}
	if err = validateReviewUnits(report, "r", task, scoped); err != nil {
		t.Fatal("missing evidence cannot be returned for author retrieval", err)
	}
}

func TestScopedQuestionReviewRetrievesUncitedQuestionWithoutInliningVideo(t *testing.T) {
	s := indexedFixture()
	s.turn = &domain.Turn{Question: "unique-target"}
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV3}
	s.transcript[20].Text = "unique-target"
	task := reviewTask{Mode: "source_text", Units: []reviewUnit{{ID: "u", Value: "A question with no citations yet"}}}
	source, err := s.indexedReviewSource(task, nil)
	if err != nil {
		t.Fatal(err)
	}
	passages := source["transcript"].([]sourcePassage)
	got := []string{}
	for _, passage := range passages {
		got = append(got, passage.Alias)
	}
	if !slices.Equal(got, []string{"t20", "t21", "t22"}) {
		t.Fatalf("uncited review ignored question retrieval: %v", got)
	}
	if err = s.questionReviewReading(map[string]any{}, map[string]any{}, nil); err == nil {
		t.Fatal("missing scoped source silently fell back to the full transcript")
	}
}
