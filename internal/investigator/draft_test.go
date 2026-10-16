package investigator

import (
	"encoding/json"
	"strings"
	"testing"
	"vidlens/internal/domain"
)

func TestArticleDraftProtocolUsesBoundedSubmissionTools(t *testing.T) {
	s := &session{cp: checkpoint{LayoutVersion: "source-context-v3"}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"begin_article", "save_article_sections", "submit_article_draft"} {
		if s.tools[name] == nil {
			t.Errorf("new article protocol has no %s; still requires a monolithic JSON candidate", name)
		}
	}
	if s.tools["submit_article"] != nil {
		t.Error("new article protocol still exposes the monolithic submission schema")
	}
}

func TestDraftBatchPreservesAcceptedChaptersAndCanonicalSources(t *testing.T) {
	d := articleDraft{Title: "全片", Plan: []sectionPlan{{ID: "one", Title: "第一章"}, {ID: "two", Title: "第二章"}}, Sections: map[string]domain.Section{}, Citations: map[string]domain.Citation{}}
	text := domain.Citation{ID: "t1", Kind: "video_text", SnapshotID: "owned-snapshot", Range: &domain.Range{StartMS: 0, EndMS: 1000}, SegmentIDs: []string{"owned-segment"}, TextOrigin: "platform_subtitles"}
	frame := domain.Citation{ID: "f1", Kind: "video_frame", SnapshotID: "owned-snapshot", FrameID: "owned-frame", TimeMS: domain.Ptr(int64(500))}
	catalog := map[string]domain.Citation{"t1": text, "f1": frame}
	first := domain.Section{ID: "one", Title: "ignored", Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "真实内容"}, {Type: "citation", CitationID: "t1"}}}, {Type: "figure", Origin: "video", FrameIDs: []string{"f1"}, CitationIDs: []string{"f1"}}}}
	accepted, err := assembleDraftBatch(d, []domain.Section{first}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	baseline := string(domain.JSON(accepted))
	bad := domain.Section{ID: "two", Blocks: []domain.Block{{Type: "paragraph", Content: []domain.Inline{{Type: "citation", CitationID: "invented"}}}}}
	if _, err = assembleDraftBatch(accepted, []domain.Section{bad}, catalog); err == nil {
		t.Fatal("invented source accepted")
	}
	if string(domain.JSON(accepted)) != baseline {
		t.Fatal("rejected next chapter changed accepted draft")
	}
	if accepted.Sections["one"].Blocks[1].FrameIDs[0] != "owned-frame" || accepted.Citations["t1"].SnapshotID != "owned-snapshot" {
		t.Fatal("server did not resolve owned citation metadata")
	}
	if accepted.Sections["one"].Title != "第一章" || accepted.Sections["one"].Blocks[0].ID != "one_b1" {
		t.Fatal("unstable chapter/block identity")
	}
	// The incident's premature top-level closure is rejected without consuming any saved chapter.
	var in saveSectionsInput
	err = json.Unmarshal([]byte(`{"version":1,"sections":[]}, {"id":"two"}`), &in)
	if err == nil || candidateIssue(err)["code"] != "json_syntax" {
		t.Fatal("incident JSON defect was not diagnosed")
	}
}
func TestDraftAssemblyRequiresEveryPlannedChapter(t *testing.T) {
	s := &session{snapshot: domain.Snapshot{ID: "source"}}
	d := articleDraft{Title: "文章", Plan: []sectionPlan{{ID: "one"}, {ID: "two"}}, Sections: map[string]domain.Section{"one": {ID: "one"}}}
	if _, err := s.assembleArticle(d); err == nil {
		t.Fatal("partial video draft was publishable")
	}
	d.Sections["two"] = domain.Section{ID: "two"}
	a, err := s.assembleArticle(d)
	if err != nil || len(a.Sections) != 2 || a.MindMap == nil {
		t.Fatalf("assembly: %+v %v", a, err)
	}
	if len(a.Limitations) != 0 || a.Quality != "standard" {
		t.Fatal("assembly changed quality")
	}
}
func TestCandidateSyntaxErrorKeepsOffsetAndCategory(t *testing.T) {
	var in saveSectionsInput
	err := json.Unmarshal([]byte(`{"version":1,"sections":[]},"sections":[]}`), &in)
	issue := candidateIssue(err)
	if issue["code"] != "json_syntax" || issue["byte_offset"] == nil {
		t.Fatal(issue)
	}
	s := &session{cp: checkpoint{FinalizationFailure: &domain.Problem{Code: "MODEL_RESULT_INVALID", Detail: "JSON语法错误"}}}
	final := s.finalizationError()
	if !strings.Contains(final.Error(), "MODEL_RESULT_INVALID") || !strings.Contains(final.Error(), "JSON语法错误") {
		t.Fatal(final)
	}
}
