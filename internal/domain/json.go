package domain

import "encoding/json"

func (x Inline) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": x.Type}
	switch x.Type {
	case "text":
		marks := x.Marks
		if marks == nil {
			marks = []string{}
		}
		m["text"] = x.Text
		m["marks"] = marks
	case "citation":
		m["citation_id"] = x.CitationID
	case "link":
		m["text"] = x.Text
		m["url"] = x.URL
	}
	return json.Marshal(m)
}
func (b Block) MarshalJSON() ([]byte, error) {
	m := map[string]any{"id": b.ID, "type": b.Type, "origin": b.Origin}
	switch b.Type {
	case "paragraph", "quote":
		m["content"] = b.Content
	case "list":
		m["ordered"] = b.Ordered
		m["items"] = b.Items
	case "code":
		m["language"] = b.Language
		m["text"] = b.Text
		m["citation_ids"] = nonNil(b.CitationIDs)
	case "table":
		m["columns"] = b.Columns
		m["rows"] = b.Rows
	case "figure":
		m["frame_ids"] = b.FrameIDs
		m["layout"] = b.Layout
		m["caption"] = b.Caption
		m["citation_ids"] = nonNil(b.CitationIDs)
	}
	return json.Marshal(m)
}
func (c Citation) MarshalJSON() ([]byte, error) {
	m := map[string]any{"id": c.ID, "kind": c.Kind, "label": c.Label}
	switch c.Kind {
	case "video_text":
		m["snapshot_id"] = c.SnapshotID
		m["range"] = c.Range
		m["segment_ids"] = c.SegmentIDs
		m["quote"] = c.Quote
		m["text_origin"] = c.TextOrigin
	case "video_frame":
		m["snapshot_id"] = c.SnapshotID
		m["frame_id"] = c.FrameID
		m["time_ms"] = c.TimeMS
	case "article_passage":
		m["article_id"] = c.ArticleID
		m["section_id"] = c.SectionID
		m["block_id"] = c.BlockID
		m["quote"] = c.Quote
	case "external_reference":
		m["reference_id"] = c.ReferenceID
		m["content_sha256"] = c.ContentSHA256
		m["url"] = c.URL
		m["title"] = c.Title
		m["read_at"] = c.ReadAt
		m["quote"] = c.Quote
	}
	return json.Marshal(m)
}
func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
