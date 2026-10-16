package domain

import "strings"

// ModelFamily identifies the same version of a model across provider routes.
// Only known publisher namespaces are aliases; display names, unknown prefixes,
// version suffixes and reasoning variants never establish equivalence.
func ModelFamily(brand, upstream string) string {
	brand = strings.ToLower(strings.TrimSpace(brand))
	upstream = strings.ToLower(strings.TrimSpace(upstream))
	namespace, name, slash := strings.Cut(upstream, "/")
	publisher := map[string]string{"openai": "openai", "deepseek": "deepseek", "z-ai": "glm", "glm": "glm", "qwen": "qwen", "google": "gemini", "anthropic": "claude", "moonshotai": "kimi", "xiaomi": "mimo", "x-ai": "grok"}[namespace]
	base := upstream
	if slash && publisher != "" && !strings.Contains(name, "/") {
		base = name
	}
	// Older imported routes use the generic icon. Infer only unambiguous model
	// families from upstream IDs, never the owner-editable display name.
	if brand == "" || brand == "generic" {
		for _, known := range []struct{ prefix, brand string }{{"deepseek-", "deepseek"}, {"glm-", "glm"}, {"gpt-", "openai"}, {"qwen", "qwen"}, {"gemini-", "gemini"}, {"claude-", "claude"}, {"kimi-", "kimi"}, {"mimo-", "mimo"}, {"grok-", "grok"}} {
			if strings.HasPrefix(base, known.prefix) {
				brand = known.brand
				break
			}
		}
	}
	if slash && name != "" && !strings.Contains(name, "/") {
		if publisher != "" && publisher == brand {
			upstream = name
		}
	}
	return brand + ":" + upstream
}
