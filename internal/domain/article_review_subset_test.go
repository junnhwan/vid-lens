package domain

import (
	"slices"
	"testing"
)

func TestArticleReviewSubsetPreservesBlockingAndScopeBoundaries(t *testing.T) {
	a := Article{Sections: []Section{
		{ID: "chapter_a", Blocks: []Block{{ID: "block_a"}}},
		{ID: "chapter_b", Blocks: []Block{{ID: "block_b"}}},
	}}
	text := []Segment{{ID: "original_a"}, {ID: "original_b"}}
	parent := ArticleReviewScope{Global: true, SectionIDs: []string{"chapter_a", "chapter_b"}, SourceIDs: []string{"original_a", "original_b"}, FrameIDs: []string{"pixel_a", "pixel_b"}}
	child := ArticleReviewScope{SectionIDs: []string{"chapter_a"}, SourceIDs: []string{"original_a"}, FrameIDs: []string{"pixel_a"}}
	report := ArticleReviewReport{Complete: true, Topics: []ArticleTopic{
		{Topic: "第一章", SectionIDs: []string{"chapter_a"}, SourceIDs: []string{"t1"}},
		{Topic: "第二章", SectionIDs: []string{"chapter_b"}, SourceIDs: []string{"t2"}},
	}, Issues: []ArticleReviewIssue{
		{Severity: "blocking", AC: "AC03", Location: "block_b（第二章）", EvidenceIDs: []string{"t2"}, Problem: "第二章实质缺陷"},
		{Severity: "advisory", AC: "AC06", Location: "block_a", EvidenceIDs: []string{"t1"}, Problem: "措辞建议"},
		{Severity: "advisory", AC: "AC06", Location: "block_b", EvidenceIDs: []string{"t2"}, Problem: "其它章建议"},
	}}
	if !ArticleReviewScopeContains(parent, child) {
		t.Fatal("完整核验没有覆盖实际子集")
	}
	projected, ok := ArticleReviewSubset(report, a, child, text)
	if !ok || !projected.Complete || len(projected.Topics) != 1 || len(projected.Issues) != 1 || projected.Issues[0].Location != "block_a" || !slices.Equal(projected.Topics[0].SourceIDs, []string{"t1"}) {
		t.Fatalf("未改章节的结论没有严格限制到子集：%+v %v", projected, ok)
	}
	for _, issue := range []ArticleReviewIssue{
		{Severity: "blocking", AC: "AC03", Location: "block_a"},
		{Severity: "blocking", AC: "AC03", Location: "chapter_a"},
		{Severity: "blocking", AC: "AC03", Location: "block_a、block_b"},
		{Severity: "blocking", AC: "AC02", Location: "无法定位的遗漏"},
		{Severity: "blocking", AC: "AC06", Location: "block_b", Problem: "全文关系存在实质矛盾"},
	} {
		changed := report
		changed.Issues = []ArticleReviewIssue{issue}
		if _, ok := ArticleReviewSubset(changed, a, child, text); ok {
			t.Fatalf("受影响或无法定位的问题继承批准：%+v", issue)
		}
	}
	for _, changed := range []ArticleReviewReport{
		{Complete: false, Topics: report.Topics},
		{Complete: true, Unassessed: []string{"未核对"}, Topics: report.Topics},
		{Complete: true, Topics: report.Topics[1:]},
	} {
		if _, ok := ArticleReviewSubset(changed, a, child, text); ok {
			t.Fatal("不完整核验或无局部主题映射被当成局部批准")
		}
	}
	observations := []ArticleVisualObservation{{FrameID: "pixel_a", VisibleFacts: []string{"真实像素A"}}, {FrameID: "pixel_b", VisibleFacts: []string{"真实像素B"}}}
	if got := ArticleReviewSubsetObservations(observations, child); len(got) != 1 || got[0].FrameID != "pixel_a" {
		t.Fatal("子集复用没有严格限制实际像素观察", got)
	}
}

func TestArticleReviewScopeSubsetCannotInventEvidenceOrGlobalApproval(t *testing.T) {
	parent := ArticleReviewScope{SectionIDs: []string{"a"}, SourceIDs: []string{"one"}, ContextSourceIDs: []string{"two"}, FrameIDs: []string{"pixel"}}
	for _, child := range []ArticleReviewScope{
		{Global: true, SectionIDs: []string{"a"}, SourceIDs: []string{"one"}},
		{SectionIDs: []string{"b"}, SourceIDs: []string{"one"}},
		{SectionIDs: []string{"a"}, SourceIDs: []string{"two"}},
		{SectionIDs: []string{"a"}, SourceIDs: []string{"one"}, ContextSourceIDs: []string{"three"}},
		{SectionIDs: []string{"a"}, SourceIDs: []string{"one"}, FrameIDs: []string{"other_pixel"}},
	} {
		if ArticleReviewScopeContains(parent, child) {
			t.Fatal("上下文冒充覆盖、缺少材料或旧全文批准被复用", child)
		}
	}
	child := ArticleReviewScope{SectionIDs: []string{"a"}, SourceIDs: []string{"one"}, ContextSourceIDs: []string{"two"}, FrameIDs: []string{"pixel"}}
	if !ArticleReviewScopeContains(parent, child) {
		t.Fatal("已交付的相同补充上下文不能复用")
	}
	parent.Global, parent.SourceIDs = true, nil
	if ArticleReviewScopeContains(parent, child) {
		t.Fatal("只有全文关系核对的记录变成了事实批准")
	}
}
