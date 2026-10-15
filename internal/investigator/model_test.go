package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestToolImageBridgePreservesCompletedCallGroup(t *testing.T) {
	msgs := []*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "a"}, {ID: "b"}}}, {Role: schema.Tool, ToolCallID: "a", UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: "frame a"}, {Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: domain.Ptr("actual-pixel-data"), MIMEType: "image/jpeg"}}, Extra: map[string]any{"frame_id": "frame-a"}}}}, {Role: schema.Tool, ToolCallID: "b", Content: "other result"}}
	bridged := NormalizeToolImages(msgs)
	if len(bridged) != 4 || bridged[1].Role != schema.Tool || bridged[2].ToolCallID != "b" || bridged[3].Role != schema.User || len(bridged[1].UserInputMultiContent) > 0 {
		t.Fatal("tool group was interrupted")
	}
	if e := validateProtocol(bridged); e != nil {
		t.Fatal(e)
	}
	wire, _ := json.Marshal(bridged)
	if !strings.Contains(string(wire), "actual-pixel-data") {
		t.Fatal("image was dropped before the model")
	}
	saved, _ := json.Marshal(stripPixels(msgs))
	if strings.Contains(string(saved), "actual-pixel-data") || !strings.Contains(string(saved), "frame-a") {
		t.Fatal("checkpoint did not retain only the reloadable frame reference")
	}
}
func TestProtocolRejectsOrphansAndMissingResults(t *testing.T) {
	for _, msgs := range [][]*schema.Message{{{Role: schema.Tool, ToolCallID: "missing"}}, {{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "pending"}}}}, {{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "pending"}}}, {Role: schema.User, Content: "interruption"}}} {
		if e := validateProtocol(msgs); e == nil {
			t.Fatal("invalid exchange accepted")
		}
	}
}
func TestMultimodalUserTextIsNotSentTwice(t *testing.T) {
	input := []*schema.Message{{Role: schema.User, Content: "source metadata", UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: domain.Ptr("pixels"), MIMEType: "image/jpeg"}}}}}}
	normalized := NormalizeToolImages(input)
	if normalized[0].Content != "" || len(normalized[0].UserInputMultiContent) != 2 || normalized[0].UserInputMultiContent[0].Text != "source metadata" {
		t.Fatal("adapter would reject Content plus MultiContent")
	}
	if input[0].Content != "source metadata" {
		t.Fatal("transport changed durable input")
	}
}
func TestProviderThoughtsAreNeverCheckpointData(t *testing.T) {
	msgs := []*schema.Message{{Role: schema.Assistant, ReasoningContent: "private-thought", Extra: map[string]any{"reasoning-content": "private-thought", "openai-request-id": "request-id"}}}
	b, _ := json.Marshal(stripPixels(msgs))
	if strings.Contains(string(b), "private-thought") || !strings.Contains(string(b), "request-id") {
		t.Fatal("provider thinking leaked into durable messages")
	}
}
func TestMindMapRejectsDisconnectedCycles(t *testing.T) {
	parent := "b"
	other := "a"
	m := &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{{ID: "root", Label: "article"}, {ID: "a", ParentID: &parent, Label: "a"}, {ID: "b", ParentID: &other, Label: "b"}}}
	if e := validateMap(m, map[string]bool{}); e == nil {
		t.Fatal("disconnected cycle accepted")
	}
}

func TestCandidateRepairReportsIndependentBlockProblemsTogether(t *testing.T) {
	s := &session{}
	err := s.validateBlocks(context.Background(), []domain.Block{
		{ID: "dialogue-quote", Type: "quote", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "a quote"}}},
		{ID: "credits-quote", Type: "quote", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "another quote"}}},
		{ID: "unsafe-link", Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "link", URL: "javascript:alert(1)", Text: "link"}}},
	}, map[string]domain.Citation{}, true)
	if err == nil {
		t.Fatal("invalid candidate accepted")
	}
	for _, id := range []string{"dialogue-quote", "credits-quote", "unsafe-link"} {
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("single repair cannot fix all problems; missing %s in %v", id, err)
		}
	}
}

func TestMalformedCandidateUsesCandidateRepairInsteadOfInternalFailure(t *testing.T) {
	s := &session{}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"title":"candidate","citations":{"c":{}}`, `{"title":"candidate","citations":{"limitations":[]}}`} {
		_, err := s.tools["submit_article"].InvokableRun(context.Background(), &schema.ToolArgument{Text: raw})
		var candidate *candidateError
		if !errors.As(err, &candidate) {
			t.Fatalf("malformed candidate bypassed bounded repair: %v", err)
		}
	}
}

func TestObservationTypoReportsCurrentInspectionIDsWithoutAcceptingIt(t *testing.T) {
	actual := "59686da4632c84510717dab33fc394f8"
	typo := "59686da4632c84510717dab3fc394f8"
	s := &session{cp: checkpoint{Inspections: map[string][]string{"overview": {actual}}}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	result, err := s.tools["record_observation"].InvokableRun(context.Background(), &schema.ToolArgument{Text: string(domain.JSON(observation{InspectionID: "overview", FrameIDs: []string{typo}, VisibleFacts: []string{"test"}, Status: "supported"}))})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(domain.JSON(result))
	if !strings.Contains(wire, actual) || !strings.Contains(wire, typo) || strings.Contains(wire, `"saved":true`) {
		t.Fatalf("unactionable or unsafe ID correction: %s", wire)
	}
}

func TestInvalidEvidenceToolArgumentsReturnCorrectableFeedback(t *testing.T) {
	s := &session{}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	result, err := s.tools["record_observation"].InvokableRun(context.Background(), &schema.ToolArgument{Text: `{"inspection_id":"question-context","unresolved":"a string instead of an array","status":"completed"}`})
	if err != nil {
		t.Fatalf("provider argument mistake became an internal failure: %v", err)
	}
	wire := string(domain.JSON(result))
	if !strings.Contains(wire, "correct_arguments_and_try_again") || !strings.Contains(wire, "unresolved") {
		t.Fatalf("missing field-level correction: %s", wire)
	}
}

func TestParaphrasedCitationGetsExplicitNullQuoteCorrection(t *testing.T) {
	s := &session{snapshot: domain.Snapshot{ID: "snapshot", DurationMS: 1000}, transcript: []domain.Segment{{ID: "segment", Ordinal: 0, StartMS: 0, EndMS: 1000, Text: "It handles difficult decisions for me.", Origin: "asr"}}}
	c := domain.Citation{ID: "source", Kind: "video_text", Label: "原文", SnapshotID: "snapshot", Range: &domain.Range{StartMS: 0, EndMS: 1000}, SegmentIDs: []string{"segment"}, TextOrigin: "asr", Quote: domain.Ptr("It helps me make choices.")}
	err := s.validateCitation(context.Background(), c, true)
	if err == nil || !strings.Contains(err.Error(), "quote=null") {
		t.Fatalf("paraphrase needs actionable exact-quote correction: %v", err)
	}
	c.Quote = nil
	if err = s.validateCitation(context.Background(), c, true); err != nil {
		t.Fatalf("source-backed paraphrase with no verbatim quote rejected: %v", err)
	}
}
