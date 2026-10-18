package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestAuthoringPromptHasOnePublicationAndBatchContract(t *testing.T) {
	prompt := authoringInstructions("标准：保留主要事实和解释。")
	for _, stale := range []string{"1–3章", "最多3章", "submit_article_draft", "submit_answer", "只能表达文章章节结构", "禁止输出整篇巨大"} {
		if strings.Contains(prompt, stale) {
			t.Fatal("conflicting old instruction", stale)
		}
	}
	if !strings.Contains(prompt, "不能为了少调用删除") || !strings.Contains(prompt, "年份") || len([]rune(prompt)) >= 4000 {
		t.Fatal("quality contract missing or bloated")
	}
	if !strings.Contains(prompt, "end_ms-start_ms<=60000") {
		t.Fatal("inspection window bound missing")
	}
	if !strings.Contains(prompt, "不是句子的精确时间") || !strings.Contains(prompt, "字幕时间与真实视频帧时间") {
		t.Fatal("coarse ASR timing treated as sentence timing")
	}
}

func TestAuthoringLimitationSchemaAndFeedbackListAllDefects(t *testing.T) {
	p, _ := domain.InterpretationPolicy("standard")
	s := &session{spec: domain.GenerationSpec{DetailPolicy: &p}, cp: checkpoint{HarnessRevision: 12, LayoutVersion: draftLayout}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	info, err := s.tools["begin_article"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(domain.JSON(sc), &root); err != nil {
		t.Fatal(err)
	}
	code := root["properties"].(map[string]any)["limitations"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["code"].(map[string]any)
	if string(domain.JSON(code["enum"])) != string(domain.JSON(domain.LimitationCodes())) {
		t.Fatal(code)
	}
	err = authoringLimitationIssues([]domain.Limitation{
		{Code: "asr_proper_nouns", SectionIDs: []string{"unknown"}, CitationIDs: []string{"f999"}},
		{Code: "date_year_unknown"},
	}, map[string]bool{"one": true}, map[string]domain.Citation{"t1": {ID: "t1"}})
	problem, ok := err.(*domain.Problem)
	if !ok || problem.Meta["issue_count"] != 4 {
		t.Fatal(err)
	}
	feedback := string(domain.JSON(problem.Meta))
	for _, path := range []string{"/limitations/0/code", "/limitations/1/code", "/limitations/0/section_ids/0", "/limitations/0/citation_ids/0"} {
		if !strings.Contains(feedback, path) {
			t.Fatal(feedback)
		}
	}
	if err := authoringLimitationIssues([]domain.Limitation{{Code: "partial_text", SectionIDs: []string{"one"}, CitationIDs: []string{"t1"}}}, map[string]bool{"one": true}, map[string]domain.Citation{"t1": {ID: "t1"}}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthoringThirteenChaptersFitOneBodyCall(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 12
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := conceptPlanInput{Title: "完整主题", MindMap: &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{{ID: "root", Label: "完整主题"}, {ID: "group", Label: "机制与依据", ParentID: domain.Ptr("root")}}}, Limitations: []domain.Limitation{}}
	sections := []domain.Section{}
	for i := 0; i < 13; i++ {
		id := fmt.Sprintf("topic%d", i)
		plan.Sections = append(plan.Sections, sectionPlan{ID: id, Title: id, KeyPoints: []string{id}, EvidenceIDs: []string{"t1"}})
		plan.MindMap.Nodes = append(plan.MindMap.Nodes, domain.MapNode{ID: id, Label: id, ParentID: domain.Ptr("group"), SectionID: domain.Ptr(id)})
		sections = append(sections, domain.Section{ID: id, Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "这个主题在来源中有对应事实。"}, {Type: "citation", CitationID: "t1"}}}}})
	}
	if body, err := planCall(s, "thirteen-plan", "begin_article", plan); err != nil || body["version"] != float64(1) {
		t.Fatal(body, err)
	}
	if body, err := planCall(s, "thirteen-body", "save_article_sections", saveSectionsInput{Version: 1, Sections: sections}); err != nil || body["saved_section_count"] != float64(13) {
		t.Fatal(body, err)
	}
}

func TestAuthoringRangesUseRealEvidenceRatherThanRoundedBoundaries(t *testing.T) {
	catalog := map[string]domain.Citation{"t1": {Range: &domain.Range{StartMS: 0, EndMS: 420000}}, "t8": {Range: &domain.Range{StartMS: 420000, EndMS: 480000}}, "f5": {TimeMS: domain.Ptr(int64(416767))}}
	input := []sectionPlan{{ID: "intro", Title: "前文", EvidenceIDs: []string{"t1"}}, {ID: "security", Title: "安全审查", EvidenceIDs: []string{"t8", "f5"}}}
	plan := deriveAuthoringRanges(input, catalog, 480000)
	if plan[1].StartMS != 416767 || input[1].StartMS != 0 || validateSectionPlan(plan, 480000, true) != nil {
		t.Fatal(plan)
	}
}

func TestAuthoringHistoryRetainsLatestIssueWithoutObsoletePlans(t *testing.T) {
	call := func(id string) *schema.Message {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: id, Function: schema.FunctionCall{Name: "save_article_sections", Arguments: `{"old_body":"private original"}`}}}}
	}
	messages := []*schema.Message{schema.SystemMessage("frozen"), call("old"), {Role: schema.Tool, ToolCallID: "old", Content: `{"version":1,"plan":["obsolete"],"error":{"code":"old_error"}}`}, call("accepted"), {Role: schema.Tool, ToolCallID: "accepted", Content: `{"version":2,"plan":["duplicate"]}`}, call("latest"), {Role: schema.Tool, ToolCallID: "latest", Content: `{"version":2,"plan":["duplicate"],"error":{"code":"missing_caption"}}`}}
	out := projectAuthoringHistory(messages)
	raw := string(domain.JSON(out))
	if len(out) != 2 || strings.Contains(raw, "obsolete") || strings.Contains(raw, "old_error") || strings.Contains(raw, "duplicate") || !strings.Contains(raw, "missing_caption") || strings.Contains(raw, "private original") {
		t.Fatal(raw)
	}
	if err := validateProtocol(out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(domain.JSON(messages)), "private original") {
		t.Fatal("original trace mutated")
	}
}

func TestAuthoringStrictTransportIsExplicitAndDoesNotMutateCaller(t *testing.T) {
	for _, raw := range []string{"https://gateway.test/beta", "https://api.deepseek.com/v1", "http://api.deepseek.com/beta"} {
		if nativeStrictEndpoint(raw) {
			t.Fatal("guessed strict capability", raw)
		}
	}
	if !nativeStrictEndpoint("https://api.deepseek.com/beta") {
		t.Fatal("explicit native Beta endpoint unsupported")
	}
	req, _ := http.NewRequest("POST", "https://api.deepseek.com/beta/chat/completions", strings.NewReader(`{"messages":[],"tools":[{"type":"function","function":{"name":"write","parameters":{"type":"object","properties":{"caption":{"type":"string","minLength":1},"frames":{"type":"array","minItems":1,"maxItems":6,"items":{"type":"string"}}},"additionalProperties":false}}}]}`))
	modified := strictToolRequest(req)
	if modified == req {
		t.Fatal("strict not applied")
	}
	var body map[string]any
	_ = json.NewDecoder(modified.Body).Decode(&body)
	function := body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if function["strict"] != true {
		t.Fatal(body)
	}
	if strings.Contains(string(domain.JSON(function)), "minItems") || strings.Contains(string(domain.JSON(function)), "minLength") {
		t.Fatal("native strict wire contains unsupported bounds")
	}
	original, _ := req.GetBody()
	var before map[string]any
	_ = json.NewDecoder(original).Decode(&before)
	original.Close()
	if strings.Contains(string(domain.JSON(before)), `"strict"`) {
		t.Fatal("caller/frozen request mutated")
	}
	for _, raw := range []string{`{"type":"object","additionalProperties":{"type":"string"}}`, `{}`} {
		if closedStrictSchema(canonicalArguments(raw)) {
			t.Fatal("open schema advertised as strict", raw)
		}
	}
}

func TestAuthoringRetainedBodyFieldRepairSurvivesReplay(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 12
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	m := &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{{ID: "root", Label: "主题"}, {ID: "group", Label: "条件与结果", ParentID: domain.Ptr("root")}, {ID: "a", Label: "起始条件", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("one")}, {ID: "b", Label: "结束状态", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("two")}}}
	plan := conceptPlanInput{Title: "来源事实", Sections: []sectionPlan{{ID: "one", Title: "起始条件", KeyPoints: []string{"第一个来源事实"}, EvidenceIDs: []string{"t1"}}, {ID: "two", Title: "结束状态", KeyPoints: []string{"结尾的独立事实"}, EvidenceIDs: []string{"t25"}}}, MindMap: m, Limitations: []domain.Limitation{}}
	if body, err := planCall(s, "outline", "begin_article", plan); err != nil || body["version"] != float64(1) {
		t.Fatal(body, err)
	}
	bad := `{"version":1,"sections":[{"id":"one","blocks":[{"type":"paragraph","origin":"video","text":"起始条件有来源提供的事实，需要在后续过程中保留。","citation_ids":["t1"]}]},{"id":"two","blocks":[{"type":"list","origin":"video","ordered":true,"content":[]}]}]}`
	if body, err := planCall(s, "bad-body", "save_article_sections", bad); err != nil || body["error"] == nil {
		t.Fatal(body, err)
	}
	candidate := s.cp.LastCandidate
	block := map[string]any{"type": "list", "origin": "video", "ordered": true, "items": []any{map[string]any{"text": "结束状态同样有独立的来源事实，不能省略。", "citation_ids": []string{"t25"}}}}
	input := authoringRepairInput{CandidateID: candidate["candidate_id"].(string), ExpectedHash: candidate["expected_hash"].(string), Version: 1, Patches: []authoringFieldPatch{{Op: "replace", Path: "/sections/1/blocks/0", Value: domain.JSON(block)}}}
	for i := 0; i < 2; i++ {
		body, err := planCall(s, "field-fix", "repair_article_sections", input)
		if err != nil || body["version"] != float64(2) || body["saved_section_count"] != float64(2) {
			t.Fatal(body, err)
		}
	}
	version, draft, err := s.loadDraft(context.Background())
	if err != nil || version != 2 || !strings.Contains(domain.BlockText(draft.Sections["one"].Blocks[0]), "起始条件有来源") || !strings.Contains(domain.BlockText(draft.Sections["two"].Blocks[0]), "结束状态同样有独立") {
		t.Fatal(version, err)
	}
	for _, path := range []string{"/version", "/sections/0/id", "/sections/0/blocks/0/snapshot_id"} {
		if _, err := patchAuthoringBody(bad, []authoringFieldPatch{{Op: "replace", Path: path, Value: domain.JSON("forged")}}); err == nil {
			t.Fatal("protected identity writable", path)
		}
	}
	s.transcript[0].Text = "10月15日，介绍来源事实。"
	section := domain.Section{ID: "one", Blocks: []domain.Block{{ID: "one_b1", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "2025 年10月15日"}}}}}
	if err := s.authoringFacts(context.Background(), []domain.Section{section}); err == nil {
		t.Fatal("unsupported absolute year accepted")
	}
}

func TestAuthoringFigureContractRequiresCaption(t *testing.T) {
	p, _ := domain.InterpretationPolicy("standard")
	s := &session{spec: domain.GenerationSpec{DetailPolicy: &p}, cp: checkpoint{HarnessRevision: 12, LayoutVersion: draftLayout}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	info, err := s.tools["save_article_sections"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	raw := string(domain.JSON(sc))
	if !strings.Contains(raw, `"anyOf"`) || !strings.Contains(raw, `"required":["type","origin","frame_ids","caption"]`) {
		t.Fatal("figure caption is only a prose requirement; model can omit it", raw)
	}
}

func TestAuthoringUsageKeepsReasoningUnknownUntilReported(t *testing.T) {
	o := newUsageObserver("test", "provider_default", nil)
	o.parse([]byte(`{"usage":{"prompt_tokens":10,"completion_tokens":8192}}`))
	if o.report.Reasoning.Status != "unknown" || o.report.Reasoning.Value != nil {
		t.Fatal("missing reasoning became zero")
	}
	o.parse([]byte(`{"usage":{"completion_tokens_details":{"reasoning_tokens":7000}}}`))
	if o.report.Reasoning.Value == nil || *o.report.Reasoning.Value != 7000 {
		t.Fatal(o.report.Reasoning)
	}
}

func TestAuthoringRedundantClosingBraceDoesNotDiscardBody(t *testing.T) {
	calls := 0
	base, err := utils.InferEnhancedTool("save_article_sections", "test", func(_ context.Context, in saveSectionsInput) (*schema.ToolResult, error) {
		calls++
		if len(in.Sections) != 1 || in.Sections[0].Blocks[0].Content[0].Text != "原始正文" {
			t.Fatal(in)
		}
		return textResult(map[string]bool{"accepted": true}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checked := &argumentCheckedTool{EnhancedInvokableTool: base, name: "save_article_sections", session: &session{cp: checkpoint{HarnessRevision: 12}}}
	raw := `{"version":1,"sections":[{"id":"s1","blocks":[{"type":"paragraph","origin":"video","content":[{"type":"text","text":"原始正文"},{"type":"citation","citation_id":"t1"}]}]}]}}`
	if _, err = checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: raw}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("unambiguous extra brace discarded the complete body")
	}
	// A second value is never treated as harmless punctuation.
	if _, err = checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: strings.TrimSuffix(raw, "}") + `,{"version":2}`}); err == nil && calls != 1 {
		t.Fatal("ambiguous second object executed")
	}
}

func TestAuthoringShallowBodyPreservesWordsMarksAndReferences(t *testing.T) {
	called := false
	base, _ := utils.InferEnhancedTool("save_article_sections", "test", func(_ context.Context, in saveSectionsInput) (*schema.ToolResult, error) {
		called = true
		b := in.Sections[0].Blocks
		if b[0].Text != "" || b[0].Content[0].Text != "原始数字 59%、2.08→1.32 与条件。" || b[0].Content[0].Marks[0] != "bold" || b[0].Content[1].CitationID != "t12" || b[1].Items[0].Content[0].Text != "保留第一步和风险条件。" || b[1].Items[0].Content[1].CitationID != "t13" {
			t.Fatal(in)
		}
		return textResult(map[string]bool{"accepted": true}), nil
	})
	checked := &argumentCheckedTool{EnhancedInvokableTool: base, name: "save_article_sections", session: &session{cp: checkpoint{HarnessRevision: 12}}}
	raw := `{"version":1,"sections":[{"id":"one","blocks":[{"type":"paragraph","origin":"video","text":"原始数字 59%、2.08→1.32 与条件。","marks":["bold"],"citation_ids":["t12"]},{"type":"list","origin":"video","ordered":true,"items":[{"text":"保留第一步和风险条件。","citation_ids":["t13"]}]}]}]}`
	if _, err := checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: raw}); err != nil || !called {
		t.Fatal(err)
	}
	if err := authoringShapeIssues(strings.Replace(raw, `"text":"原始数字`, `"content":[],"text":"原始数字`, 1)); err == nil {
		t.Fatal("ambiguous authoring fields accepted")
	}
}

func TestAuthoringRawErrorsAreAggregated(t *testing.T) {
	// The production failure hid four missing captions behind one list error.
	var raw any
	if err := json.Unmarshal([]byte(`{"version":1,"sections":[{"id":"s8","blocks":[{"type":"list","origin":"video","content":[]},{"type":"figure","origin":"video","frame_ids":["f9"]}]},{"id":"s9","blocks":[{"type":"figure","origin":"video","frame_ids":["f11"]}]}]}`), &raw); err != nil {
		t.Fatal(err)
	}
	base, _ := utils.InferEnhancedTool("save_article_sections", "test", func(context.Context, saveSectionsInput) (*schema.ToolResult, error) {
		t.Fatal("invalid body executed")
		return nil, nil
	})
	checked := &argumentCheckedTool{EnhancedInvokableTool: base, name: "save_article_sections", session: &session{cp: checkpoint{HarnessRevision: 12}}}
	_, err := checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: string(domain.JSON(raw))})
	if err == nil || !strings.Contains(err.Error(), "caption") || !strings.Contains(err.Error(), "items") {
		t.Fatalf("independent defects were hidden: %v", err)
	}
}
