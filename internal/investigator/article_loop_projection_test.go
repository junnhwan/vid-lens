package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleLoopAuthorDraftRoundTripPreservesSemanticContentAndEvidence(t *testing.T) {
	text, quote, background := "来源事实", "事实", "单独的背景解释"
	transcript := []domain.Segment{{ID: "original", Ordinal: 0, StartMS: 0, EndMS: 1000, Text: quote}}
	catalog := map[string]domain.Citation{
		"t1": {ID: "t1", Kind: "video_text", SnapshotID: "snapshot", SegmentIDs: []string{"original"}, Range: &domain.Range{StartMS: 0, EndMS: 1000}},
		"f1": {ID: "f1", Kind: "video_frame", SnapshotID: "snapshot", FrameID: "pixels", TimeMS: domain.Ptr(int64(500))},
	}
	in := loopSection{Title: "全部语义", Content: []loopContent{
		{Text: &text, Sources: []string{"t1"}}, {Quote: &quote, Sources: []string{"t1"}},
		{Background: &background},
		{Code: &loopCode{Text: "x := 1", Language: "go"}, Sources: []string{"t1"}},
		{List: &loopList{Ordered: true, Items: []loopSourcedText{{Text: text, Sources: []string{"t1"}}}}},
		{Table: &loopTable{Columns: []string{"条件"}, Rows: []loopRow{{Cells: []string{text}, Sources: []string{"t1"}}}}},
		{Figure: &loopFigure{Frames: []string{"f1"}, Caption: "实际画面", VisibleFacts: []string{"可见事实"}, Uncertainty: []string{"画外未知"}}},
	}, Limitations: []loopSectionLimitation{{Code: "partial_text", Message: "局部原文模糊", Sources: []string{"t1"}}}}
	sec, cits, notes, limits, err := loopCanonicalSection(in, "stable", catalog, transcript)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{transcript: transcript, cp: checkpoint{ArticleLoop: &articleLoopState{Frames: []string{"pixels"}}}}
	projected, err := s.loopAuthorDraft(domain.Article{Title: "标题", Sections: []domain.Section{sec}, Citations: cits, Limitations: limits}, notes, 7)
	if err != nil || projected.ExpectedVersion != 7 || projected.Sections[0].SectionID != "stable" {
		t.Fatalf("lost authoritative identities: %+v %v", projected, err)
	}
	raw := string(domain.JSON(projected))
	for _, rendererField := range []string{`"blocks"`, `"type"`, `"citation_ids"`, `"pixels"`} {
		if strings.Contains(raw, rendererField) {
			t.Fatalf("renderer representation leaked into author workspace: %s", rendererField)
		}
	}
	var decoded loopSaveInput
	if err = loopDecode([]byte(raw), &decoded); err != nil {
		t.Fatal("author cannot submit its own workspace", err)
	}
	got, gotCits, gotNotes, gotLimits, err := loopCanonicalSection(decoded.Sections[0], "stable", catalog, transcript)
	if err != nil || domain.ContentHash(got) != domain.ContentHash(sec) || domain.ContentHash(gotCits) != domain.ContentHash(cits) || domain.ContentHash(gotNotes) != domain.ContentHash(notes) || domain.ContentHash(gotLimits) != domain.ContentHash(limits) {
		t.Fatalf("semantic projection changed persisted meaning/evidence: %v\n%s", err, domain.JSON(got))
	}
}

func TestArticleLoopRetryReviewSeparatesCitationAndPixelAliases(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("no provider expected") })
	ctx := context.Background()
	frames, err := s.engine.Store.RunFrames(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.cp.ArticleLoop = &articleLoopState{Frames: []string{frames[0].ID}, SeenFrames: []string{frames[0].ID}, ReadSources: s.loopAllSourceAliases()}
	if _, _, err = s.saveLoopSections(ctx, "seed", loopGoodWrite()); err != nil {
		t.Fatal(err)
	}
	saved, d, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Retry copies only used pixels, while canonical citations retain old names.
	cit := d.Article.Citations["f1"]
	delete(d.Article.Citations, "f1")
	cit.ID = "f4"
	d.Article.Citations["f4"] = cit
	d.Article.Sections[0].Blocks[1].CitationIDs = []string{"f4"}
	if _, err = s.engine.Store.SaveArticleDraft(ctx, s.claim, "old-frame-alias", nil, saved.Version, d); err != nil {
		t.Fatal(err)
	}
	s.cp.ArticleLoop.CandidateHash = domain.ArticleHash(d.Article)
	scope := domain.ArticleReviewScope{Global: true, FrameIDs: []string{frames[0].ID}, SectionIDs: []string{d.Article.Sections[0].ID}}
	for _, original := range s.transcript {
		scope.SourceIDs = append(scope.SourceIDs, original.ID)
	}
	input, err := s.loopInput(ctx, articleLoopCall{Mode: "review", Scope: &scope, Frames: scope.FrameIDs, Sources: s.loopAllSourceAliases()})
	if err != nil {
		t.Fatal(err)
	}
	var review map[string]json.RawMessage
	for _, msg := range input {
		var p map[string]json.RawMessage
		if json.Unmarshal([]byte(msg.Content), &p) == nil && p["candidate"] != nil {
			review = p
		}
	}
	if strings.Contains(string(review["candidate"]), `"f4"`) || strings.Contains(string(review["visual_claims"]), `"f4"`) {
		t.Fatal("old f4 citation is still presented as a current pixel alias")
	}
	if !strings.Contains(string(review["candidate"]), frames[0].ID) || !strings.Contains(string(review["citation_bindings"]), frames[0].ID) {
		t.Fatal("citation binding lost")
	}
	_, after, err := s.loadLoopDraft(ctx)
	if err != nil || domain.ArticleHash(after.Article) != domain.ArticleHash(d.Article) {
		t.Fatal("review projection mutated authoritative candidate")
	}
}

func TestArticleLoopMissingSeverityHasActionablePath(t *testing.T) {
	report := domain.ArticleReviewReport{Complete: true, Topics: []domain.ArticleTopic{{Topic: "topic"}}, Issues: []domain.ArticleReviewIssue{{AC: "AC05"}}}
	err := validateLoopReport(report, domain.ArticleReviewScope{}, domain.Article{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "issues[0].severity") || !strings.Contains(err.Error(), "blocking") {
		t.Fatalf("opaque repair feedback: %v", err)
	}
}

func TestArticleLoopActualWireDeclaresCanonicalLimitationCodes(t *testing.T) {
	infos, err := (&session{}).loopTools(false)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			props, _ := node["properties"].(map[string]any)
			if props["code"] != nil && props["message"] != nil && props["sources"] != nil {
				found++
				if string(domain.JSON(props["code"].(map[string]any)["enum"])) != string(domain.JSON(domain.LimitationCodes())) {
					t.Fatal("wire vocabulary differs from runtime validation")
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	for _, info := range infos {
		if info.Name == loopSaveTool {
			sc, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				t.Fatal(err)
			}
			var root any
			if err = json.Unmarshal(domain.JSON(sc), &root); err != nil {
				t.Fatal(err)
			}
			walk(root)
		}
	}
	if found == 0 {
		t.Fatal("no scoped limitation schema in actual save tool")
	}
}

func TestArticleLoopReviewAcceptsOnlyActuallyDeliveredIdentities(t *testing.T) {
	scope := domain.ArticleReviewScope{SourceIDs: []string{"original"}, FrameIDs: []string{"pixels"}, SectionIDs: []string{"stable"}, Global: true}
	a := domain.Article{Sections: []domain.Section{{ID: "stable"}}}
	text := []domain.Segment{{ID: "original"}, {ID: "other-original"}}
	report := domain.ArticleReviewReport{Complete: true, Topics: []domain.ArticleTopic{{Topic: "主要主题", SourceIDs: []string{"original"}, SectionIDs: []string{"stable"}}}, Issues: []domain.ArticleReviewIssue{{Severity: "blocking", AC: "AC05", Location: "stable_b1", EvidenceIDs: []string{"original", "pixels"}, Problem: "实际像素与图注冲突", Correction: "按实际像素修正"}}}
	if err := validateLoopReport(report, scope, a, text, map[string]string{"f1": "pixels", "f2": "other-pixels"}); err != nil {
		t.Fatal("rejected delivered canonical identity", err)
	}
	for _, id := range []string{"other-original", "t2", "other-pixels", "f2", "invented"} {
		report.Issues[0].EvidenceIDs = []string{id}
		if validateLoopReport(report, scope, a, text, map[string]string{"f1": "pixels", "f2": "other-pixels"}) == nil {
			t.Fatalf("accepted undelivered review identity %s", id)
		}
	}
	report.Issues = nil
	report.Topics[0].SourceIDs = []string{"t2"}
	err := validateLoopReport(report, scope, a, text, nil)
	if err == nil || !strings.Contains(err.Error(), "topics[0].source_ids") || !strings.Contains(err.Error(), `"t2"`) {
		t.Fatalf("out-of-scope feedback does not identify the exact topic and original: %v", err)
	}
}

func TestArticleLoopImportedInlineCitationsSurviveAuthorProjection(t *testing.T) {
	s := &session{transcript: []domain.Segment{{ID: "original"}}, cp: checkpoint{ArticleLoop: &articleLoopState{}}}
	a := domain.Article{Sections: []domain.Section{{ID: "chapter", Blocks: []domain.Block{
		{ID: "untouched", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "Saved source-backed paragraph."}, {Type: "citation", CitationID: "legacy_ref"}}},
		{ID: "code", Type: "code", Origin: "video", Text: "value := 1", Content: []domain.Inline{{Type: "citation", CitationID: "legacy_ref"}}},
	}}}, Citations: map[string]domain.Citation{"legacy_ref": {ID: "legacy_ref", Kind: "video_text", SegmentIDs: []string{"original"}}}}
	projected, err := s.loopAuthorDraft(a, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, content := range projected.Sections[0].Content {
		if len(content.Sources) != 1 || content.Sources[0] != "t1" {
			t.Fatalf("imported block %d lost inline evidence before author/atomic patch: %+v", i, content)
		}
	}
}

func TestArticleLoopAtomicPatchPreservesImportedUntouchedInlineEvidence(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if loopPayload(t, r)["scope"] != nil {
			w.WriteHeader(503)
			return
		}
		in := loopGoodWrite()
		in.Sections[0].Content = in.Sections[0].Content[:1]
		text := "同一章中不应被重写的旧段落，拥有自己的来源引用。"
		in.Sections[0].Content = append(in.Sections[0].Content, loopContent{Text: &text, Sources: []string{"t2"}})
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	ctx := context.Background()
	if s.engine.Run(ctx, s.claim, s.profile) == nil {
		t.Fatal("expected review interruption")
	}
	s = reloadLoopSession(t, s)
	saved, d, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range d.Article.Sections[0].Blocks {
		b := &d.Article.Sections[0].Blocks[i]
		for _, ref := range b.CitationIDs {
			b.Content = append(b.Content, domain.Inline{Type: "citation", CitationID: ref})
		}
		b.CitationIDs = nil
	}
	if saved, err = s.engine.Store.SaveArticleDraft(ctx, s.claim, "import-inline", nil, saved.Version, d); err != nil {
		t.Fatal(err)
	}
	text := "按原文修正第一段，不能破坏第二段的证据。"
	in := loopPatchInput{ExpectedVersion: saved.Version, Replacements: []loopBlockReplacement{{BlockID: d.Article.Sections[0].Blocks[0].ID, Content: loopContent{Text: &text, Sources: []string{"t1"}}}}}
	result, progress, err := s.patchLoopBlocks(ctx, "patch-import", in)
	if err != nil || !progress || result.(map[string]any)["error"] != nil {
		t.Fatalf("valid one-block repair rejected due to untouched imported inline source: %v %v", result, err)
	}
	_, after, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b := after.Article.Sections[0].Blocks[1]
	if domain.BlockText(b) != domain.BlockText(d.Article.Sections[0].Blocks[1]) || len(blockCitationIDs(b)) != 1 || blockCitationIDs(b)[0] != "t2" {
		t.Fatal("untouched paragraph or evidence changed", b)
	}
	feedback := loopWriteFeedback(map[string]any{"error": "bad replacement"}, []byte(`{}`), loopPatchTool)
	if !strings.Contains(string(feedback), `"previous_tool":"revise_blocks"`) {
		t.Fatal("block repair feedback incorrectly asks for chapter save", string(feedback))
	}
}
