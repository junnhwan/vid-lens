package service

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

const queryVisualResponseParserVersion = "query-response-v2"
const queryVisualResponseFormatGap = "视觉响应格式未通过校验，未提取结构化事实。"

// parseQueryVisualResponse accepts the facts/gaps object requested by the
// investigator, optionally wrapped in one complete Markdown JSON fence. It
// preserves historical plain-text observations, but never promotes malformed
// or unfamiliar JSON into observed facts. The original response is retained
// separately in the immutable observation for review.
func parseQueryVisualResponse(raw string) ([]string, []string) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, nil
	}
	fenced := strings.HasPrefix(text, "```")
	if fenced {
		var ok bool
		text, ok = unwrapQueryVisualJSONFence(text)
		if !ok {
			return nil, []string{queryVisualResponseFormatGap}
		}
	}
	structured := fenced || strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") || json.Valid([]byte(text))
	if !structured {
		return []string{text}, nil
	}
	facts, gaps, ok := decodeQueryVisualFacts([]byte(text))
	if !ok {
		return nil, []string{queryVisualResponseFormatGap}
	}
	return compactStrings(facts), compactStrings(gaps)
}

func unwrapQueryVisualJSONFence(text string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return "", false
	}
	opener := strings.TrimSpace(lines[0])
	if opener != "```" && !strings.EqualFold(opener, "```json") {
		return "", false
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n")), true
}

// Read the two permitted members explicitly: a struct decoder alone ignores
// unknown members and silently overwrites duplicate keys. Neither should turn
// an unrecognized provider response into structured evidence.
func decodeQueryVisualFacts(data []byte) (facts, gaps []string, ok bool) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, nil, false
	}
	seen := map[string]bool{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, nil, false
		}
		key, isString := keyToken.(string)
		if !isString || key != "facts" && key != "gaps" || seen[key] {
			return nil, nil, false
		}
		seen[key] = true
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, nil, false
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil, nil, false
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			var value string
			item = bytes.TrimSpace(item)
			if len(item) == 0 || item[0] != '"' || json.Unmarshal(item, &value) != nil {
				return nil, nil, false
			}
			values = append(values, value)
		}
		if key == "facts" {
			facts = values
		} else {
			gaps = values
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(seen) == 0 || decoder.Decode(new(any)) != io.EOF {
		return nil, nil, false
	}
	return facts, gaps, true
}
