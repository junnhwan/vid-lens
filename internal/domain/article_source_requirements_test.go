package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestArticleSourceWorkBatchKeepsWholeSegmentsAndBoundaryContext(t *testing.T) {
	text := []Segment{{ID: "a", Text: strings.Repeat("甲", 8000)}, {ID: "b", Text: strings.Repeat("乙", 8000)}, {ID: "c", Text: "末段必要条件"}}
	if got := ArticleSourceWorkBatch(text, []string{"t1", "t2", "t3"}); !slices.Equal(got, []string{"t1", "t2"}) {
		t.Fatal(got)
	}
	if got := ArticleSourceBoundaryRefs(text, []string{"t2"}); !slices.Equal(got, []string{"t1", "t3"}) {
		t.Fatal(got)
	}
	if got := ArticleSourceWorkBatch(text, []string{"t3"}); !slices.Equal(got, []string{"t3"}) {
		t.Fatal("ending dropped", got)
	}
	text[0].Text = strings.Repeat("整", ArticleSourceWorkRunes+1)
	if len(ArticleSourceWorkBatch(text, []string{"t1", "t2"})) != 0 {
		t.Fatal("oversized original was clipped or skipped")
	}
}

func TestArticleSourceInventoryCannotSkipEndingOrInventReferences(t *testing.T) {
	text := []Segment{{ID: "first", Text: "先检查环境，再执行任务。"}, {ID: "last", Text: "最后验证结果，只适用于可验证的任务。"}}
	refs := []string{"t1", "t2"}
	dir := ArticleEvidenceDirectory("snapshot", text, refs)
	good := ArticleSourceRequirements{Complete: true, Requirements: []ArticleSourceRequirement{{Requirement: "环境检查、执行及末尾验证和适用限制", EvidenceRefs: refs}}, Incidental: []ArticleIncidentalSource{}}
	if err := ValidateArticleSourceRequirements(good, refs, dir); err != nil {
		t.Fatal(err)
	}
	for _, badRefs := range [][]string{{"t1"}, {"t1", "t999"}, {"t1", "t1", "t2"}} {
		bad := good
		bad.Requirements = []ArticleSourceRequirement{{Requirement: "不完整的覆盖", EvidenceRefs: badRefs}}
		if ValidateArticleSourceRequirements(bad, refs, dir) == nil {
			t.Fatal("skipped or fabricated source admitted", badRefs)
		}
	}
	missingArray := good
	missingArray.Incidental = nil
	if ValidateArticleSourceRequirements(missingArray, refs, dir) == nil {
		t.Fatal("missing required array admitted; original reply would differ at publish")
	}
	good.Requirements = []ArticleSourceRequirement{}
	good.Incidental = []ArticleIncidentalSource{{Reason: "此合成范围仅为寒暄重复，语义判断另行校准", EvidenceRefs: refs}}
	if err := ValidateArticleSourceRequirements(good, refs, dir); err != nil {
		t.Fatal(err)
	}
}

func TestArticleSourceMajorUnitLimitIsVersionedAndPreservesDetails(t *testing.T) {
	text := []Segment{{ID: "original", Text: "环境、进入条件、排除项、执行、验证及失败分支"}}
	refs := []string{"t1"}
	dir := ArticleEvidenceDirectory("snapshot", text, refs)
	r := ArticleSourceRequirements{Complete: true, Requirements: []ArticleSourceRequirement{}, Incidental: []ArticleIncidentalSource{}}
	for range 7 {
		r.Requirements = append(r.Requirements, ArticleSourceRequirement{Requirement: text[0].Text, EvidenceRefs: refs})
	}
	if ValidateArticleSourceRequirementsForRevision(r, refs, dir, ArticleConvergenceReviewV11) == nil {
		t.Fatal("new source work still admits per-segment inventories without bound")
	}
	if err := ValidateArticleSourceRequirementsForRevision(r, refs, dir, ArticleConvergenceReviewV10); err != nil {
		t.Fatal("old admitted plan changed", err)
	}
	r.Requirements = r.Requirements[:1]
	if err := ValidateArticleSourceRequirementsForRevision(r, refs, dir, ArticleConvergenceReviewV11); err != nil {
		t.Fatal("coherent process lost its necessary details", err)
	}
}
