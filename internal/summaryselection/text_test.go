package summaryselection

import "testing"

func TestCanonicalUnicodeMarkdownText(t *testing.T) {
	input := "# 配置😀\n\n**中文** [链接](https://example.test) 和 `&amp; <T>`。\\*星号\\*\n\n> 引文\n\n```go\nmap[string]any <T>\n```"
	expected := "配置😀\n中文 链接 和 &amp; <T>。*星号*\n引文\nmap[string]any <T>"
	text := Text(input)
	if text != expected {
		t.Fatalf("text=%q", text)
	}
	runes := []rune(text)
	if string(runes[2:3]) != "😀" {
		t.Fatal("emoji offset uses UTF16")
	}
}
