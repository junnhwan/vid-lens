package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestModelRateLimitPreservesStructuredRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name, body, header string
		want               time.Duration
	}{
		{"native", `{"retry_policy":{"retry":true,"retry_after":13}}`, "", 13 * time.Second},
		{"nested_message", `{"error":{"message":"{\"retry_policy\":{\"retry\":true,\"retry_after\":13}}"}}`, "", 13 * time.Second},
		{"header_floor", `{"retry_policy":{"retry_after":13}}`, "23", 23 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				w.WriteHeader(429)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			req, _ := http.NewRequest(http.MethodPost, server.URL, nil)
			_, err := (safeTransport{base: http.DefaultTransport}).RoundTrip(req)
			var work *domain.WorkError
			if !errors.As(err, &work) || work.Code != "PROVIDER_RATE_LIMITED" || work.RetryAfter != tc.want {
				t.Fatalf("rate-limit backoff lost: %+v %v", work, err)
			}
		})
	}
}

func TestRunAnytimeModelsCanBindCapabilityTools(t *testing.T) {
	for _, name := range []string{"gpt-6-astra", "gpt-5.5", "gpt-5.6-sol"} {
		t.Run(name, func(t *testing.T) {
			profile := config.DefaultProfile()
			profile.ModelBaseURL = "https://runanytime.hxi.me/v1"
			profile.ModelName = name
			profile.ModelProxyPolicy = "direct"

			base, err := NewModel(context.Background(), profile, "test-key", "", "runanytime.hxi.me")
			if err != nil {
				t.Fatalf("construct OpenAI-compatible model: %v", err)
			}
			inspect, err := utils.InferEnhancedTool("inspect_model_screenshot", "Return the actual pixels of the model-admin screenshot for visual inspection.", func(context.Context, struct{}) (*schema.ToolResult, error) {
				return &schema.ToolResult{}, nil
			})
			if err != nil {
				t.Fatalf("build image tool schema: %v", err)
			}
			submit, err := utils.InferEnhancedTool("submit_probe", "Submit screenshot observations.", func(context.Context, capabilityProbeObservation) (*schema.ToolResult, error) {
				return &schema.ToolResult{}, nil
			})
			if err != nil {
				t.Fatalf("build submission tool schema: %v", err)
			}
			inspectInfo, err := inspect.Info(context.Background())
			if err != nil {
				t.Fatalf("read image tool schema: %v", err)
			}
			submitInfo, err := submit.Info(context.Background())
			if err != nil {
				t.Fatalf("read submission tool schema: %v", err)
			}
			if _, err = base.WithTools([]*schema.ToolInfo{inspectInfo, submitInfo}); err != nil {
				t.Fatalf("bind capability tools to model: %v", err)
			}
		})
	}
}

func TestRunAnytimeModelsSerializeCapabilityToolRequests(t *testing.T) {
	for _, name := range []string{"gpt-6-astra", "gpt-5.5", "gpt-5.6-sol"} {
		t.Run(name, func(t *testing.T) {
			requests := make(chan map[string]json.RawMessage, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				requests <- body
				w.Header().Set("Content-Type", "application/json")
				toolName := "inspect_model_screenshot"
				if strings.Contains(string(body["tool_choice"]), "submit_probe") {
					toolName = "submit_probe"
				}
				response, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]string{"name": toolName, "arguments": "{}"}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
				_, _ = fmt.Fprint(w, string(response))
			}))
			defer server.Close()

			profile := config.DefaultProfile()
			profile.ModelBaseURL = server.URL
			profile.ModelName = name
			profile.ModelProxyPolicy = "direct"
			base, err := NewModel(context.Background(), profile, "test-key", "", server.Listener.Addr().String())
			if err != nil {
				t.Fatalf("construct model: %v", err)
			}
			inspect, err := utils.InferEnhancedTool("inspect_model_screenshot", "Return the actual screenshot pixels.", func(context.Context, struct{}) (*schema.ToolResult, error) {
				return &schema.ToolResult{}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			info, err := inspect.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			submit, err := utils.InferEnhancedTool("submit_probe", "Submit screenshot observations.", func(context.Context, capabilityProbeObservation) (*schema.ToolResult, error) {
				return &schema.ToolResult{}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			submitInfo, err := submit.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			bridge := &imageBridge{base: base, modelName: name}
			bound, err := bridge.WithTools([]*schema.ToolInfo{info, submitInfo})
			if err != nil {
				t.Fatalf("bind capability tools: %v", err)
			}
			bridge = bound.(*imageBridge)
			stream, err := bridge.Stream(context.Background(), []*schema.Message{schema.UserMessage("Inspect the image and return the actual screenshot pixels.")})
			if err != nil {
				t.Fatalf("create capability request: %v", err)
			}
			response, err := stream.Recv()
			if err != nil || response == nil || len(response.ToolCalls) != 1 {
				t.Fatalf("read tool-call response: response=%v err=%v", response, err)
			}
			stream.Close()
			body := <-requests
			var modelName string
			if err := json.Unmarshal(body["model"], &modelName); err != nil || modelName != name {
				t.Fatalf("unexpected model field: %s err=%v", body["model"], err)
			}
			if string(body["stream"]) == "true" || !strings.Contains(string(body["tools"]), "inspect_model_screenshot") || !strings.Contains(string(body["tools"]), "submit_probe") {
				t.Fatalf("capability request must include both tools: %s", body)
			}
			if name == "gpt-5.6-sol" {
				var choice string
				if err := json.Unmarshal(body["tool_choice"], &choice); err != nil || choice != "auto" {
					t.Fatalf("GPT-5.6-Sol first capability turn should use automatic tool choice: %s", body["tool_choice"])
				}
			}
			if name != "gpt-5.6-sol" && !strings.Contains(string(body["tool_choice"]), "inspect_model_screenshot") {
				t.Fatalf("first capability turn did not force screenshot inspection: %s", body["tool_choice"])
			}
			if string(body["max_completion_tokens"]) != "10000" || len(body["max_tokens"]) != 0 || len(body["temperature"]) != 0 {
				t.Fatalf("reasoning GPT request used incompatible sampling/budget fields: %s", body)
			}

			second, err := bridge.Stream(context.Background(), []*schema.Message{
				schema.UserMessage("Inspect the image and return the actual screenshot pixels."),
				{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "inspect-call", Type: "function", Function: schema.FunctionCall{Name: "inspect_model_screenshot", Arguments: "{}"}}}},
				{Role: schema.Tool, ToolCallID: "inspect-call", Content: "Screenshot pixels returned."},
			})
			if err != nil {
				t.Fatalf("create structured submission request: %v", err)
			}
			_, _ = second.Recv()
			second.Close()
			secondBody := <-requests
			if !strings.Contains(string(secondBody["tool_choice"]), "submit_probe") || !strings.Contains(string(secondBody["tools"]), "submit_probe") || !strings.Contains(string(secondBody["tools"]), "inspect_model_screenshot") {
				t.Fatalf("second capability turn did not force structured submission: %s", secondBody)
			}
		})
	}
}

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

func TestCapabilityToolChoiceAdvancesToSubmissionAfterScreenshot(t *testing.T) {
	if got := nextCapabilityTool([]*schema.Message{schema.UserMessage("check")}); got != "inspect_model_screenshot" {
		t.Fatalf("first model turn must inspect the screenshot, got %q", got)
	}
	if got := nextCapabilityTool([]*schema.Message{
		schema.UserMessage("check"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "inspect_model_screenshot"}}}},
		{Role: schema.Tool, ToolCallID: "inspect-call"},
	}); got != "submit_probe" {
		t.Fatalf("after screenshot inspection the model must submit structured observations, got %q", got)
	}
}

func TestProviderDiagnosticCapturesOnlyRedactedErrorFields(t *testing.T) {
	observer := newUsageObserver("test", "", nil)
	observer.providerFailure([]byte(`{"error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"tools[0].strict","message":"bad key sk-abcdefghijk and Bearer private-token"}}`))
	if observer.report.ProviderErrorType != "invalid_request_error" || observer.report.ProviderErrorCode != "unsupported_parameter" || observer.report.ProviderErrorParam != "tools[0].strict" {
		t.Fatalf("provider error fields were not captured: %+v", observer.report)
	}
	if strings.Contains(observer.report.ProviderErrorMessage, "sk-") || strings.Contains(observer.report.ProviderErrorMessage, "private-token") {
		t.Fatalf("provider error message leaked credentials: %q", observer.report.ProviderErrorMessage)
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
