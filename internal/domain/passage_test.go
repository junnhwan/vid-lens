package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func passageArticle() Article {
	return Article{ID: "article", Sections: []Section{
		{ID: "s1", Blocks: []Block{{ID: "a", Type: "paragraph", Content: []Inline{{Type: "text", Text: "第一段。"}}}, {ID: "b", Type: "paragraph", Content: []Inline{{Type: "text", Text: "第二段。"}}}}},
		{ID: "s2", Blocks: []Block{{ID: "c", Type: "paragraph", Content: []Inline{{Type: "text", Text: "第三段。"}}}}},
	}}
}

func TestQuestionPassagesValidateContinuousQuotesAndTotalLimit(t *testing.T) {
	article := passageArticle()
	parts := []Passage{{SectionID: "s1", BlockID: "a", Quote: "一段。"}, {SectionID: "s1", BlockID: "b", Quote: "第二段。"}, {SectionID: "s2", BlockID: "c", Quote: "第三"}}
	if err := ValidateQuestionPassages(article, QuestionContext{Passages: parts}, 11); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuestionPassages(article, QuestionContext{Passages: parts}, 10); err == nil {
		t.Fatal("combined length escaped the total limit")
	}
	for _, tc := range []struct {
		name  string
		parts []Passage
	}{
		{"forged quote", []Passage{parts[0], {SectionID: "s1", BlockID: "b", Quote: "不存在的字句"}}},
		{"wrong section", []Passage{parts[0], {SectionID: "foreign", BlockID: "b", Quote: "第二段。"}}},
		{"wrong block", []Passage{parts[0], {SectionID: "s1", BlockID: "foreign", Quote: "第二段。"}}},
		{"duplicate", []Passage{parts[0], parts[0]}},
		{"reversed", []Passage{parts[1], parts[0]}},
		{"skipped", []Passage{parts[0], parts[2]}},
		{"empty quote", []Passage{parts[0], {SectionID: "s1", BlockID: "b", Quote: " \t "}}},
		{"first edge gap", []Passage{{SectionID: "s1", BlockID: "a", Quote: "第一"}, parts[1]}},
		{"middle gap", []Passage{parts[0], {SectionID: "s1", BlockID: "b", Quote: "第二"}, parts[2]}},
		{"last edge gap", []Passage{parts[0], {SectionID: "s1", BlockID: "b", Quote: "二段。"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateQuestionPassages(article, QuestionContext{Passages: tc.parts}, 1000); err == nil {
				t.Fatal("invalid selection accepted")
			}
		})
	}
}

func TestQuestionChapterMustBelongToArticle(t *testing.T) {
	article := passageArticle()
	if err := ValidateQuestionPassages(article, QuestionContext{SectionID: "s1"}, 1000); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuestionPassages(article, QuestionContext{SectionID: "foreign"}, 1000); err == nil {
		t.Fatal("foreign chapter accepted without a selection")
	}
}

func TestQuestionPassagesNormalizeUnicodeAndPreserveLegacyJSON(t *testing.T) {
	article := passageArticle()
	article.Sections[0].Blocks[0].Content[0].Text = "甲😀\u00a0乙"
	article.Sections[0].Blocks[1].Content[0].Text = "丙😀"
	c := QuestionContext{Passages: []Passage{{SectionID: "s1", BlockID: "a", Quote: "甲😀\t 乙"}, {SectionID: "s1", BlockID: "b", Quote: "丙😀"}}}
	c.NormalizePassages()
	if c.Passages[0].Quote != "甲😀 乙" || ValidateQuestionPassages(article, c, 7) != nil || ValidateQuestionPassages(article, c, 6) == nil {
		t.Fatal("Unicode whitespace or rune accounting changed")
	}
	legacy := QuestionContext{Passage: &Passage{SectionID: "s1", BlockID: "a", Quote: "甲😀 乙"}}
	if ValidateQuestionPassages(article, legacy, 4) != nil || len(legacy.SelectedPassages()) != 1 {
		t.Fatal("legacy single-block selection rejected")
	}
	encoded := string(JSON(legacy))
	if strings.Contains(encoded, "passages") || encoded != `{"frame_id":null,"passage":{"section_id":"s1","block_id":"a","quote":"甲😀 乙"}}` {
		t.Fatalf("legacy idempotency wire shape changed: %s", encoded)
	}
	if ValidateQuestionPassages(article, QuestionContext{Passage: legacy.Passage, Passages: c.Passages}, 1000) == nil {
		t.Fatal("ambiguous legacy and multi-part forms accepted together")
	}
	var restored QuestionContext
	if json.Unmarshal(JSON(c), &restored) != nil || len(restored.Passages) != 2 || ValidateQuestionPassages(article, restored, 7) != nil {
		t.Fatal("multi-part context did not round-trip")
	}
	if ValidateQuestionPassages(article, QuestionContext{}, 1000) != nil {
		t.Fatal("plain question requires a selection")
	}
}
