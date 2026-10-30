package domain

import (
	"strings"
	"testing"
)

func TestArticleReadingPreservesContentAndIdentityWithoutCitationMetadata(t *testing.T) {
	a := Article{ID: "article", Title: "完整标题", Sections: []Section{{ID: "section", Title: "原样章节", Blocks: []Block{
		{ID: "text", Type: "paragraph", Origin: "video", Content: []Inline{{Type: "text", Text: "完整文字，包括尚未完成的句段"}, {Type: "citation", CitationID: "source"}}},
		{ID: "quote", Type: "quote", Content: []Inline{{Type: "text", Text: "原样引述"}}},
		{ID: "code", Type: "code", Text: "x := 1\nprint(x)", Language: Ptr("go")},
		{ID: "list", Type: "list", Ordered: Ptr(true), Items: []ListItem{{ID: "item", Content: []Inline{{Type: "text", Text: "原样列表项"}}}}},
		{ID: "table", Type: "table", Columns: []string{"列一", "列二"}, Rows: []TableRow{{Cells: []string{"值一", "值二"}}}},
		{ID: "figure", Type: "figure", FrameIDs: []string{"pixels"}, Caption: "原样图注"},
	}}}, Citations: map[string]Citation{"source": {ID: "source", ContentSHA256: "metadata-must-not-repeat"}}, Limitations: []Limitation{{Message: "有作用域的局限", SectionIDs: []string{"section"}}}}
	view := ReadingArticle(a)
	raw := string(JSON(view))
	for _, value := range []string{"article", "section", "text", "quote", "code", "list", "item", "table", "figure", "pixels", "完整文字，包括尚未完成的句段", "原样引述", "原样列表项", "值一", "值二", "原样图注", "有作用域的局限"} {
		if !strings.Contains(raw, value) {
			t.Fatalf("reading view lost %q", value)
		}
	}
	if view.Sections[0].Blocks[2].Text != a.Sections[0].Blocks[2].Text {
		t.Fatal("code changed")
	}
	if strings.Contains(raw, "metadata-must-not-repeat") {
		t.Fatal("reading view retained repeated citation metadata")
	}
	if QuestionInputRevision(ArticleLoopV2) != QuestionReadingV7 || QuestionInputRevision(ArticleLoopV1) != QuestionReadingV7 || QuestionInputRevision(ExecutionContractV12) != QuestionReadingV7 {
		t.Fatal("new question input depends on the historical article writer")
	}
}
