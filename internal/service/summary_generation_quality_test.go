package service

import (
	"testing"
	"vid-lens/internal/artifact"
	"vid-lens/internal/textsource"
)

func TestSummaryGenerationPercentagesRequireSelectedExactEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name, source, body string
		bad                bool
	}{
		{"supported", "例子准确率90%以上", "举例准确率90%。", false},
		{"full_width", "例子准确率90％以上", "举例准确率90%。", false},
		{"invented_comparison", "本例90%以上，下周低于90%", "从90%降到60%。", true},
		{"substring_is_not_evidence", "例子160%", "例子60%。", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, e, envelope := minimalWireFixture(t)
			copySource := *e.source
			first := copySource.Cues[0]
			first.Text = scenario.source
			second := copySource.Cues[1]
			second.Text = "另一例60%与不同条件"
			copySource.Cues = []textsource.Cue{first, second}
			e.source = &copySource
			envelope["document"].(map[string]any)["blocks"] = []any{map[string]any{"id": "detail", "parent_id": nil, "order": 0, "title": "示例", "body_markdown": scenario.body, "cue_ids": []string{first.ID}}}
			result := e.validateResponse(artifact.JSON(envelope))
			if result.Invalid != scenario.bad {
				t.Fatalf("unexpected acceptance: %+v", result)
			}
			if scenario.bad && result.ValidationCode != "unsupported_percentage" {
				t.Fatalf("wrong diagnostic: %+v", result)
			}
		})
	}
}

func TestSummaryGenerationNamedSectionsMustAppearInSharedHierarchy(t *testing.T) {
	_, e, envelope := minimalWireFixture(t)
	e.snapshot.Intent.Options.MindmapEnabled = true
	block := map[string]any{"id": "topic", "parent_id": nil, "order": 0, "title": "主题", "body_markdown": "**机制**：配置与执行分别维护。\n\n**案例**：信息不足需要追问。", "cue_ids": []string{e.source.Cues[0].ID}}
	envelope["document"].(map[string]any)["blocks"] = []any{block}
	result := e.validateResponse(artifact.JSON(envelope))
	if !result.Invalid || result.ValidationCode != "content_hierarchy_missing" {
		t.Fatalf("hidden hierarchy accepted: %+v", result)
	}
	block["body_markdown"] = ""
	block["cue_ids"] = []string{}
	envelope["document"].(map[string]any)["blocks"] = []any{block, map[string]any{"id": "mechanism", "parent_id": "topic", "order": 0, "title": "机制", "body_markdown": "配置与执行分别维护。", "cue_ids": []string{e.source.Cues[0].ID}}, map[string]any{"id": "case", "parent_id": "topic", "order": 1, "title": "案例", "body_markdown": "信息不足需要追问。", "cue_ids": []string{e.source.Cues[1].ID}}}
	result = e.validateResponse(artifact.JSON(envelope))
	if result.Invalid || len(result.Document.Blocks) != 3 {
		t.Fatalf("real hierarchy rejected: %+v", result)
	}
}

func TestSummaryGenerationPercentageInBlockTitleCannotBypassEvidence(t *testing.T) {
	_, e, envelope := minimalWireFixture(t)
	envelope["document"].(map[string]any)["blocks"].([]any)[1].(map[string]any)["title"] = "效果提高60%"
	result := e.validateResponse(artifact.JSON(envelope))
	if !result.Invalid || result.ValidationCode != "unsupported_percentage" || result.ValidationPath != "document.blocks[1].title" {
		t.Fatalf("unsupported title accepted: %+v", result)
	}
}

func TestSummaryGenerationNamedHeadingFormsHaveSameHierarchyPolicy(t *testing.T) {
	for _, body := range []string{"**机制：**配置与执行分开。\n**案例：**说明条件。", "**机制**\n配置与执行分开。\n**案例**\n说明条件。", "## 机制\n配置与执行分开。\n## 案例\n说明条件。"} {
		_, e, envelope := minimalWireFixture(t)
		e.snapshot.Intent.Options.MindmapEnabled = true
		envelope["document"].(map[string]any)["blocks"] = []any{map[string]any{"id": "topic", "parent_id": nil, "order": 0, "title": "主题", "body_markdown": body, "cue_ids": []string{e.source.Cues[0].ID}}}
		result := e.validateResponse(artifact.JSON(envelope))
		if !result.Invalid || result.ValidationCode != "content_hierarchy_missing" {
			t.Fatalf("heading variant bypassed hierarchy: %+v", result)
		}
	}
}
