package investigator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestSDKPixelTransportReceiptsBindExactBytesWithoutStoringPixels(t *testing.T) {
	for _, last := range []bool{false, true} {
		t.Run(fmt.Sprint("image_last_", last), func(t *testing.T) {
			pixels := []byte("synthetic-image-wire-bytes")
			encoded := base64.StdEncoding.EncodeToString(pixels)
			var received []pixelInputReceipt
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				o := newUsageObserver("test", "provider_default", nil)
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				request, _ := http.NewRequest("POST", "http://fixture/", strings.NewReader(string(body)))
				o.wire(request)
				received = o.report.PixelInputs
				if strings.Contains(string(domain.JSON(o.report)), encoded) {
					t.Error("receipt retained pixels")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			p := config.DefaultProfile()
			p.ModelBaseURL = server.URL
			base, err := newModelWithClient(context.Background(), p, "fixture", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			image := &schema.Message{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: &encoded, MIMEType: "image/jpeg"}}}}}
			text := schema.UserMessage("candidate after or before the actual image")
			messages := []*schema.Message{schema.SystemMessage("inspect"), image, text}
			if last {
				messages[1], messages[2] = text, image
			}
			if _, err = completeModelResponse(context.Background(), base, NormalizeToolImages(messages)); err != nil {
				t.Fatal(err)
			}
			if len(received) != 1 || received[0].SHA256 != digest(pixels) || received[0].Bytes != len(pixels) {
				t.Fatal("actual SDK request lost exact image bytes", received)
			}
		})
	}
}

func TestUsageObserverChunkBoundariesAndPresence(t *testing.T) {
	for _, tc := range []struct {
		name, usage, read, write string
		value                    int64
	}{
		{"missing", `{"prompt_tokens":40}`, "unknown", "unknown", 0},
		{"zero", `{"prompt_tokens":40,"prompt_tokens_details":{"cached_tokens":0}}`, "reported", "unknown", 0},
		{"hit", `{"prompt_tokens":40,"prompt_tokens_details":{"cached_tokens":32},"cache_creation_input_tokens":4}`, "reported", "reported", 32},
		{"null", `{"prompt_tokens_details":{"cached_tokens":null}}`, "null", "unknown", 0},
		{"negative", `{"prompt_tokens_details":{"cached_tokens":-1}}`, "invalid", "unknown", 0},
		{"wrong_type", `{"prompt_tokens_details":{"cached_tokens":"0"}}`, "invalid", "unknown", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `data: {"choices":[{"finish_reason":"stop"}],"usage":` + tc.usage + "}\r\n\r\n"
			body += body + "data: [DONE]\n\n"
			for _, size := range []int{1, 7, 4096} {
				var got usageReport
				o := newUsageObserver("legacy", "provider_default", func(r usageReport) { got = r })
				for i := 0; i < len(body); i += size {
					o.consume([]byte(body[i:min(i+size, len(body))]))
				}
				o.finish(true)
				o.finish(false)
				if got.CacheRead.Status != tc.read || got.CacheWrite.Status != tc.write || got.StreamStatus != "complete" {
					t.Fatalf("%+v", got)
				}
				if got.CacheRead.Value != nil && *got.CacheRead.Value != tc.value {
					t.Fatalf("cumulative usage doubled: %+v", got)
				}
			}
		})
	}
}
func TestUsageObserverOversizedEventAndIncomplete(t *testing.T) {
	var got usageReport
	o := newUsageObserver("legacy", "", func(r usageReport) { got = r })
	o.consume([]byte("data: " + strings.Repeat("x", 70<<10) + "\n\ndata: {\"usage\":{\"cache_read_input_tokens\":2}}\n\n"))
	o.finish(false)
	if got.StreamStatus != "incomplete" || got.CacheRead.Value == nil || *got.CacheRead.Value != 2 {
		t.Fatalf("%+v", got)
	}
}
func TestTransportObservesUsageOnlyWithoutChangingAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":1,\"total_tokens\":41,\"prompt_tokens_details\":{\"cached_tokens\":0}}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p := config.DefaultProfile()
	p.ModelBaseURL = server.URL
	p.ModelName = "fixture"
	base, e := newModelWithClient(context.Background(), p, "fixture", &http.Client{Transport: safeTransport{http.DefaultTransport}})
	if e != nil {
		t.Fatal(e)
	}
	var got usageReport
	o := newUsageObserver("source-context-v2", "provider_default", func(r usageReport) { got = r })
	ctx := context.WithValue(context.Background(), usageKey{}, o)
	r, e := completeModelResponse(ctx, base, []*schema.Message{schema.SystemMessage("rules"), schema.UserMessage("complete source"), schema.UserMessage("fixed article"), schema.UserMessage("dynamic question")})
	if e != nil || r.Content != "ok" {
		t.Fatalf("%+v %v", r, e)
	}
	if got.CacheRead.Status != "reported" || got.CacheRead.Value == nil || *got.CacheRead.Value != 0 || got.Total.Value == nil || *got.Total.Value != 41 || got.StreamStatus != "complete" {
		t.Fatalf("%+v", got)
	}
	if got.StablePrefixHash == "" {
		t.Fatal("SDK wire prefix was not observed")
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "complete source") {
		t.Fatal("telemetry contains private text")
	}
}

func TestFirstContentIgnoresRoleHeartbeatAndPrivateThinking(t *testing.T) {
	var got usageReport
	o := newUsageObserver(draftLayout, "provider_default", func(r usageReport) { got = r })
	o.consume([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
	o.consume([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"private\"}}]}\n\n"))
	if o.report.FirstMeaningfulMS != nil {
		t.Fatal("role/private thinking counted as first content")
	}
	o.consume([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"begin_article\"}}]}}]}\n\ndata: [DONE]\n\n"))
	o.finish(true)
	if got.FirstByteMS == nil || got.FirstMeaningfulMS == nil || *got.FirstMeaningfulMS < *got.FirstByteMS {
		t.Fatalf("timing missing or inverted: %+v", got)
	}
}
