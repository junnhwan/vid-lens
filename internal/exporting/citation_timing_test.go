package exporting

import (
	"bytes"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func timingArticle(origin string) domain.Article {
	return domain.Article{
		Title:  "引用时间",
		Source: domain.Source{CanonicalURL: "https://example.com/watch?v=video"},
		Sections: []domain.Section{{ID: "section", Title: "正文", Blocks: []domain.Block{{
			ID: "body", Type: "paragraph", Origin: "video",
			Content: []domain.Inline{{Type: "text", Text: "保留正文。"}, {Type: "citation", CitationID: "text"}},
		}}}},
		Citations: map[string]domain.Citation{"text": {
			ID: "text", Kind: "video_text", Label: "01:00–02:00", TextOrigin: origin,
			Range: &domain.Range{StartMS: 60_000, EndMS: 120_000}, Quote: domain.Ptr("原始转写文字"),
		}},
	}
}

func TestMarkdownUntimedTextRetainsSourceAndQuote(t *testing.T) {
	for _, origin := range []string{"asr", "mixed", ""} {
		t.Run("origin="+origin, func(t *testing.T) {
			a := timingArticle(origin)
			before := domain.JSON(a)
			md, err := Markdown(a, domain.GenerationModel{}, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			text := string(md)
			for _, want := range []string{"保留正文。[^source-1]", "[视频原文](<https://example.com/watch?v=video>)", "原始转写文字"} {
				if !strings.Contains(text, want) {
					t.Fatalf("source or content missing %q:\n%s", want, text)
				}
			}
			for _, unwanted := range []string{"01:00", "02:00", "t=60", "（至"} {
				if strings.Contains(text, unwanted) {
					t.Fatalf("unverified text time %q exposed:\n%s", unwanted, text)
				}
			}
			if !bytes.Equal(before, domain.JSON(a)) {
				t.Fatal("rendering changed the immutable article")
			}
		})
	}
}

func TestMarkdownKeepsSubtitleAndFrameTimes(t *testing.T) {
	for _, origin := range []string{"platform_subtitles", "platform_subtitle"} {
		t.Run(origin, func(t *testing.T) {
			a := timingArticle(origin)
			a.Citations["frame"] = domain.Citation{ID: "frame", Kind: "video_frame", Label: "真实画面", TimeMS: domain.Ptr(int64(75_000))}
			md, err := Markdown(a, domain.GenerationModel{}, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"t=60", "（至 02:00）", "真实画面 · 01:15", "t=75"} {
				if !strings.Contains(string(md), want) {
					t.Fatalf("trusted time missing %q:\n%s", want, md)
				}
			}
		})
	}
}

func TestLegacyCitationRenderingRemainsAvailable(t *testing.T) {
	for _, version := range []int{1, 2} {
		md, err := markdownVersion(timingArticle("asr"), domain.GenerationModel{}, "", nil, version)
		if err != nil || !strings.Contains(string(md), "t=60") || !strings.Contains(string(md), "（至 02:00）") {
			t.Fatalf("legacy renderer %d changed: %s %v", version, md, err)
		}
	}
}
