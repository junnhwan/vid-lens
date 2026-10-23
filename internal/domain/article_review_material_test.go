package domain

import (
	"encoding/json"
	"testing"
)

func TestArticleReviewMaterialIdentityIncludesEveryLocalDependency(t *testing.T) {
	a := Article{SnapshotID: "snapshot", Title: "文章", Source: Source{SourceID: "source"}, Sections: []Section{
		{ID: "a", Title: "第一章", Blocks: []Block{{ID: "a_b1", Type: "paragraph", Origin: "video", Content: []Inline{{Type: "text", Text: "第一项事实"}, {Type: "citation", CitationID: "t1"}}}}},
		{ID: "b", Title: "第二章", Blocks: []Block{{ID: "b_b1", Type: "paragraph", Origin: "video", Content: []Inline{{Type: "text", Text: "第二项事实"}, {Type: "citation", CitationID: "t2"}}}}},
	}, Citations: map[string]Citation{"t1": {ID: "t1", Kind: "video_text", SegmentIDs: []string{"original-a"}}, "t2": {ID: "t2", Kind: "video_text", SegmentIDs: []string{"original-b"}}}}
	scope := ArticleReviewScope{ID: "scope-a", SectionIDs: []string{"a"}, SourceIDs: []string{"original-a"}, FrameIDs: []string{"pixel"}}
	snap := Snapshot{ID: "snapshot", Source: a.Source}
	text := []Segment{{ID: "original-a", Text: "第一项事实"}, {ID: "original-b", Text: "第二项事实"}}
	assets := []Asset{{ID: "pixel", SHA256: "first-sha"}}
	identity := ArticleReviewMaterialIdentity(a, scope, snap, text, assets)
	clone := func() Article { var out Article; _ = json.Unmarshal(JSON(a), &out); return out }
	other := clone()
	other.Sections[1].Blocks[0].Content[0].Text = "第二章已修正"
	if ArticleReviewMaterialIdentity(other, scope, snap, text, assets) != identity {
		t.Fatal("未改章节的局部材料无法复用")
	}
	for _, change := range []func(*Article){
		func(v *Article) { v.Sections[0].Blocks[0].Content[0].Text = "实质变化" },
		func(v *Article) { c := v.Citations["t1"]; c.SegmentIDs = []string{"original-b"}; v.Citations["t1"] = c },
		func(v *Article) { v.Sections[1].Title = "结构导航变化" },
		func(v *Article) { v.Title = "文章标题变化" },
		func(v *Article) {
			v.Limitations = []Limitation{{Code: "partial_text", Message: "新的局限", SectionIDs: []string{"a"}}}
		},
	} {
		v := clone()
		change(&v)
		if ArticleReviewMaterialIdentity(v, scope, snap, text, assets) == identity {
			t.Fatal("变化后的正文、引用、标题或局限继承旧核验")
		}
	}
	assets[0].SHA256 = "changed-sha"
	if ArticleReviewMaterialIdentity(a, scope, snap, text, assets) == identity {
		t.Fatal("像素变化继承旧核验")
	}
	assets[0].SHA256 = "first-sha"
	text[0].Text = "变化后的原文"
	if ArticleReviewMaterialIdentity(a, scope, snap, text, assets) == identity {
		t.Fatal("原文变化继承旧核验")
	}
	if ArticleReviewPolicyIdentity(GenerationSpec{}, ArticleLoopV2, "") != ArticlePolicyIdentityForContract(GenerationSpec{}, ArticleLoopV2) || ArticleReviewPolicyIdentity(GenerationSpec{}, ArticleLoopV2, ArticleConvergenceReviewV1) == ArticlePolicyIdentityForContract(GenerationSpec{}, ArticleLoopV2) {
		t.Fatal("复核修订破坏旧策略身份或复用了全文批准")
	}
}
