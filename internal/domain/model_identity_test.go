package domain

import "testing"

func TestModelFamilyOnlyNormalizesKnownPublisherAliases(t *testing.T) {
	for _, tc := range []struct {
		brand, a, b string
		same        bool
	}{
		{"deepseek", "deepseek-v4.1-flash", "deepseek/deepseek-v4.1-flash", true},
		{"glm", "glm-5.3-flash", "z-ai/glm-5.3-flash", true},
		{"openai", "GPT-6-ASTRA", "openai/gpt-6-astra", true},
		{"openai", "gpt-6-astra", "gpt-5.5", false},
		{"deepseek", "deepseek-v4.1-flash", "deepseek-v4.1-flash-thinking", false},
		{"generic", "model-v1", "reseller/model-v1", false},
		{"openai", "gpt-6-astra", "custom/gpt-6-astra", false},
	} {
		if got := ModelFamily(tc.brand, tc.a) == ModelFamily(tc.brand, tc.b); got != tc.same {
			t.Fatalf("family(%q, %q, %q) same=%v, want %v", tc.brand, tc.a, tc.b, got, tc.same)
		}
	}
	if ModelFamily("openai", "shared-name") == ModelFamily("glm", "shared-name") {
		t.Fatal("different brands collapsed")
	}
	for _, pair := range [][4]string{{"generic", "deepseek-v4.1-flash", "deepseek", "deepseek/deepseek-v4.1-flash"}, {"generic", "glm-5.3-flash", "glm", "z-ai/glm-5.3-flash"}} {
		if ModelFamily(pair[0], pair[1]) != ModelFamily(pair[2], pair[3]) {
			t.Fatalf("legacy generic brand did not join exact upstream identity: %v", pair)
		}
	}
}
