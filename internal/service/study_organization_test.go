package service

import (
	"testing"
	"vid-lens/internal/artifact"
)

func TestStudyIndexPlanPreservesChaptersAndDistinctConcepts(t *testing.T) {
	parent := "chapter"
	otherParent := "other-chapter"
	blocks := []artifact.Block{
		{BlockID: parent, Type: "section", Title: "训练方法"},
		{BlockID: "a", ParentID: &parent, Type: "concept", Title: "RLCD"},
		{BlockID: "b", ParentID: &parent, Type: "concept", Title: "RLHF"},
		{BlockID: "c", ParentID: &parent, Type: "concept", Title: "RLCD"},
		{BlockID: otherParent, Type: "section", Title: "工程应用"},
		{BlockID: "d", ParentID: &otherParent, Type: "concept", Title: "RLCD"},
		{BlockID: "e", ParentID: &parent, Type: "example", Title: "RLCD"},
	}
	for _, merged := range [][]int{{1, 2, 3, 4, 5, 6, 7}, {2, 3}, {2, 6}, {2, 7}, {1, 5}} {
		plan := studyIndexPlan{Groups: []studyIndexGroup{{BlockIndices: merged, TitleFrom: merged[0]}}}
		for i := range blocks {
			included := false
			for _, n := range merged {
				included = included || n == i+1
			}
			if !included {
				plan.Groups = append(plan.Groups, studyIndexGroup{BlockIndices: []int{i + 1}, TitleFrom: i + 1})
			}
		}
		if _, err := indexStudyPlan(plan, blocks); err == nil {
			t.Fatalf("accepted loss of chapter/concept structure: %+v", merged)
		}
	}
	plan := studyIndexPlan{Groups: []studyIndexGroup{{BlockIndices: []int{2, 4}, TitleFrom: 2}}}
	for _, n := range []int{1, 3, 5, 6, 7} {
		plan.Groups = append(plan.Groups, studyIndexGroup{BlockIndices: []int{n}, TitleFrom: n})
	}
	if _, err := indexStudyPlan(plan, blocks); err != nil {
		t.Fatalf("matching leaf concepts in the same chapter should be mergeable: %v", err)
	}
}

func TestStudyIndexPlanCannotInventOmitDuplicateOrMergeOpposingBlocks(t *testing.T) {
	blocks := []artifact.Block{
		{BlockID: "a", Title: "提交", EvidenceRefs: []artifact.Ref{{EvidenceID: "e1", Relation: "supports"}}},
		{BlockID: "b", Title: "相反观点", EvidenceRefs: []artifact.Ref{{EvidenceID: "e2", Relation: "contradicts"}}},
	}
	for _, plan := range []studyIndexPlan{
		{Groups: []studyIndexGroup{{BlockIndices: []int{1}, TitleFrom: 1}}},
		{Groups: []studyIndexGroup{{BlockIndices: []int{1, 1}, TitleFrom: 1}, {BlockIndices: []int{2}, TitleFrom: 2}}},
		{Groups: []studyIndexGroup{{BlockIndices: []int{1, 3}, TitleFrom: 1}}},
		{Groups: []studyIndexGroup{{BlockIndices: []int{1}, TitleFrom: 2}, {BlockIndices: []int{2}, TitleFrom: 2}}},
		{Groups: []studyIndexGroup{{BlockIndices: []int{1, 2}, TitleFrom: 1}}},
	} {
		if _, err := indexStudyPlan(plan, blocks); err == nil {
			t.Fatalf("accepted invalid indices: %+v", plan)
		}
	}
	plan, err := indexStudyPlan(studyIndexPlan{Groups: []studyIndexGroup{{BlockIndices: []int{2}, TitleFrom: 2}, {BlockIndices: []int{1}, TitleFrom: 1}}}, blocks)
	if err != nil || plan.Groups[0].OldBlockIDs[0] != "b" || plan.Groups[0].Title != "相反观点" {
		t.Fatalf("canonical mapping failed: %+v %v", plan, err)
	}
}
