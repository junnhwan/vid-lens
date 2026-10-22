package investigator

import (
	"encoding/json"
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
}
