package investigator

import (
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestConceptMapFeedbackIdentifiesRootsAndBadSectionReferences(t *testing.T) {
	for _, tc := range []struct {
		name string
		tree domain.MindMap
		want []string
	}{
		{"roots", domain.MindMap{RootID: "missing", Nodes: []domain.MapNode{{ID: "a", Label: "主题"}, {ID: "b", Label: "分组"}}}, []string{"/mind_map", "invalid_root", "parentless_node_ids"}},
		{"section", domain.MindMap{RootID: "a", Nodes: []domain.MapNode{{ID: "a", Label: "主题"}, {ID: "b", Label: "要点", ParentID: domain.Ptr("a"), SectionID: domain.Ptr("invented")}}}, []string{"/mind_map/nodes/1/section_id", "unknown_section", "chapter-one"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := validateConceptMap(&tc.tree, map[string]bool{"chapter-one": true})
			if cause == nil {
				t.Fatal("invalid fixture accepted")
			}
			raw := string(domain.JSON(candidateIssue(authoringConceptMapIssue(&tc.tree, map[string]bool{"chapter-one": true}, cause))))
			for _, want := range tc.want {
				if !strings.Contains(raw, want) {
					t.Fatal("repair information missing", want, raw)
				}
			}
		})
	}
}
