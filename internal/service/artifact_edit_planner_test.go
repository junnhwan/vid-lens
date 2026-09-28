package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
)

func fixtureArtifactEditBody() artifact.Body {
	root := "root"
	return artifact.Body{SchemaVersion: 1, Kind: "study", Title: "课程", Blocks: []artifact.Block{
		{BlockID: "root", Type: "section", Title: "课程", Content: "概览", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-root", Relation: "supports"}}},
		{BlockID: "install", ParentID: &root, Type: "concept", Title: "安装", Content: "命令", ClaimOrigin: "source", EvidenceRefs: []artifact.Ref{{EvidenceID: "ev-install", Relation: "supports"}}},
		{BlockID: "child", ParentID: stringPointer("install"), Type: "note", Title: "说明", Content: "保留", ClaimOrigin: "user", EvidenceRefs: []artifact.Ref{}},
	}, Warnings: []string{}}
}

type artifactEditFixtureChat struct {
	response string
	messages []ai.ChatMessage
}

func (c *artifactEditFixtureChat) Chat(_ context.Context, messages []ai.ChatMessage) (string, error) {
	c.messages = append([]ai.ChatMessage(nil), messages...)
	return c.response, nil
}

func TestParseArtifactEditPlannerDecisionIsStrict(t *testing.T) {
	t.Parallel()
	valid := `{"tool":"read_artifact","reason":"读取范围","public_summary":"正在读取","arguments":{"limit":20}}`
	for _, input := range []string{valid, "```json\n" + valid + "\n```", "```\n" + valid + "\n```", " \n```json\r\n" + valid + "\r\n```\n "} {
		decision, err := ParseArtifactEditPlannerDecision(input)
		if err != nil || decision.Tool != ArtifactEditToolRead {
			t.Fatalf("decision=%#v err=%v input=%s", decision, err, input)
		}
	}
	for _, invalid := range []string{
		"Here is the decision:\n```json\n" + valid + "\n```",
		"```json\n" + valid + "\n```\nExplanation",
		"```javascript\n" + valid + "\n```",
		"```json\n" + valid + "```",
		"```json\n" + valid + "\n",
		"```json\n" + valid + valid + "\n```",
		"```json\n" + valid + "\n```\n```json\n" + valid + "\n```",
		"```json\n" + `{"tool":"read_artifact","reason":"x","public_summary":"x","arguments":{},"extra":true}` + "\n```",
		`{"tool":"read_artifact","reason":"x","public_summary":"x","arguments":{},"extra":true}`,
		valid + valid,
		`{"tool":"read_artifact","reason":"x","public_summary":"x","arguments":null}`,
	} {
		if _, err := ParseArtifactEditPlannerDecision(invalid); err == nil {
			t.Fatalf("accepted non-strict planner JSON: %s", invalid)
		}
	}
}

func TestArtifactEditPlannerQuestionCanOnlyEndAsAnswer(t *testing.T) {
	t.Parallel()
	chat := &artifactEditFixtureChat{response: `{"tool":"propose_artifact_patch","reason":"改写","public_summary":"准备改写","arguments":{"summary":"改写","patch":{}}}`}
	planner := NewLLMArtifactEditPlanner(chat)
	registry, digest, err := NewArtifactEditToolRegistry(ArtifactEditModeApply)
	if err != nil {
		t.Fatal(err)
	}
	state := ArtifactEditPlannerState{Instruction: "这个名称对吗？", Mode: ArtifactEditModeApply, Intent: ArtifactEditIntentAnswer, ToolSchemaDigest: digest}
	decision, _, err := planner.NextDecisionWithUsage(context.Background(), state, registry.Definitions())
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateArtifactEditDecision(state, registry, decision); err == nil {
		t.Fatalf("question accepted write decision: %#v", decision)
	}
}

func TestArtifactEditPlannerFixtureRoutesQuestionToAnswerAndEditToProposal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		instruction string
		response    string
		wantIntent  ArtifactEditIntent
		wantTool    string
	}{
		{
			instruction: "这个名称对吗？",
			response:    `{"tool":"answer_artifact_question","reason":"这是询问","public_summary":"回答问题，不修改笔记","arguments":{"message":"需要核对","evidence_ids":[]}}`,
			wantIntent:  ArtifactEditIntentAnswer,
			wantTool:    ArtifactEditToolAnswerQuestion,
		},
		{
			instruction: "如何修改这个名称？",
			response:    `{"tool":"answer_artifact_question","reason":"询问修改方法","public_summary":"说明做法，不修改笔记","arguments":{"message":"可以给出明确的新名称后再修改。","evidence_ids":[]}}`,
			wantIntent:  ArtifactEditIntentAnswer,
			wantTool:    ArtifactEditToolAnswerQuestion,
		},
		{
			instruction: "请问怎么修改这一段？",
			response:    `{"tool":"answer_artifact_question","reason":"询问修改方法","public_summary":"说明做法，不修改笔记","arguments":{"message":"可以先说明希望改成什么。","evidence_ids":[]}}`,
			wantIntent:  ArtifactEditIntentAnswer,
			wantTool:    ArtifactEditToolAnswerQuestion,
		},
		{
			instruction: "请问修改这一段的最佳方式",
			response:    `{"tool":"answer_artifact_question","reason":"询问修改方法","public_summary":"说明做法，不修改笔记","arguments":{"message":"可以先明确目标和范围。","evidence_ids":[]}}`,
			wantIntent:  ArtifactEditIntentAnswer,
			wantTool:    ArtifactEditToolAnswerQuestion,
		},
		{
			instruction: "能否删除这一段？",
			response:    `{"tool":"answer_artifact_question","reason":"询问能力","public_summary":"回答问题，不修改笔记","arguments":{"message":"可以在明确下达删除指令后执行。","evidence_ids":[]}}`,
			wantIntent:  ArtifactEditIntentAnswer,
			wantTool:    ArtifactEditToolAnswerQuestion,
		},
		{
			instruction: "把安装部分拆成三个步骤",
			response:    `{"tool":"propose_artifact_patch","reason":"明确要求拆分","public_summary":"准备拆分安装步骤","arguments":{"summary":"拆分安装步骤","patch":{"schema_version":1,"artifact_id":"a","base_version_id":"v","base_version":1,"basis":"user_instruction","evidence_ids":[],"operations":[]}}}`,
			wantIntent:  ArtifactEditIntentEdit,
			wantTool:    ArtifactEditToolProposePatch,
		},
		{
			instruction: "请修改这个名称",
			response:    `{"tool":"propose_artifact_patch","reason":"明确要求修改","public_summary":"准备修改名称","arguments":{"summary":"修改名称","patch":{"schema_version":1,"artifact_id":"a","base_version_id":"v","base_version":1,"basis":"user_instruction","evidence_ids":[],"operations":[]}}}`,
			wantIntent:  ArtifactEditIntentEdit,
			wantTool:    ArtifactEditToolProposePatch,
		},
		{
			instruction: "把相邻的两个章节分组到一个新章节下面；保留两块原正文与引用。",
			response:    `{"tool":"propose_artifact_patch","reason":"明确要求分组","public_summary":"准备分组章节","arguments":{"summary":"分组章节","patch":{"schema_version":1,"artifact_id":"a","base_version_id":"v","base_version":1,"basis":"user_instruction","evidence_ids":[],"operations":[]}}}`,
			wantIntent:  ArtifactEditIntentEdit,
			wantTool:    ArtifactEditToolProposePatch,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.wantTool, func(t *testing.T) {
			t.Parallel()
			chat := &artifactEditFixtureChat{response: test.response}
			planner := NewLLMArtifactEditPlanner(chat)
			registry, digest, err := NewArtifactEditToolRegistry(ArtifactEditModeApply)
			if err != nil {
				t.Fatal(err)
			}
			state := ArtifactEditPlannerState{Instruction: test.instruction, Mode: ArtifactEditModeApply, Intent: ClassifyArtifactEditIntent(test.instruction, ArtifactEditModeApply), ToolSchemaDigest: digest}
			if state.Intent != test.wantIntent {
				t.Fatalf("intent=%s want %s", state.Intent, test.wantIntent)
			}
			decision, _, err := planner.NextDecisionWithUsage(context.Background(), state, registry.Definitions())
			if err != nil {
				t.Fatal(err)
			}
			if err = ValidateArtifactEditDecision(state, registry, decision); err != nil {
				t.Fatal(err)
			}
			if decision.Tool != test.wantTool {
				t.Fatalf("tool=%s want %s", decision.Tool, test.wantTool)
			}
			joined := ""
			for _, message := range chat.messages {
				joined += message.Content
			}
			if !strings.Contains(joined, test.instruction) || !strings.Contains(joined, digest) {
				t.Fatalf("planner prompt omitted frozen request identity: %s", joined)
			}
		})
	}
}

func TestArtifactEditFrozenDigestsCoverBodyScopeAndSchemas(t *testing.T) {
	t.Parallel()
	registry, schemaDigest, err := NewArtifactEditToolRegistry(ArtifactEditModePreview)
	if err != nil {
		t.Fatal(err)
	}
	body := fixtureArtifactEditBody()
	got, err := FreezeArtifactEditDigests(body, []string{"install"}, registry.Definitions())
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolSchema != schemaDigest || len(got.Base) != 64 || len(got.Scope) != 64 {
		t.Fatalf("digests=%#v schema=%s", got, schemaDigest)
	}
	body.Title = "changed"
	changed, _ := FreezeArtifactEditDigests(body, []string{"install"}, registry.Definitions())
	if changed.Base == got.Base || changed.Scope != got.Scope || changed.ToolSchema != got.ToolSchema {
		t.Fatalf("body change produced wrong digest changes: before=%#v after=%#v", got, changed)
	}
	scopeChanged, _ := FreezeArtifactEditDigests(fixtureArtifactEditBody(), []string{"child"}, registry.Definitions())
	if scopeChanged.Scope == got.Scope {
		t.Fatal("scope digest did not change")
	}
	definitions := registry.Definitions()
	definitions[0].InputSchema = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	schemaChanged, _ := FreezeArtifactEditDigests(fixtureArtifactEditBody(), []string{"install"}, definitions)
	if schemaChanged.ToolSchema == got.ToolSchema {
		t.Fatal("tool schema digest did not change")
	}
}
