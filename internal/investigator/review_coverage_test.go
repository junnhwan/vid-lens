package investigator

import (
	"testing"

	"vidlens/internal/domain"
)

func TestReviewRecoveryApprovalsBindContentAndPrimaryEvidence(t *testing.T) {
	s := &session{}
	candidate := map[string]any{"title": "来源标题", "sections": []domain.Section{{ID: "one", Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "已确认事实。"}}, CitationIDs: []string{"t1"}}}}}}
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text", SegmentIDs: []string{"segment-one"}}}
	batch := evidenceReviewBatch{Mode: "source_text"}
	keys := s.reviewApprovalKeys(candidate, catalog, batch)
	path := "/sections/0/blocks/0/content/0/text"
	candidate["title"] = "改变了整篇的范围标题"
	next := s.reviewApprovalKeys(candidate, catalog, batch)
	if next[path] == keys[path] || next["/title"] == keys["/title"] {
		t.Fatal("a changed article title reused an approval for a different semantic scope")
	}
	candidate["title"] = "来源标题"
	blocks := candidate["sections"].([]domain.Section)
	blocks[0].Blocks[0].Content[0].Text = "这句话已经改变。"
	if s.reviewApprovalKeys(candidate, catalog, batch)[path] == keys[path] {
		t.Fatal("a changed claim reused its old approval")
	}
	blocks[0].Blocks[0].Content[0].Text = "已确认事实。"
	for _, change := range []struct {
		name  string
		apply func()
	}{
		{"origin", func() { blocks[0].Blocks[0].Origin = "background_explanation" }},
		{"citation", func() { catalog["t1"] = domain.Citation{Kind: "video_text", SegmentIDs: []string{"another-segment"}} }},
		{"source", func() { s.snapshot.TextHash = "changed-primary-source" }},
		{"transcript", func() {
			s.transcript = []domain.Segment{{ID: "segment-one", Text: "Changed original evidence", Origin: "asr"}}
		}},
		{"question", func() { s.turn = &domain.Turn{Question: "A different question about this source"} }},
		{"pixels", func() { batch.Frames = []domain.Asset{{ID: "frame", SHA256: "different-bytes"}} }},
		{"mode", func() { batch.Mode = "visual"; batch.Fields = map[string]any{path: true} }},
	} {
		before := s.reviewApprovalKeys(candidate, catalog, batch)[path]
		change.apply()
		if s.reviewApprovalKeys(candidate, catalog, batch)[path] == before {
			t.Errorf("changed %s retained an approval for different evidence", change.name)
		}
	}
}

func TestReviewRecoveryApprovalIncludesChapterMeaning(t *testing.T) {
	s := &session{}
	sections := []domain.Section{
		{ID: "one", Title: "仅在前提成立时", Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "适用条件。"}}}, {Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "在此条件下成立。"}}}}},
		{ID: "two", Title: "另一个独立主题", Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "独立事实。"}}}}},
	}
	candidate := map[string]any{"title": "整体范围", "sections": sections}
	batch := evidenceReviewBatch{Mode: "source_text"}
	path := "/sections/0/blocks/1/content/0/text"
	before := s.reviewApprovalKeys(candidate, nil, batch)[path]
	sections[1].Blocks[0].Content[0].Text = "只改变另一个独立章节。"
	if s.reviewApprovalKeys(candidate, nil, batch)[path] != before {
		t.Fatal("an independent chapter edit forced a full article review")
	}
	sections[0].Blocks[0].Content[0].Text = "适用条件已经改变。"
	if s.reviewApprovalKeys(candidate, nil, batch)[path] == before {
		t.Fatal("an unchanged sentence reused approval after its chapter's conditions changed")
	}
}
