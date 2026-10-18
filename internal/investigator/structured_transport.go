package investigator

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type nativeStrictKey struct{}
type strictTransport struct{ base http.RoundTripper }

func (t strictTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(strictToolRequest(req))
}

// An explicit native Beta endpoint opts into the documented protocol. A model
// name on an OpenAI-compatible gateway is not evidence of strict support.
func nativeStrictEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "api.deepseek.com" && strings.TrimRight(u.Path, "/") == "/beta"
}

func closedStrictSchema(v any) bool {
	switch x := v.(type) {
	case bool:
		return false // arbitrary raw patch values are not a closed schema
	case []any:
		for _, child := range x {
			if !closedStrictSchema(child) {
				return false
			}
		}
	case map[string]any:
		// Native strict mode rejects length/item bounds. Keep these in the
		// authoring contract and server validators; omit only on this wire copy.
		for _, k := range []string{"minItems", "maxItems", "minLength", "maxLength"} {
			delete(x, k)
		}
		if x["type"] == nil && x["anyOf"] == nil {
			return false // untyped RawMessage / arbitrary patch values
		}
		if props, ok := x["properties"].(map[string]any); ok {
			required := map[string]bool{}
			if r, ok := x["required"].([]any); ok {
				for _, k := range r {
					if s, ok := k.(string); ok {
						required[s] = true
					}
				}
			}
			keys := []string{}
			for k, child := range props {
				if !closedStrictSchema(child) {
					return false
				}
				if !required[k] {
					props[k] = map[string]any{"anyOf": []any{child, map[string]any{"type": "null"}}}
				}
				keys = append(keys, k)
			}
			slices.Sort(keys)
			x["required"] = keys
			x["additionalProperties"] = false
		} else if x["type"] == "object" {
			return false // dictionary keys cannot be closed without changing meaning
		}
		for _, k := range []string{"items", "anyOf", "oneOf", "$defs"} {
			if child, ok := x[k]; ok && !closedStrictSchema(child) {
				return false
			}
		}
	}
	return true
}

func strictToolRequest(req *http.Request) *http.Request {
	if req.GetBody == nil {
		return req
	}
	r, err := req.GetBody()
	if err != nil {
		return req
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, 32<<20))
	if err != nil {
		return req
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return req
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return req
	}
	for _, v := range tools {
		t, ok := v.(map[string]any)
		if !ok {
			return req
		}
		f, ok := t["function"].(map[string]any)
		if !ok || !closedStrictSchema(f["parameters"]) {
			return req
		}
		f["strict"] = true
	}
	raw, err = json.Marshal(body)
	if err != nil {
		return req
	}
	copy := req.Clone(req.Context())
	copy.Body = io.NopCloser(bytes.NewReader(raw))
	copy.ContentLength = int64(len(raw))
	copy.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	return copy
}
