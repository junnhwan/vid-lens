package domain

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestArticleReviewDeltaBindsEveryChangeAndOriginalBlocking(t *testing.T) {
	before := Article{SnapshotID: "source", Title: "合成文章", Sections: []Section{
		{ID: "a", Title: "第一章", Blocks: []Block{{ID: "a_one", Type: "paragraph", Content: []Inline{{Type: "text", Text: "已有的名称与事实"}, {Type: "citation", CitationID: "ref_a"}}}}},
		{ID: "b", Title: "第二章", Blocks: []Block{{ID: "b_one", Type: "paragraph", Content: []Inline{{Type: "text", Text: "需要修正的数字"}}}, {ID: "b_two", Type: "paragraph", Content: []Inline{{Type: "text", Text: "未变的另一项事实"}}}}},
	}, Citations: map[string]Citation{"ref_a": {ID: "ref_a", Kind: "video_text", SegmentIDs: []string{"original_a"}}}}
	parent := ArticleReviewRecord{CandidateHash: ArticleHash(before), Scope: ArticleReviewScope{Global: true, SectionIDs: []string{"a", "b"}, SourceIDs: []string{"original_a", "original_b"}}, Report: ArticleReviewReport{Complete: true, Topics: []ArticleTopic{{Topic: "两个主要主题", SectionIDs: []string{"a", "b"}, SourceIDs: []string{"t1", "t2"}}}, Issues: []ArticleReviewIssue{{Severity: "blocking", AC: "AC03", Location: "b_one", Problem: "数字错误"}}}}
	clone := func() Article { var a Article; _ = json.Unmarshal(JSON(before), &a); return a }
	after := clone()
	after.Sections[1].Blocks[0].Content[0].Text = "已修正的数字"
	delta, ok := ArticleReviewDeltaFor("review", parent, before, after)
	if !ok || delta.HeaderChanged || !slices.Equal(delta.AffectedBlockIDs, []string{"b_one"}) || !slices.Equal(delta.ChangedSectionIDs, []string{"b"}) || delta.BaseReviewHash != ContentHash(parent) {
		t.Fatalf("未变内容被重新核验或变化内容被漏掉：%+v %v", delta, ok)
	}
	if delta.ContainsIssue(ArticleReviewIssue{Severity: "blocking", AC: "AC04", Location: "b_two"}) || !delta.ContainsIssue(ArticleReviewIssue{Severity: "blocking", AC: "AC03", Location: "b_one"}) || !delta.ContainsIssue(ArticleReviewIssue{Severity: "blocking", AC: "AC02", Location: "b"}) {
		t.Fatal("差异事实复核与当前覆盖/关系边界错误")
	}
	// 改的是别处，原阻断仍必须核对；不能把新稿版本当成问题已经关闭。
	after = clone()
	after.Sections[0].Blocks[0].Content[0].Text = "第一章改了其它内容"
	delta, _ = ArticleReviewDeltaFor("review", parent, before, after)
	if !slices.Contains(delta.AffectedBlockIDs, "a_one") || !slices.Contains(delta.AffectedBlockIDs, "b_one") {
		t.Fatal("未改的原阻断被遗漏", delta)
	}
	// 字面没有变，引用绑定变化同样是新的事实依赖。
	after = clone()
	ref := after.Citations["ref_a"]
	ref.SegmentIDs = []string{"original_b"}
	after.Citations["ref_a"] = ref
	delta, _ = ArticleReviewDeltaFor("review", parent, before, after)
	if !slices.Contains(delta.AffectedBlockIDs, "a_one") {
		t.Fatal("换引用继承了旧事实核验")
	}
	after = clone()
	after.Sections[1].Blocks = after.Sections[1].Blocks[:1]
	delta, _ = ArticleReviewDeltaFor("review", parent, before, after)
	if !slices.Contains(delta.RemovedBlockIDs, "b_two") {
		t.Fatal("删除内容没有保留覆盖检查义务")
	}
	for _, location := range []string{"全文", "无法定位的主题"} {
		bad := parent
		bad.Report.Issues = []ArticleReviewIssue{{Severity: "blocking", AC: "AC02", Location: location}}
		if _, ok := ArticleReviewDeltaFor("review", bad, before, after); ok {
			t.Fatal("全局/未知阻断被当成局部差异", location)
		}
	}
	parent.Delta = delta
	if _, ok := ArticleReviewDeltaFor("review", parent, before, after); ok {
		t.Fatal("差异批准被递归当成原始完整核验")
	}
}
