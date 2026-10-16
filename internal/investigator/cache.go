package investigator

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
)

func cacheOptions(p config.Profile) []model.Option {
	if p.CachePolicy != "qwen_explicit" || !p.CacheHintsVerified {
		return nil
	}
	return []model.Option{openai.WithRequestPayloadModifier(cachePayload)}
}
func cachePayload(_ context.Context, _ []*schema.Message, raw []byte) ([]byte, error) {
	var body map[string]json.RawMessage
	if e := json.Unmarshal(raw, &body); e != nil {
		return nil, e
	}
	var messages []map[string]json.RawMessage
	if e := json.Unmarshal(body["messages"], &messages); e != nil {
		return nil, e
	}
	// Mark the last frozen textual message only. Never mutate tool exchanges,
	// roles, images or an unrecognized multimodal content block.
	for i := min(2, len(messages)-1); i >= 0; i-- {
		var text string
		if json.Unmarshal(messages[i]["content"], &text) != nil || text == "" {
			continue
		}
		content, e := json.Marshal([]map[string]any{{"type": "text", "text": text, "cache_control": map[string]string{"type": "ephemeral"}}})
		if e != nil {
			return nil, e
		}
		messages[i]["content"] = content
		break
	}
	body["messages"], _ = json.Marshal(messages)
	return json.Marshal(body)
}
