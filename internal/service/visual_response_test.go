package service

import (
	"reflect"
	"testing"
)

func TestQueryVisualResponseExtractsActualFencedResponseShape(t *testing.T) {
	// Same wire structure as the two real VLM responses: four visible facts
	// and three missing-evidence statements, with private content replaced.
	fixture := `{
  "facts": ["画面为深色标题页", "中央有主题标题", "底部有工具名称", "角落有来源标记"],
  "gaps": ["没有实际操作界面", "没有代码产出对比", "无法从本帧判断效果"]
}`
	wantFacts := []string{"画面为深色标题页", "中央有主题标题", "底部有工具名称", "角落有来源标记"}
	wantGaps := []string{"没有实际操作界面", "没有代码产出对比", "无法从本帧判断效果"}
	for name, raw := range map[string]string{
		"json":             fixture,
		"json_fence":       "```json\n" + fixture + "\n```",
		"unlabelled_fence": "```\n" + fixture + "\n```",
		"crlf":             "```json\r\n" + fixture + "\r\n```",
	} {
		t.Run(name, func(t *testing.T) {
			facts, gaps := parseQueryVisualResponse(raw)
			if !reflect.DeepEqual(facts, wantFacts) || !reflect.DeepEqual(gaps, wantGaps) {
				t.Fatalf("facts=%v gaps=%v", facts, gaps)
			}
		})
	}
}

func TestQueryVisualResponseRejectsUnknownOrMalformedStructuredEvidence(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown":            `{"facts":["可见事实"],"confidence":1}`,
		"unknown_only":       `{"description":"未经声明的格式"}`,
		"duplicate":          `{"facts":["甲"],"facts":["乙"],"gaps":[]}`,
		"scalar_fact":        `{"facts":"可见事实","gaps":[]}`,
		"non_string_fact":    `{"facts":[{"text":"事实"}],"gaps":[]}`,
		"null":               `{"facts":null,"gaps":[]}`,
		"null_item":          `{"facts":[null],"gaps":[]}`,
		"empty_object":       `{}`,
		"array":              `["事实"]`,
		"scalar":             `null`,
		"trailing_object":    `{"facts":["事实"]} {"gaps":[]}`,
		"trailing_text":      `{"facts":["事实"]} 额外文字`,
		"incomplete":         `{"facts":["事实"]`,
		"unclosed_fence":     "```json\n{\"facts\":[\"事实\"]}",
		"wrong_fence":        "```text\n{\"facts\":[\"事实\"]}\n```",
		"fence_then_text":    "```json\n{\"facts\":[\"事实\"]}\n```\n额外文字",
		"text_in_json_fence": "```json\n未经解析的普通文字\n```",
	} {
		t.Run(name, func(t *testing.T) {
			facts, gaps := parseQueryVisualResponse(raw)
			if len(facts) != 0 || !reflect.DeepEqual(gaps, []string{queryVisualResponseFormatGap}) {
				t.Fatalf("unknown format promoted to evidence: facts=%v gaps=%v", facts, gaps)
			}
		})
	}
}

func TestQueryVisualResponsePreservesPlainTextAndKnownPartialObjects(t *testing.T) {
	facts, gaps := parseQueryVisualResponse("  画面有一个标题。\n没有展示操作过程。  ")
	if !reflect.DeepEqual(facts, []string{"画面有一个标题。\n没有展示操作过程。"}) || len(gaps) != 0 {
		t.Fatalf("plain text compatibility: facts=%v gaps=%v", facts, gaps)
	}
	facts, gaps = parseQueryVisualResponse(`{"facts":["  标题  ","标题",""]}`)
	if !reflect.DeepEqual(facts, []string{"标题"}) || len(gaps) != 0 {
		t.Fatalf("known partial object: facts=%v gaps=%v", facts, gaps)
	}
	facts, gaps = parseQueryVisualResponse(`{"gaps":["未出现操作过程"]}`)
	if len(facts) != 0 || !reflect.DeepEqual(gaps, []string{"未出现操作过程"}) {
		t.Fatalf("gaps-only response: facts=%v gaps=%v", facts, gaps)
	}
	facts, gaps = parseQueryVisualResponse(" \n ")
	if len(facts)+len(gaps) != 0 {
		t.Fatal("empty response became evidence")
	}
}
