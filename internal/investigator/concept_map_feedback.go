package investigator

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"vidlens/internal/domain"
)

// The original validator remains the publication gate. This projection gives
// the author actionable paths without silently rewriting the model's tree.
func authoringConceptMapIssue(m *domain.MindMap, sections map[string]bool, cause error) error {
	issues := []map[string]any{}
	add := func(path, code string, actual, expected any) {
		issues = append(issues, map[string]any{"path": path, "code": code, "actual": actual, "expected": expected, "repairable": true})
	}
	sectionIDs := make([]string, 0, len(sections))
	for id := range sections {
		sectionIDs = append(sectionIDs, id)
	}
	slices.Sort(sectionIDs)
	if m == nil {
		add("/mind_map", "missing_concept_map", nil, "one rooted concept tree with leaves linked to planned sections")
	} else {
		ids := map[string]bool{}
		byID := map[string]domain.MapNode{}
		parents := map[string]bool{}
		roots := []string{}
		for i, n := range m.Nodes {
			path := fmt.Sprintf("/mind_map/nodes/%d", i)
			if n.ID == "" || ids[n.ID] {
				add(path+"/id", "invalid_node_id", n.ID, "a nonempty unique node ID")
			}
			ids[n.ID] = true
			byID[n.ID] = n
			if n.Label == "" {
				add(path+"/label", "empty_label", n.Label, "a supported concept label")
			}
			if n.ParentID == nil {
				roots = append(roots, n.ID)
			} else {
				parents[*n.ParentID] = true
			}
			if n.SectionID != nil && !sections[*n.SectionID] {
				add(path+"/section_id", "unknown_section", *n.SectionID, map[string]any{"planned_section_ids": sectionIDs, "group_nodes_may_use_null": true})
			}
		}
		if len(roots) != 1 || !ids[m.RootID] || len(roots) == 1 && roots[0] != m.RootID {
			add("/mind_map", "invalid_root", map[string]any{"root_id": m.RootID, "parentless_node_ids": roots}, "exactly one node has parent_id=null and its ID equals root_id; all other nodes have a real parent_id")
		}
		for i, n := range m.Nodes {
			path := fmt.Sprintf("/mind_map/nodes/%d", i)
			if n.ParentID != nil && !ids[*n.ParentID] {
				add(path+"/parent_id", "unknown_parent", *n.ParentID, "an existing node ID; the root uses null, not an empty string")
			}
			if utf8.RuneCountInString(n.Label) > 100 {
				add(path+"/label", "label_too_long", utf8.RuneCountInString(n.Label), map[string]int{"max_characters": 100})
			}
			if !parents[n.ID] && n.ID != m.RootID && n.SectionID == nil {
				add(path+"/section_id", "missing_leaf_section", nil, map[string]any{"planned_section_ids": sectionIDs})
			}
			// The invalid tree may contain a cycle or missing parents. Never
			// assume the publication validator accepted it while building feedback.
			visited := map[string]bool{}
			at, depth := n, 0
			for at.ParentID != nil {
				if visited[at.ID] {
					add(path+"/parent_id", "parent_cycle", *n.ParentID, "a parent chain that reaches root_id without revisiting a node")
					break
				}
				visited[at.ID] = true
				next, found := byID[*at.ParentID]
				if !found {
					break
				}
				depth++
				at = next
			}
			if depth > 4 && at.ParentID == nil {
				add(path+"/parent_id", "depth_exceeded", depth, "at most 4 parent edges to root_id; reparent or merge this overly deep branch")
			}
		}
		if len(m.Nodes) > 80 {
			add("/mind_map/nodes", "node_count_exceeded", len(m.Nodes), map[string]int{"max": 80})
		}
	}
	if len(issues) == 0 {
		add("/mind_map", "invalid_concept_tree", cause.Error(), "an acyclic connected tree, at most 80 nodes and 4 branch levels; every leaf links to a planned section")
	}
	return &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "概念导图未通过校验，请按字段路径修正并保留已有主题。", Meta: map[string]any{"code": "concept_map_invalid", "issues": issues[:min(64, len(issues))], "issue_count": len(issues), "issues_truncated": len(issues) > 64, "repairable": true}}
}
