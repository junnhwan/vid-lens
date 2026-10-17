package investigator

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestObservationContractAcceptsActivityMetadataAndRejectsUnknownFields(t *testing.T) {
	s := &session{cp: checkpoint{HarnessRevision: 5, Inspections: map[string][]string{"overview": {"frame"}}}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	info, _ := s.tools["record_observation"].Info(context.Background())
	sc, _ := info.ParamsOneOf.ToJSONSchema()
	if !strings.Contains(string(domain.JSON(sc)), `"activity_title"`) {
		t.Error("instruction metadata missing from actual tool schema")
	}
	if !strings.Contains(string(domain.JSON(sc)), `"maxItems":16`) || !strings.Contains(string(domain.JSON(sc)), `"maxLength":1024`) {
		t.Fatal("observation bounds missing from transmitted schema")
	}
	args := `{"inspection_id":"overview","frame_ids":["frame"],"visible_facts":["合成蓝色画面"],"status":"bad","activity_title":"记录画面事实"}`
	result, err := s.tools["record_observation"].InvokableRun(context.Background(), &schema.ToolArgument{Text: args})
	if err != nil || !strings.Contains(string(domain.JSON(result)), "status 必须") {
		t.Fatalf("activity metadata blocked semantic validation: %v %v", result, err)
	}
	args = strings.Replace(args, `"activity_title"`, `"invented_field"`, 1)
	result, err = s.tools["record_observation"].InvokableRun(context.Background(), &schema.ToolArgument{Text: args})
	if err != nil || !strings.Contains(string(domain.JSON(result)), "unknown field") {
		t.Fatal("unknown fields silently accepted", result, err)
	}
}

func TestObservationContractKeepsHistoricalSchemasAndBoundsLongRecords(t *testing.T) {
	s := &session{cp: checkpoint{HarnessRevision: 4}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	info, _ := s.tools["record_observation"].Info(context.Background())
	sc, _ := info.ParamsOneOf.ToJSONSchema()
	if strings.Contains(string(domain.JSON(sc)), `"activity_title"`) {
		t.Fatal("historical observation schema changed")
	}
	if validObservationSize(observation{VisibleFacts: []string{strings.Repeat("字", 1025)}}) {
		t.Fatal("long screenshot transcription accepted")
	}
	if !validObservationSize(observation{VisibleFacts: []string{"必要的合成画面事实"}}) {
		t.Fatal("concise facts rejected")
	}
	oversized := observation{}
	for i := 0; i < 16; i++ {
		oversized.VisibleFacts = append(oversized.VisibleFacts, strings.Repeat("字", 1000))
	}
	if validObservationSize(oversized) {
		t.Fatal("aggregate observation payload exceeds its durable capsule budget")
	}
}

func TestMalformedToolResponseIsNotPresentedAsReadyAction(t *testing.T) {
	result := schema.AssistantMessage("", []schema.ToolCall{{Function: schema.FunctionCall{Name: "record_observation", Arguments: `{"visible_facts":["unfinished`}}})
	title, outcome := responseActivity(result)
	if !strings.Contains(title, "纠正") || !strings.Contains(outcome, "尚未执行") {
		t.Fatal(title, outcome)
	}
}

func TestOrdinaryObservationDoesNotNeedAnotherModelCallForFourExtraCharacters(t *testing.T) {
	if !validObservationSize(observation{VisibleFacts: []string{strings.Repeat("字", 244), strings.Repeat("字", 270)}}) {
		t.Fatal("bounded ordinary facts unnecessarily require a new model response")
	}
}
