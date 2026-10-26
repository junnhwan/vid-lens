package investigator

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"vidlens/internal/domain"
)

func TestDeliveryCompactsCitationsWithoutDroppingSourceBindings(t *testing.T) {
	transcript := []domain.Segment{}
	a := domain.Article{Citations: map[string]domain.Citation{}, Sections: []domain.Section{{ID: "chapter", Blocks: []domain.Block{{ID: "body", Type: "paragraph", Content: []domain.Inline{{Type: "text", Text: "The same complete reader text."}}}}}}}
	ids := []string{}
	for i := range 1250 {
		id, alias := fmt.Sprintf("source-%028d", i), fmt.Sprint("t", i+1)
		ids = append(ids, id)
		transcript = append(transcript, domain.Segment{ID: id})
		a.Citations[alias] = domain.Citation{ID: alias, Kind: "video_text", Label: "视频原文", SnapshotID: "same", SegmentIDs: []string{id}, TextOrigin: "subtitle", Range: &domain.Range{StartMS: int64(i * 1000), EndMS: int64((i + 1) * 1000)}}
		a.Sections[0].Blocks[0].Content = append(a.Sections[0].Blocks[0].Content, domain.Inline{Type: "citation", CitationID: alias})
	}
	compactDeliveryCitations(&a, transcript)
	b := a.Sections[0].Blocks[0]
	if b.ID != "body" || len(b.Content) != 2 || b.Content[0].Text != "The same complete reader text." {
		t.Fatal("reader content or stable block identity changed")
	}
	c := a.Citations[b.Content[1].CitationID]
	if !slices.Equal(c.SegmentIDs, ids) || c.Range.StartMS != 0 || c.Range.EndMS != 1250000 || c.Quote != nil || c.SnapshotID != "same" || c.TextOrigin != "subtitle" {
		t.Fatal("source identities, range or provenance lost")
	}
	before := domain.ArticleHash(a)
	compactDeliveryCitations(&a, transcript)
	if domain.ArticleHash(a) != before {
		t.Fatal("citation compaction is not idempotent")
	}
}

func TestDeliveryCompactionKeepsQuotesGapsAndProvenanceSeparate(t *testing.T) {
	transcript := []domain.Segment{{ID: "one"}, {ID: "two"}, {ID: "three"}, {ID: "four"}}
	a := domain.Article{Citations: map[string]domain.Citation{}, Sections: []domain.Section{{Blocks: []domain.Block{{Type: "code", CitationIDs: []string{"one", "three", "four", "quote"}}}}}}
	for i, original := range transcript {
		a.Citations[original.ID] = domain.Citation{ID: original.ID, Kind: "video_text", SnapshotID: "same", TextOrigin: "subtitle", SegmentIDs: []string{original.ID}, Range: &domain.Range{StartMS: int64(i), EndMS: int64(i + 1)}}
	}
	c := a.Citations["four"]
	c.TextOrigin = "asr"
	a.Citations["four"] = c
	a.Citations["quote"] = domain.Citation{ID: "quote", Kind: "video_text", Quote: domain.Ptr("exact words"), SegmentIDs: []string{"four"}}
	compactDeliveryCitations(&a, transcript)
	if !slices.Equal(a.Sections[0].Blocks[0].CitationIDs, []string{"one", "three", "four", "quote"}) || *a.Citations["quote"].Quote != "exact words" {
		t.Fatal("gaps, provenance or exact quotes merged")
	}
}

func TestDeliveryPublishesCompactedSourceBindings(t *testing.T) {
	var s *session
	s = loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		in := conceptMapWrite()
		in.Sections[0].Content[0].Sources = s.loopAllSourceAliases()
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.CurrentArticleDelivery)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil || run.ResultID == nil {
		t.Fatal(err)
	}
	a, err := s.engine.Store.PublishedArticle(context.Background(), *run.ResultID)
	if err != nil {
		t.Fatal(err)
	}
	content := a.Sections[0].Blocks[0].Content
	if len(content) != 2 || len(a.Citations[content[1].CitationID].SegmentIDs) != len(s.transcript) {
		t.Fatal("published article lost compact source bindings")
	}
	reviews, err := s.engine.Store.PendingArticleReviews(context.Background())
	if err != nil || len(reviews) != 1 {
		t.Fatal("new delivery did not queue exactly one separate review", err)
	}
}
