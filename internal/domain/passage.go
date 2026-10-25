package domain

import (
	"strings"
	"unicode/utf8"
)

// NormalizePassages preserves the legacy single-passage wire shape. Both forms
// are normalized before idempotency hashing and persisted without losing anchors.
func (c *QuestionContext) NormalizePassages() {
	if c.Passage != nil {
		c.Passage.Quote = NormalizeQuote(c.Passage.Quote)
	}
	for i := range c.Passages {
		c.Passages[i].Quote = NormalizeQuote(c.Passages[i].Quote)
	}
}

func (c QuestionContext) SelectedPassages() []Passage {
	if len(c.Passages) > 0 {
		return c.Passages
	}
	if c.Passage != nil {
		return []Passage{*c.Passage}
	}
	return nil
}

// ValidateQuestionPassages checks one continuous selection against the immutable
// article. Partial edges are allowed; interior blocks must be quoted in full.
func ValidateQuestionPassages(article Article, c QuestionContext, maxChars int) error {
	invalid := func(message string) error { return Err(422, "INVALID_CONTEXT", message) }
	if c.Passage != nil && len(c.Passages) > 0 {
		return invalid("选段引用格式冲突，请重新选择。")
	}
	parts := c.SelectedPassages()
	if len(parts) == 0 {
		return nil
	}
	// Each nonempty part consumes at least one character; bound hostile arrays
	// before building the article index. Separators count toward the same limit.
	if len(parts) > maxChars {
		return invalid("选中文字合计超过长度限制。")
	}
	total := len(parts) - 1
	for _, part := range parts {
		quote := NormalizeQuote(part.Quote)
		if quote == "" {
			return invalid("选段为空或过长。")
		}
		total += utf8.RuneCountInString(quote)
		if total > maxChars {
			return invalid("选中文字合计超过长度限制。")
		}
	}
	type blockText struct {
		sectionID, blockID, text string
	}
	var blocks []blockText
	for _, section := range article.Sections {
		for _, block := range section.Blocks {
			if text := NormalizeQuote(BlockText(block)); text != "" {
				blocks = append(blocks, blockText{section.ID, block.ID, text})
			}
		}
	}
	previous := -1
	for i, part := range parts {
		position := -1
		for index, block := range blocks {
			if block.sectionID == part.SectionID && block.blockID == part.BlockID {
				position = index
				break
			}
		}
		if position < 0 || (i > 0 && position != previous+1) {
			return invalid("选段不属于这篇文章或顺序不连续。")
		}
		text, quote := blocks[position].text, NormalizeQuote(part.Quote)
		if !strings.Contains(text, quote) {
			return invalid("选中文字对不上文章原文。")
		}
		if len(parts) > 1 {
			matches := text == quote
			if i == 0 {
				matches = strings.HasSuffix(text, quote)
			} else if i == len(parts)-1 {
				matches = strings.HasPrefix(text, quote)
			}
			if !matches {
				return invalid("选中文字不是连续的原文，请重新选择。")
			}
		}
		previous = position
	}
	return nil
}
