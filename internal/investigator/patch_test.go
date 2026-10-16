package investigator

import (
	"encoding/json"
	"strings"
	"testing"

	"vidlens/internal/config"
)

func TestRestrictedPatchPreservesOtherFieldsAndRejectsStructuralEdits(t *testing.T) {
	raw := []byte(`{"title":"keep","sections":{"one":{"blocks":[{"caption":"wrong","frame_ids":["real"]}]},"two":{"blocks":[{"text":"preserve"}]}}}`)
	allow := func(p []string) bool { return len(p) > 2 && p[0] == "sections" && p[2] == "blocks" }
	patched, err := replaceFields(raw, []fieldPatch{{Op: "replace", Path: "/sections/one/blocks/0/caption", Value: json.RawMessage(`"correct"`)}}, allow)
	if err != nil || !strings.Contains(string(patched), `"caption":"correct"`) || !strings.Contains(string(patched), `"text":"preserve"`) || !strings.Contains(string(patched), `"frame_ids":["real"]`) {
		t.Fatalf("%s %v", patched, err)
	}
	for _, patch := range []fieldPatch{{Op: "add", Path: "/sections/one/blocks/0/caption", Value: json.RawMessage(`"x"`)}, {Op: "replace", Path: "/snapshot_id", Value: json.RawMessage(`"fake"`)}, {Op: "replace", Path: "/sections/one/blocks/-", Value: json.RawMessage(`{}`)}, {Op: "replace", Path: "/sections/one/blocks/01", Value: json.RawMessage(`{}`)}, {Op: "replace", Path: "/sections/one/blocks/0/new", Value: json.RawMessage(`{}`)}} {
		if _, err = replaceFields(raw, []fieldPatch{patch}, allow); err == nil {
			t.Fatalf("unsafe patch accepted: %+v", patch)
		}
	}
}

func TestSeparateBudgetDoesNotReserveMaximumOutputForEveryCall(t *testing.T) {
	p := config.DefaultProfile()
	p.MaxOutputTokens = 393216
	p.MaxInputChars = 1712864
	if separateInputLimit(p) != 140000 {
		t.Fatal("imported combined profile lost original input capacity")
	}
	p.InputCharsLimit = 300000
	p.ContextTokens = 100000
	b := callBudget{InputChars: 200000, EstimatedTextTokens: 60000, EstimatedImageTokens: 8192, OutputTokens: 8192, SafetyTokens: 2048}
	if err := b.check(p); err != nil {
		t.Fatal("investigation was rejected using the unrelated 384K output maximum", err)
	}
	b.OutputTokens = 32768
	if err := b.check(p); err == nil {
		t.Fatal("shared context overflow was not rejected")
	}
	b.OutputTokens = 8192
	b.EstimatedImageTokens = 40000
	if err := b.check(p); err == nil {
		t.Fatal("pixel reserve omitted")
	}
}
