package summarydoc

import (
	"errors"
	"strings"
	"testing"
)

func TestGeneratedPolicyLeavesExistingAndHumanDocumentsCompatible(t *testing.T) {
	for _, mode := range []string{"missing_refs", "internal_marker", "empty_shell"} {
		t.Run(mode, func(t *testing.T) {
			doc, ctx := fixture()
			if mode == "missing_refs" {
				doc.Blocks[0].SourceRefs = nil
			} else if mode == "empty_shell" {
				doc.Blocks[0].BodyMarkdown = ""
				doc.Blocks[0].SourceRefs = nil
			} else {
				doc.Blocks[0].BodyMarkdown += " [asr-window-17:0:96]"
			}
			if err := Validate(doc, ctx); err != nil {
				t.Fatalf("legacy/edit contract changed: %v", err)
			}
			var policy *GenerationContentError
			if err := ValidateGeneratedContent(doc, ctx); !errors.As(err, &policy) {
				t.Fatalf("generated policy allowed defect: %v", err)
			}
			if _, err := Markdown(doc); err != nil {
				t.Fatal("historical projection became unreadable", err)
			}
		})
	}
}

func TestGeneratedPolicyAllowsReferencedHierarchyAndUntimedSource(t *testing.T) {
	doc, ctx := fixture()
	parent := "architecture"
	doc.Blocks[0].ParentID = &parent
	doc.Blocks = append(doc.Blocks, Block{ID: parent, Order: 0, Title: "架构与条件"})
	if err := ValidateGeneratedContent(doc, ctx); err != nil {
		t.Fatal(err)
	}
	for id := range ctx.Cues {
		ctx.Cues[id] = Cue{TimingMethod: "unknown"}
	}
	doc.Blocks[0].SourceRefs[0].StartMS = nil
	doc.Blocks[0].SourceRefs[0].EndMS = nil
	doc.Blocks[0].SourceRefs[0].TimingMethod = "unknown"
	if err := ValidateGeneratedContent(doc, ctx); err != nil {
		t.Fatal("untimed legitimate source rejected", err)
	}
	data, _ := CanonicalJSON(doc)
	if _, err := Parse([]byte(strings.Replace(string(data), `"order":0`, `"order":0,"unexpected":true`, 1))); err == nil {
		t.Fatal("unknown fields allowed")
	}
}
