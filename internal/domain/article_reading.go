package domain

// ArticleReading is a full-content reading projection, not a summary. Canonical
// citations stay authoritative in the Article; this view removes rendering and
// repeated citation metadata from language-model reading contexts.
type ArticleReading struct {
	ID          string                  `json:"id"`
	Title       string                  `json:"title"`
	Sections    []ArticleReadingSection `json:"sections"`
	Limitations []Limitation            `json:"limitations"`
}
type ArticleReadingSection struct {
	ID     string                `json:"id"`
	Title  string                `json:"title"`
	Blocks []ArticleReadingBlock `json:"blocks"`
}
type ArticleReadingBlock struct {
	ID          string               `json:"id"`
	Type        string               `json:"type"`
	Origin      string               `json:"origin"`
	Text        string               `json:"text,omitempty"`
	Ordered     *bool                `json:"ordered,omitempty"`
	Items       []ArticleReadingItem `json:"items,omitempty"`
	Language    *string              `json:"language,omitempty"`
	Columns     []string             `json:"columns,omitempty"`
	Rows        []ArticleReadingRow  `json:"rows,omitempty"`
	FrameIDs    []string             `json:"frame_ids,omitempty"`
	Caption     string               `json:"caption,omitempty"`
	CitationIDs []string             `json:"citation_ids,omitempty"`
}
type ArticleReadingItem struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	CitationIDs []string `json:"citation_ids,omitempty"`
}
type ArticleReadingRow struct {
	Cells       []string `json:"cells"`
	CitationIDs []string `json:"citation_ids,omitempty"`
}

func ReadingArticle(a Article) ArticleReading {
	out := ArticleReading{ID: a.ID, Title: a.Title, Sections: []ArticleReadingSection{}, Limitations: a.Limitations}
	for _, section := range a.Sections {
		s := ArticleReadingSection{ID: section.ID, Title: section.Title, Blocks: []ArticleReadingBlock{}}
		for _, block := range section.Blocks {
			b := ArticleReadingBlock{ID: block.ID, Type: block.Type, Origin: block.Origin, Ordered: block.Ordered, Language: block.Language, Columns: block.Columns, FrameIDs: block.FrameIDs, Caption: block.Caption, CitationIDs: append([]string{}, block.CitationIDs...)}
			for _, part := range block.Content {
				if part.Type == "citation" {
					b.CitationIDs = append(b.CitationIDs, part.CitationID)
				}
			}
			switch block.Type {
			case "paragraph", "quote", "code":
				b.Text = BlockText(block)
			case "list":
				for _, item := range block.Items {
					entry := ArticleReadingItem{ID: item.ID, Text: BlockText(Block{Type: "paragraph", Content: item.Content})}
					for _, part := range item.Content {
						if part.Type == "citation" {
							entry.CitationIDs = append(entry.CitationIDs, part.CitationID)
						}
					}
					b.Items = append(b.Items, entry)
				}
			case "table":
				for _, row := range block.Rows {
					b.Rows = append(b.Rows, ArticleReadingRow{Cells: row.Cells, CitationIDs: row.CitationIDs})
				}
			}
			s.Blocks = append(s.Blocks, b)
		}
		out.Sections = append(out.Sections, s)
	}
	return out
}
