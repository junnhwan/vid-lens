package investigator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func TestWorkflowCommonContractDoesNotAssignAuthorFieldsToOtherStages(t *testing.T) {
	for _, authorOnly := range []string{"视频内容作者", "写成可独立阅读", "limitations"} {
		if strings.Contains(workflowInstructions, authorOnly) {
			t.Errorf("shared contract assigns stage-specific responsibility %q", authorOnly)
		}
	}
}

func TestWorkflowNestedUncertaintyHasActionablePathWithoutSilentCoercion(t *testing.T) {
	for _, raw := range []string{
		`{"sections":[{"id":"s1","blocks":[],"limitations":[]}]}`,
		`{"sections":[{"id":"s1","blocks":"[]"}],"limitations":[]}`,
		`{"sections":[],"limitations":"[]"}`,
	} {
		var body workflowBody
		err := workflowDecode(raw, &body)
		if err == nil || !strings.Contains(err.Error(), "path /") {
			t.Fatal("undeclared/mistyped uncertainty was silently accepted or lacks a pointer", raw, err)
		}
	}
}

func TestWorkflowBodyRejectsUncertaintyOutsideDeliveredEvidenceAndAssignedScope(t *testing.T) {
	plan := []sectionPlan{{ID: "s1", Title: "章节"}}
	draft := articleDraft{Sections: map[string]domain.Section{}}
	catalog := map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text"}}
	for _, limit := range []domain.Limitation{
		{Code: "partial_text", Message: "不能影响未指派章节。", SectionIDs: []string{"s2"}},
		{Code: "partial_text", Message: "写作必须标明所属章节。"},
		{Code: "visual_unclear", Message: "本批没有交付这张图片。", SectionIDs: []string{"s1"}, CitationIDs: []string{"f9"}},
	} {
		body := workflowBody{Sections: []workflowSection{{ID: "s1", Blocks: []workflowBlock{{Type: "paragraph", Origin: "video", Text: "来源事实。", CitationIDs: []string{"t1"}}}}}, Limitations: []domain.Limitation{limit}}
		before := store.Hash(draft)
		if _, err := workflowApplyBody(draft, body, plan, catalog, false, false); err == nil || store.Hash(draft) != before {
			t.Fatal("invalid uncertainty mutated the draft or was silently retained", limit, err)
		}
	}
}

func TestWorkflowUncertaintyMergePreservesUntouchedScopeAndEvidence(t *testing.T) {
	shared := domain.Limitation{Code: "partial_text", Message: "两个章节共享的来源专名含糊。", SectionIDs: []string{"s1", "s2"}, CitationIDs: []string{"t2", "t1"}}
	global := domain.Limitation{Code: "not_observed", Message: "原始连续过程不可见。"}
	updated := domain.Limitation{Code: "visual_unclear", Message: "首章局部文字不可辨认。", SectionIDs: []string{"s1"}, CitationIDs: []string{"f1"}}
	merged := workflowMergeLimitations([]domain.Limitation{shared, global}, []domain.Limitation{updated, updated}, map[string]bool{"s1": true}, true, false)
	if len(merged) != 3 || len(merged[0].SectionIDs) != 1 || merged[0].SectionIDs[0] != "s2" || len(merged[1].SectionIDs) != 0 || merged[2].Code != updated.Code {
		t.Fatal("revision erased unrelated/global uncertainty or duplicated a replay", merged)
	}
	if len(shared.SectionIDs) != 2 || shared.CitationIDs[0] != "t2" {
		t.Fatal("merge mutated the preserved input", shared)
	}
}

func TestWorkflowReviewUncertaintyInvalidatesOnlyItsScopeAndGlobal(t *testing.T) {
	s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV11}}, snapshot: domain.Snapshot{ID: "source", TextHash: "immutable"}}
	a := domain.Article{Title: "标题", Sections: []domain.Section{{ID: "s1", Title: "首章"}, {ID: "s2", Title: "末章"}}, Limitations: []domain.Limitation{{Code: "partial_text", Message: "首章旧局限。", SectionIDs: []string{"s1"}}}}
	units := workflowArticleUnits(a, nil)
	keys := func() []string {
		t.Helper()
		out := []string{}
		for _, unit := range append(units, workflowGlobalUnit(a)) {
			key, err := s.workflowReviewKey(context.Background(), a, unit, workflowGlobalOutline(a), nil)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, key)
		}
		return out
	}
	before := keys()
	a.Limitations[0].Message = "首章新的具体局限。"
	after := keys()
	if before[0] == after[0] || before[1] != after[1] || before[2] == after[2] {
		t.Fatal("scoped uncertainty either kept stale approval or invalidated another chapter", before, after)
	}
}

func TestWorkflowWriterDeclaresAndRetainsChapterUncertainty(t *testing.T) {
	// A synthetic shape of the cross-machine failure, not the original response.
	raw := `{"sections":[{"id":"s1","blocks":[{"type":"paragraph","origin":"video","text":"专名在原文中含糊，不能据此确定仓库名称。","citation_ids":["t1"]}]}],"limitations":[{"code":"partial_text","message":"原文未能确认专名。","section_ids":["s1"],"citation_ids":["t1"]}]}`
	var body workflowBody
	if err := workflowDecode(raw, &body); err != nil {
		t.Fatal("writer cannot return a declared uncertainty without another model call", err)
	}
	var stored struct {
		Limitations []domain.Limitation `json:"limitations"`
	}
	if err := json.Unmarshal(domain.JSON(body), &stored); err != nil || len(stored.Limitations) != 1 || stored.Limitations[0].SectionIDs[0] != "s1" {
		t.Fatal("accepted writer uncertainty was silently lost", stored, err)
	}
}

func TestWorkflowFormatFailureIdentifiesArrayPathAndTypes(t *testing.T) {
	var body workflowBody
	err := workflowDecode(`{"sections":"[]"}`, &body)
	if err == nil || !strings.Contains(err.Error(), "/sections") || !strings.Contains(err.Error(), "array") || !strings.Contains(err.Error(), "string") {
		t.Fatal("format recovery lacks the failing path, expected array, and actual string", err)
	}
}

func TestWorkflowPlanRejectsUnboundOrInvalidLimitationsBeforeSaving(t *testing.T) {
	for _, limitation := range []domain.Limitation{
		{Code: "invented", Message: "不能接受未知局限类型。"},
		{Code: "partial_text", Message: "规划尚未分配章节身份。", SectionIDs: []string{"s1"}},
		{Code: "partial_text", Message: "引用不在本次交付原文中。", CitationIDs: []string{"invented"}},
	} {
		p := workflowPlan{Title: "主题", Chapters: []workflowChapter{{Title: "首章", StartMS: 0}}, FrameNotes: []workflowFrameNote{}, Limitations: []domain.Limitation{limitation}}
		if err := validateWorkflowPlan(&p, 1000, nil, nil); err == nil {
			t.Errorf("invalid planning uncertainty was accepted: %+v", limitation)
		}
	}
}
