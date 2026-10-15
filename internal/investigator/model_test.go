package investigator

import (
	"encoding/json"
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
