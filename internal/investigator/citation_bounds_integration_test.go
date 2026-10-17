package investigator

import (
	"context"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestCitationCatalogIntersectsMediaWithoutChangingPlatformCue(t *testing.T) {
	db, _, _ := testsupport.Services(t)
	s := &session{engine: &Engine{Store: db}, snapshot: domain.Snapshot{ID: "snapshot", DurationMS: 1000}, transcript: []domain.Segment{{ID: "tail", Ordinal: 1, StartMS: 900, EndMS: 1239, Text: "完整的结尾字幕", Origin: "platform_subtitle"}, {ID: "outside", Ordinal: 2, StartMS: 1200, EndMS: 1500, Text: "原始数据保留", Origin: "platform_subtitle"}}}
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.validateCitation(context.Background(), catalog["t1"], true); err != nil {
		t.Fatalf("server offered a citation that its own validator rejects: %v", err)
	}
	if catalog["t1"].Range.EndMS != 1000 || s.transcript[0].EndMS != 1239 || len(s.transcript) != 2 {
		t.Fatal("original source was changed or citation was not bounded")
	}
	if _, ok := catalog["t2"]; ok {
		t.Fatal("cue entirely outside actual media offered as a valid citation")
	}
}
