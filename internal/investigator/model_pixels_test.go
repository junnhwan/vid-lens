package investigator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/schema"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestCurrentContractsDeliverAllPixelsInFinalUserMessage(t *testing.T) {
	for _, contract := range []*domain.ExecutionContract{{Version: domain.ArticleLoopV2}, {Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV2}, {Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV3}, {Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV4}, {Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV5}} {
		t.Run(contract.Version+contract.InputRevision, func(t *testing.T) {
			s := &session{cp: checkpoint{ExecutionContract: contract}}
			image := func(id string) *schema.Message {
				return &schema.Message{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: "original metadata " + id}, {Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: domain.Ptr(base64.StdEncoding.EncodeToString([]byte(id))), MIMEType: "image/jpeg"}}, Extra: map[string]any{"frame_id": id}}}}
			}
			tool := image("second-frame")
			tool.Role = schema.Tool
			tool.ToolCallID = "read-frame"
			input := []*schema.Message{schema.SystemMessage("rules"), image("first-frame"), {Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "read-frame", Function: schema.FunctionCall{Name: "inspect"}}}}, tool, schema.UserMessage("full candidate and current readiness")}
			before := string(domain.JSON(input))
			out := s.modelImageInput(input)
			if err := validateProtocol(out); err != nil {
				t.Fatal(err)
			}
			if string(domain.JSON(input)) != before {
				t.Fatal("transport mutated frozen input")
			}
			count := 0
			for i, m := range out {
				for _, p := range m.UserInputMultiContent {
					if p.Type == schema.ChatMessagePartTypeImageURL {
						count++
						if i != len(out)-1 || m.Role != schema.User {
							t.Fatal("relay will filter earlier pixels", i, len(out))
						}
					}
				}
			}
			if count != 2 {
				t.Fatal("pixel loss or duplication", count)
			}
			if !strings.Contains(string(domain.JSON(out)), "full candidate and current readiness") || !strings.Contains(string(domain.JSON(out)), "original metadata first-frame") {
				t.Fatal("text lost")
			}
			got := s.modelImageInput(out)
			if string(domain.JSON(got)) != string(domain.JSON(out)) {
				t.Fatal("normalization must be idempotent")
			}
			var wire struct {
				Messages []struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(b, &wire); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			p := config.DefaultProfile()
			p.ModelBaseURL = server.URL
			base, err := newModelWithClient(context.Background(), p, "fixture", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = completeModelResponse(context.Background(), base, out); err != nil {
				t.Fatal(err)
			}
			for i, m := range wire.Messages {
				if strings.Contains(string(m.Content), "image_url") && (i != len(wire.Messages)-1 || m.Role != "user") {
					t.Fatal("SDK sent images before final user message")
				}
			}
		})
	}
}
func TestHistoricalImageMessageOrderingRemainsFrozen(t *testing.T) {
	input := []*schema.Message{schema.UserMessage("question"), {Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: domain.Ptr("pixels"), MIMEType: "image/jpeg"}}}}}, schema.UserMessage("candidate")}
	for _, c := range []*domain.ExecutionContract{{Version: domain.ArticleLoopV1}, {Version: domain.ExecutionContractV12}, {Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV1}} {
		s := &session{cp: checkpoint{ExecutionContract: c}}
		if string(domain.JSON(s.modelImageInput(input))) != string(domain.JSON(NormalizeToolImages(input))) {
			t.Fatal("historical wire ordering changed")
		}
	}
}
