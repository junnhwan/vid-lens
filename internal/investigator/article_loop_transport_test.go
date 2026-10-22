package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
)

func TestArticleLoopSerialTransportKeepsThinkingAndLegacyOptions(t *testing.T) {
	for _, serial := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "article"}[serial], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				value, present := body["parallel_tool_calls"]
				if serial && (!present || value != false) || !serial && present {
					t.Errorf("serial setting differs from actual wire: serial=%v value=%v present=%v", serial, value, present)
				}
				if body["enable_thinking"] != false {
					t.Error("serial setting overwrote frozen thinking option")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"fixture","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()
			p := config.DefaultProfile()
			p.ModelName = "test"
			p.ModelBaseURL = server.URL
			p.ModelDisableThinking = true
			ctx := context.Background()
			if serial {
				ctx = context.WithValue(ctx, serialArticleToolsKey{}, true)
			}
			model, err := newModelWithClient(ctx, p, "synthetic-test-key", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = model.Generate(context.Background(), []*schema.Message{schema.UserMessage("synthetic fixture")}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestArticleLoopViewWireStatesActualMediaBounds(t *testing.T) {
	infos, err := (&session{}).loopTools(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name == "view_frames" {
			sc, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]any
			raw, _ := json.Marshal(sc)
			if err = json.Unmarshal(raw, &root); err != nil {
				t.Fatal(err)
			}
			props := root["properties"].(map[string]any)
			count := props["count"].(map[string]any)
			if count["minimum"] != float64(1) || count["maximum"] != float64(6) {
				t.Fatal("actual frame limit hidden from schema", count)
			}
			return
		}
	}
	t.Fatal("view tool missing")
}
