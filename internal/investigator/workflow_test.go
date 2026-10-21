package investigator

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
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

func TestWorkflowPlanAcceptsAValidWideInvestigationForServerSampling(t *testing.T) {
	// The first real MiniMax Theo plan was rejected solely because this
	// 93.44-second question range was longer than one FFmpeg window.
	plan := workflowPlan{Title: "完整主题结构", Chapters: []workflowChapter{{Title: "主题", StartMS: 0, KeyPoints: []string{"具体机制"}}}, FrameNotes: []workflowFrameNote{}, Investigations: []workflowInvestigation{{Question: "核对文章中的实际数字与旁白是否一致", StartMS: 693920, EndMS: 787360}}}
	if err := validateWorkflowPlan(&plan, 3772902, nil); err != nil {
		t.Fatal("model was asked to coordinate an internal sampling ceiling", err)
	}
	windows := workflowInspectionWindows(plan.Investigations[0])
	if len(windows) != 3 {
		t.Fatal("wide evidence question lost bounded sampling", windows)
	}
	last := int64(-1)
	count := 0
	for _, window := range windows {
		if window.EndMS-window.StartMS > 60000 || window.StartMS <= last || window.StartMS < plan.Investigations[0].StartMS || window.EndMS > plan.Investigations[0].EndMS {
			t.Fatal("sampling violated original range, chronology or media ceiling", windows)
		}
		last = window.StartMS
		count += window.Count
	}
	if count != 3 {
		t.Fatal("wide range expanded the evidence cost", count)
	}
}

func TestWorkflowOutlineContractDoesNotRequestAnotherArticle(t *testing.T) {
	tool, err := workflowPlanTool(workflowPlanInstructions)
	if err != nil {
		t.Fatal(err)
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || strings.Contains(string(domain.JSON(sc)), "key_points") {
		t.Fatal("outline requested duplicated body facts instead of structural metadata", err)
	}
}

func TestWorkflowFrameNoteSharesDeliveredAliasAndKeepsStrictLegacyDecode(t *testing.T) {
	for _, key := range []string{"alias", "frame_alias"} {
		var note workflowFrameNote
		if err := workflowDecode(`{"`+key+`":"f1","visible_facts":["实际可见内容"],"uncertain":[]}`, &note); err != nil || note.FrameAlias != "f1" {
			t.Fatal("safe evidence identity required a new model call", key, err)
		}
	}
	for _, raw := range []string{
		`{"alias":"f1","frame_alias":"f2","visible_facts":["内容"],"uncertain":[]}`,
		`{"alias":"f1","invented":"f2","visible_facts":["内容"],"uncertain":[]}`,
	} {
		if workflowDecode(raw, new(workflowFrameNote)) == nil {
			t.Fatal("conflicting or unknown evidence field accepted", raw)
		}
	}
	encoded := string(domain.JSON(workflowFrameNote{FrameAlias: "f1"}))
	if !strings.Contains(encoded, `"alias":"f1"`) || strings.Contains(encoded, "frame_alias") {
		t.Fatal("new output still disagrees with delivered pixel identity", encoded)
	}
}

func TestWorkflowStageAcceptsOneCompleteTypedJSONInEitherEnvelope(t *testing.T) {
	const name, body = "record_value", `{"value":"已核对"}`
	toolResponse := func(n string, args string) *schema.Message {
		return &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: n, Arguments: args}}}}
	}
	for _, response := range []*schema.Message{{Content: body}, toolResponse(name, body)} {
		raw, err := workflowArguments(response, name)
		var value struct {
			Value string `json:"value"`
		}
		if err != nil || workflowDecode(raw, &value) != nil || value.Value != "已核对" {
			t.Fatal("complete typed stage required another paid envelope repair", err, raw)
		}
	}
	mixed := toolResponse(name, body)
	mixed.Content = body
	multiple := toolResponse(name, body)
	multiple.ToolCalls = append(multiple.ToolCalls, multiple.ToolCalls[0])
	for _, response := range []*schema.Message{nil, mixed, multiple, toolResponse("other", body), {Content: "说明：" + body}, {Content: body + body}, {Content: `{"value":`}, {Content: `{"value":"ok","invented":true}`}, {Content: "null"}} {
		raw, err := workflowArguments(response, name)
		if err == nil {
			err = workflowDecode(raw, new(struct {
				Value string `json:"value"`
			}))
		}
		if err == nil {
			t.Fatal("ambiguous, partial or undeclared output became a stage result", response)
		}
	}
}
