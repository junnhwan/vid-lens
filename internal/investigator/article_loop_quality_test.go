package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

// A complete/no-issues report used to publish without any independent account
// of what its delivered pixels show. This reproduces the real false-pass shape;
// the deterministic test checks the protocol, not model semantic competence.
func TestArticleLoopVisualReviewRequiresIndependentObservation(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] == nil {
			write := loopGoodWrite()
			write.Sections[0].Content[1].Figure.Caption = "画面显示72个测试样本。"
			writeReviewWireArgs(w, loopSaveTool, write)
			return
		}
		var blind map[string]any
		_ = json.Unmarshal(mustJSON(t, loopGoodReport(t, p)), &blind)
		delete(blind, "visual_observations")
		writeReviewWireArgs(w, loopReviewTool, blind)
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err == nil {
		t.Fatal("published after a complete/no-issues review with no independent pixel observations")
	}
	assertNoLoopPublication(t, s)
	draft, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	if err != nil || draft.Version != 1 {
		t.Fatalf("review protocol failure rewrote the saved article: version=%d error=%v", draft.Version, err)
	}
}

func TestArticleLoopReviewWireKeepsAdmittedSchema(t *testing.T) {
	for _, version := range []string{domain.ArticleLoopV1, domain.ArticleLoopV2} {
		s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: version}}}
		infos, err := s.loopTools(true)
		if err != nil {
			t.Fatal(err)
		}
		schema, err := infos[0].ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		var shape struct {
			Properties map[string]any `json:"properties"`
		}
		_ = json.Unmarshal(domain.JSON(schema), &shape)
		if _, has := shape.Properties["visual_observations"]; has != (version == domain.ArticleLoopV2) {
			t.Fatalf("admitted %s received wrong review wire schema", version)
		}
		for _, field := range []string{"complete", "unassessed", "topics", "issues"} {
			if _, has := shape.Properties[field]; !has {
				t.Fatalf("review wire lost %s", field)
			}
		}
	}
}

func TestArticleLoopSemanticWireDeclaresExclusiveConstructors(t *testing.T) {
	s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}}}
	infos, err := s.loopTools(false)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	var walk func(map[string]any)
	walk = func(node map[string]any) {
		properties, _ := node["properties"].(map[string]any)
		if properties["background"] != nil && properties["figure"] != nil && properties["quote"] != nil {
			found++
			variants, _ := node["oneOf"].([]any)
			if len(variants) != 7 {
				t.Error("actual author wire does not express exactly one of the seven constructors")
			}
		}
		for _, value := range node {
			switch value := value.(type) {
			case map[string]any:
				walk(value)
			case []any:
				for _, child := range value {
					if object, ok := child.(map[string]any); ok {
						walk(object)
					}
				}
			}
		}
	}
	schema, err := infos[0].ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err = json.Unmarshal(domain.JSON(schema), &root); err != nil {
		t.Fatal(err)
	}
	walk(root)
	if found == 0 {
		t.Fatal("author wire has no content constructor schema")
	}
}

func TestArticleLoopRejectedSourcesHaveExactContentPath(t *testing.T) {
	text := "合成的事实"
	_, _, _, _, err := loopCanonicalSection(loopSection{Title: "合成章节", Content: []loopContent{{Text: &text}}}, "stable", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "/content/0/sources") {
		t.Fatalf("missing sources diagnostic does not identify the field to repair: %v", err)
	}
}

func TestArticleLoopRepeatedInvalidEnvelopeNarrowsWorkspace(t *testing.T) {
	s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}, ArticleLoop: &articleLoopState{OutputSections: 40, SourceChars: 8000, Failures: map[string]int{}}}}
	msg := &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: loopSaveTool, Arguments: `{"expected_version":0,"sections":[`}}}}
	for i := 0; i < 2; i++ {
		if _, err := s.applyLoopResponse(context.Background(), articleLoopCall{Mode: "author"}, msg); err != nil {
			t.Fatal(err)
		}
	}
	if s.cp.ArticleLoop.OutputSections >= 40 {
		t.Fatal("repeated malformed output retained the same author scope")
	}
	if s.cp.ArticleLoop.SourceChars != 8000 {
		t.Fatal("output syntax failure discarded source workspace without evidence of input-capacity failure")
	}
	if s.cp.Calls != 0 {
		t.Fatal("recovery reset or invented model calls")
	}
}

func TestArticleLoopConcreteDefectCannotBecomeAdvisory(t *testing.T) {
	var payload map[string]json.RawMessage
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
		} else {
			payload = p
			w.WriteHeader(503)
		}
	})
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err == nil {
		t.Fatal("expected unavailable review")
	}
	s = reloadLoopSession(t, s)
	report := loopGoodReport(t, payload)
	report.Issues = []domain.ArticleReviewIssue{{AC: "AC05", Severity: "advisory", Location: "main caption", EvidenceIDs: []string{"f1"}, Problem: "该具体图注无法由实际像素支持", Correction: "删除没有像素依据的断言"}}
	if _, err := s.applyLoopReview(ctx, s.cp.ArticleLoop.Scopes[0], domain.JSON(report)); err != nil {
		t.Fatal(err)
	}
	if s.cp.ArticleLoop.CandidateHash == "" || len(s.cp.ArticleLoop.ReviewKeys) != 0 || !strings.Contains(string(s.cp.ArticleLoop.Feedback), "inconsistent review") {
		t.Fatal("misgraded review was approved or turned into an instruction to rewrite the candidate")
	}
	report.Issues[0].Severity = "blocking"
	if _, err := s.applyLoopReview(ctx, s.cp.ArticleLoop.Scopes[0], domain.JSON(report)); err != nil {
		t.Fatal(err)
	}
	if s.cp.ArticleLoop.CandidateHash != "" || len(s.cp.ArticleLoop.Issues) != 1 || s.cp.ArticleLoop.Issues[0].Severity != "blocking" {
		t.Fatal("corrected concrete defect did not return to the author")
	}
	assertNoLoopPublication(t, s)
}

func TestArticleLoopRevisionWorkspaceFollowsActualIssueLocations(t *testing.T) {
	s := &session{transcript: []domain.Segment{{ID: "original-a"}, {ID: "original-b"}}, cp: checkpoint{ArticleLoop: &articleLoopState{Frames: []string{"pixel-a", "pixel-b"}}}}
	a := domain.Article{Sections: []domain.Section{
		{ID: "s_affected", Blocks: []domain.Block{{ID: "b_a", CitationIDs: []string{"text-a"}, FrameIDs: []string{"pixel-a"}}}},
		{ID: "s_untouched", Blocks: []domain.Block{{ID: "b_b", CitationIDs: []string{"text-b"}, FrameIDs: []string{"pixel-b"}}}},
	}, Citations: map[string]domain.Citation{"text-a": {SegmentIDs: []string{"original-a"}}, "text-b": {SegmentIDs: []string{"original-b"}}}}
	sections, sources, frames := s.loopRevisionWorkspace(a, []domain.ArticleReviewIssue{{Location: "s_affected/b_a", EvidenceIDs: []string{"t1", "f1"}}})
	if string(domain.JSON(sections)) != `["s_affected"]` || string(domain.JSON(sources)) != `["t1"]` || string(domain.JSON(frames)) != `["pixel-a"]` {
		t.Fatalf("revision pulled unrelated saved work: %v %v %v", sections, sources, frames)
	}
}

func TestArticleLoopCandidateLocationsNeverSubstituteForOriginalEvidence(t *testing.T) {
	quote := "实际原文引文"
	a := domain.Article{Sections: []domain.Section{{ID: "s_current", Blocks: []domain.Block{{ID: "s_current_b1", CitationIDs: []string{"q_saved"}}}}}, Citations: map[string]domain.Citation{"q_saved": {ID: "q_saved", Kind: "video_text", Quote: &quote, SegmentIDs: []string{"original"}}}}
	scope := domain.ArticleReviewScope{SectionIDs: []string{"s_current"}, SourceIDs: []string{"original"}}
	report := domain.ArticleReviewReport{Complete: true, Topics: []domain.ArticleTopic{{Topic: "主题", SourceIDs: []string{"t1"}, SectionIDs: []string{"s_current"}}}, Issues: []domain.ArticleReviewIssue{{Severity: "blocking", AC: "AC06", Location: "重复段落", EvidenceIDs: []string{"s_current", "s_current_b1", "t1"}, Problem: "候选内容重复", Correction: "合并重复内容"}}}
	report = loopReviewLocations(report, a)
	if err := validateLoopReport(report, scope, a, []domain.Segment{{ID: "original"}}, nil); err != nil {
		t.Fatal("known candidate positions obscured actual original evidence", err)
	}
	if string(domain.JSON(report.Issues[0].EvidenceIDs)) != `["t1"]` || !strings.Contains(report.Issues[0].Location, "s_current_b1") {
		t.Fatal("candidate positions were lost or treated as original evidence")
	}
	report.Issues[0].EvidenceIDs = []string{"t1", "q_saved"}
	report = loopReviewLocations(report, a)
	if err := validateLoopReport(report, scope, a, []domain.Segment{{ID: "original"}}, nil); err != nil {
		t.Fatal("known quote location obscured delivered original evidence", err)
	}
	if string(domain.JSON(report.Issues[0].EvidenceIDs)) != `["t1"]` || !strings.Contains(report.Issues[0].Location, "q_saved") {
		t.Fatal("quote identity was lost or substituted for original evidence")
	}
	for _, ids := range [][]string{{"s_current"}, {"s_invented", "t1"}, {"q_saved"}, {"q_invented", "t1"}, {"q_saved", "t2"}} {
		report.Issues[0].EvidenceIDs = ids
		projected := loopReviewLocations(report, a)
		if validateLoopReport(projected, scope, a, []domain.Segment{{ID: "original"}}, nil) == nil {
			t.Fatal("candidate-only or invented evidence bypassed original evidence requirement")
		}
	}
}

func TestQuestionReadingReadinessHasNoWholeArticleObligation(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("readiness must not call a provider") })
	s.turn = &domain.Turn{Question: "只解释当前问题"}
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV9, RunID: s.claim.Run.ID, InputRevision: domain.QuestionReadingV1}
	ready, err := s.readiness(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ready["can_submit_answer"] != true {
		t.Fatal("question with no pending obligations cannot submit an answer", ready)
	}
	if _, ok := ready["can_compose"]; ok {
		t.Fatal("article composition readiness leaked into question context")
	}
	s.cp.Inspections = map[string][]string{"question-context": {"pending-pixels"}}
	ready, err = s.readiness(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ready["can_submit_answer"] != false {
		t.Fatal("pending actual pixels were bypassed")
	}
	s.cp.ExecutionContract.InputRevision = ""
	ready, err = s.readiness(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ready["can_compose"]; !ok {
		t.Fatal("historical question input changed")
	}
}

func TestQuestionReadingCatalogDoesNotRepeatOwnedIdentityMetadata(t *testing.T) {
	catalog := map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text", SnapshotID: "owned-snapshot", SegmentIDs: []string{"owned-original"}}, "f1": {ID: "f1", Kind: "video_frame", FrameID: "owned-pixels", TimeMS: domain.Ptr(int64(100))}}
	hints := questionCitationHints(catalog, map[string]any{"t1": catalog["t1"], "f1": catalog["f1"]})
	if hints["t1"] != "video_text" || strings.Contains(string(domain.JSON(hints)), "owned-original") {
		t.Fatal("question catalog retained repeated original identity records")
	}
	if !strings.Contains(string(domain.JSON(hints)), "owned-pixels") {
		t.Fatal("frame navigation identity was lost")
	}
	resolved, err := resolveAnswerReferences(compactAnswerInput{Kind: "grounded", Blocks: []domain.Block{{ID: "answer", Type: "paragraph", Origin: "video", CitationIDs: []string{"t1"}, Content: []domain.Inline{{Type: "text", Text: "合成事实"}}}}}, catalog)
	if err != nil || resolved.Citations["t1"].SnapshotID != "owned-snapshot" || resolved.Citations["t1"].SegmentIDs[0] != "owned-original" {
		t.Fatal("compact display changed authoritative citation resolution", err)
	}
}

func TestArticleLoopV1KeepsOriginalReviewAndPublication(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
		} else {
			if p["visual_claims"] != nil {
				t.Error("historical V1 run received new review input")
			}
			writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p).ArticleReviewReport)
		}
	})
	s.cp.ExecutionContract.Version = domain.ArticleLoopV1
	if err := s.engine.Store.Checkpoint(context.Background(), s.claim, s.cp); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal("historical frozen report no longer publishes", err)
	}
}

func TestArticleLoopPublicationRechecksIndependentPixelCoverage(t *testing.T) {
	var reviewPayload map[string]json.RawMessage
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
		} else {
			reviewPayload = p
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err == nil {
		t.Fatal("expected unavailable review")
	}
	s = reloadLoopSession(t, s)
	if _, err := s.applyLoopReview(ctx, s.cp.ArticleLoop.Scopes[0], domain.JSON(loopGoodReport(t, reviewPayload))); err != nil {
		t.Fatal(err)
	}
	if err := s.engine.Store.Checkpoint(ctx, s.claim, s.cp); err != nil {
		t.Fatal(err)
	}
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE run_operations SET result=result-'visual_observations' WHERE run_id=$1 AND kind='article_loop_review'", s.claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	_, draft, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.Publish(ctx, s.claim, draft.Article); err == nil {
		t.Fatal("transaction trusted approval after independent pixel observations disappeared")
	}
	if err = s.publishLoopCandidate(ctx); err == nil {
		t.Fatal("candidate replay bypassed independent pixel coverage")
	}
	assertNoLoopPublication(t, s)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestArticleLoopResponseDeadlineKeepsCandidateAndAllReviewObligations(t *testing.T) {
	s := &session{cp: checkpoint{
		Calls: 2,
		ArticleLoop: &articleLoopState{
			CandidateHash: "unchanged-candidate", ReadSources: []string{"t1", "t2"}, OutputSections: 8,
			Budget: articleLoopBudget{Calls: 20},
			Scopes: []domain.ArticleReviewScope{{ID: "whole", Global: true, SourceIDs: []string{"original1", "original2"}, SectionIDs: []string{"s1", "s2"}}},
		},
	}}
	scope := s.cp.ArticleLoop.Scopes[0]
	if err := s.recoverLoopFailure(context.Background(), articleLoopCall{Mode: "review", Scope: &scope}, &domain.WorkError{Code: "MODEL_RESPONSE_BUDGET_EXHAUSTED"}); err != nil {
		t.Fatal(err)
	}
	l := s.cp.ArticleLoop
	if l.CandidateHash != "unchanged-candidate" || s.cp.Calls != 2 || len(l.ReadSources) != 2 || len(l.ReviewKeys) != 0 || len(l.Scopes) != 3 || !l.Scopes[2].Global || len(l.Scopes[2].SourceIDs) != 0 || len(l.Scopes[0].SourceIDs)+len(l.Scopes[1].SourceIDs) != 2 {
		t.Fatal("deadline recovery lost candidate, budget, source coverage or global review")
	}
}

func TestArticleLoopKeepsCompleteValidWriteAboveRecoverySuggestion(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		w.WriteHeader(503)
	})
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err == nil {
		t.Fatal("expected paused review")
	}
	s = reloadLoopSession(t, s)
	before, err := s.engine.Store.ArticleDraft(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.cp.ArticleLoop.OutputSections = 1
	text := "实际来源中的事实。"
	write := loopSaveInput{ExpectedVersion: before.Version, Sections: []loopSection{
		{Key: "additional-a", Title: "第一处补充", Content: []loopContent{{Text: &text, Sources: []string{"t1"}}}},
		{Key: "additional-b", Title: "第二处补充", Content: []loopContent{{Text: &text, Sources: []string{"t25"}}}},
	}}
	message := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: loopSaveTool, Arguments: string(domain.JSON(write))}}}}
	progress, err := s.applyLoopResponse(ctx, articleLoopCall{Mode: "author", Slot: 200}, message)
	if err != nil || !progress {
		t.Fatalf("complete valid chapters lost to a recovery suggestion: %v %s", err, s.cp.ArticleLoop.Feedback)
	}
	after, err := s.engine.Store.ArticleDraft(ctx, s.claim.Run.ID)
	if err != nil || after.Version != before.Version+1 {
		t.Fatal("write not persisted", err)
	}
	assertNoLoopPublication(t, s)
}
