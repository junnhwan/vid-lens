package investigator

import (
	"context"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestWorkflowPassagesKeepEveryOriginalCueAndStableAliases(t *testing.T) {
	transcript := []domain.Segment{
		{ID: "cue-a", StartMS: 0, EndMS: 500, Origin: "platform", Text: "Original first cue."},
		{ID: "cue-b", StartMS: 500, EndMS: 900, Origin: "platform", Text: "Exact second cue."},
		{ID: "coarse-asr", StartMS: 900, EndMS: 60000, Origin: "asr", Text: "Uncertain whole audio window."},
	}
	passages := workflowPassages(transcript, 0, 60000)
	if len(passages) != 2 || passages[0].StartMS != 0 || passages[0].EndMS != 900 || passages[1].StartMS != 900 || passages[1].EndMS != 60000 {
		t.Fatal("transport merging changed original timing or provenance", passages)
	}
	for i, cue := range transcript {
		if strings.Count(passages[0].Text+passages[1].Text, cue.Text) != 1 || !strings.Contains(passages[i/2].Text, cue.Text) {
			t.Fatal("original cue lost, duplicated or rewritten", cue)
		}
	}
	selected := workflowPassages(transcript, 900, 60000)
	if len(selected) != 1 || !strings.HasPrefix(selected[0].Text, "[t3]") {
		t.Fatal("filtering renumbered an original source", selected)
	}
}

func TestWorkflowCanonicalWriterRequiresEachItemEvidence(t *testing.T) {
	catalog := map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text"}}
	chapter := sectionPlan{ID: "s1", Title: "主题"}
	input := workflowSection{ID: "s1", Blocks: []workflowBlock{{Type: "list", Origin: "video", CitationIDs: []string{"t1"}, Items: []workflowItem{{Text: "有原文的条目", CitationIDs: []string{"t1"}}, {Text: "不能借用整块的引用"}}}}}
	if _, err := workflowCanonicalSection(input, chapter, catalog); err == nil {
		t.Fatal("block-level evidence silently supported an uncited list item")
	}
	input.Blocks[0].Items[1].CitationIDs = []string{"invented"}
	if _, err := workflowCanonicalSection(input, chapter, catalog); err == nil {
		t.Fatal("invented source alias was admitted")
	}
	input.Blocks[0].Items[1].CitationIDs = []string{"t1"}
	section, err := workflowCanonicalSection(input, chapter, catalog)
	if err != nil || len(section.Blocks[0].Items) != 2 || section.Blocks[0].Items[1].Content[1].CitationID != "t1" {
		t.Fatal("item evidence was not preserved", section, err)
	}
}

func TestWorkflowReviewIdentityInvalidatesDependenciesOnly(t *testing.T) {
	s := &session{snapshot: domain.Snapshot{ID: "source", TextHash: "immutable"}, spec: domain.GenerationSpec{Model: domain.GenerationModel{RevisionID: "model-revision"}}}
	a := domain.Article{Title: "标题", Sections: []domain.Section{
		{ID: "s1", Title: "首段", Blocks: []domain.Block{{ID: "s1_b1", Type: "code", Text: "one"}}},
		{ID: "s2", Title: "末段", Blocks: []domain.Block{{ID: "s2_b1", Type: "code", Text: "two"}}},
	}}
	catalog := map[string]domain.Citation{}
	units := workflowArticleUnits(a, catalog)
	unit := units[len(units)-1]
	key := func() string {
		k, err := s.workflowReviewKey(context.Background(), a, unit, workflowGlobalOutline(a), catalog)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	before := key()
	a.Sections[0].Blocks[0].Text = "local revision in another section"
	if key() != before {
		t.Fatal("unrelated chapter body invalidated accepted coverage")
	}
	a.Sections[1].Blocks[0].Text = "changed same-section context"
	if key() == before {
		t.Fatal("same-section context was ignored")
	}
	before = key()
	s.snapshot.TextHash = "changed-original"
	if key() == before {
		t.Fatal("changed evidence retained approval")
	}
	before = key()
	s.spec.Model.RevisionID = "new-model-revision"
	if key() == before {
		t.Fatal("changed model retained approval")
	}
}

func TestWorkflowIndependentCoverageNeverInfersMissingOrDuplicateVerdicts(t *testing.T) {
	task := reviewTask{Units: []reviewUnit{{ID: "one"}, {ID: "two"}}}
	for _, results := range [][]reviewUnitResult{
		{{UnitID: "one", Verdict: "supported", Issues: []reviewUnitIssue{}}},
		{{UnitID: "one", Verdict: "supported", Issues: []reviewUnitIssue{}}, {UnitID: "two", Verdict: "supported", Issues: []reviewUnitIssue{}}, {UnitID: "two", Verdict: "not_assessed", Issues: []reviewUnitIssue{}}},
	} {
		accepted, err := admitIndependentReview(reviewUnitReport{RequestID: "request", Results: results}, "request", task, nil)
		if err == nil || len(accepted.Results) != 1 || accepted.Results[0].UnitID != "one" {
			t.Fatal("missing/conflicting evidence became approval", accepted, err)
		}
	}
}
