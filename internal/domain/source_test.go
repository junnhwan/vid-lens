package domain

import "testing"

func TestSourcePartsAndBoundaries(t *testing.T) {
	a, e := ParseSource("https://www.bilibili.com/video/BV1xx411c7mD?p=2&share_source=x", false)
	if e != nil || a.PartKey != "2" || a.URL != "https://www.bilibili.com/video/BV1xx411c7mD?p=2" {
		t.Fatalf("%+v %v", a, e)
	}
	b, _ := ParseSource("https://www.bilibili.com/video/BV1xx411c7mD?p=1", false)
	if a == b {
		t.Fatal("parts collapsed")
	}
	for _, raw := range []string{"https://www.bilibili.com.evil.test/video/BV1xx411c7mD", "http://127.0.0.1/video/BV1xx411c7mD", "https://user@youtube.com/watch?v=abcdefghijk", "https://youtube.com/playlist?list=x", "local://sample.mp4"} {
		if _, e := ParseSource(raw, false); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
func TestSelectionWhitespace(t *testing.T) {
	b := Block{Type: "paragraph", Content: []Inline{{Type: "text", Text: "first\u00a0"}, {Type: "citation", CitationID: "x"}, {Type: "link", Text: "second"}, {Type: "text", Text: "\n  third"}}}
	if s := NormalizeQuote(BlockText(b)); s != "first second third" {
		t.Fatal(s)
	}
}
