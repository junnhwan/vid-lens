package exporting

import (
	"context"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestLegacyRendererRetainsItsOriginalFlatPayload(t *testing.T) {
	a := domain.Article{Title: "主题", Source: domain.Source{CanonicalURL: "https://www.youtube.com/watch?v=abcdefghijk"}, MindMap: &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{{ID: "root", Label: "主线"}, {ID: "branch", Label: "分支", ParentID: domain.Ptr("root")}}}}
	md, err := markdownVersion(a, domain.GenerationModel{}, "", nil, 1)
	if err != nil || !strings.Contains(string(md), "- 主线\n- 分支\n") {
		t.Fatalf("frozen legacy export changed: %s %v", md, err)
	}
}

func TestNewExportsDoNotReuseOrReplaceLegacyRendererPayloads(t *testing.T) {
	s, cfg, owner, request, _ := exportFixture(t)
	ctx := context.Background()
	service := &Service{Store: s, Config: cfg}
	old, _, err := s.CreateExport(ctx, owner, request.ID, "markdown_zip", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	// Represents work accepted before this renderer was deployed.
	if _, err = s.Pool.Exec(ctx, "UPDATE article_exports SET renderer_version=1 WHERE id=$1", old.ID); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimExport(ctx, old.ID)
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	if err = service.Run(ctx, *claim); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.Export(ctx, owner, request.ID, old.ID)
	if err != nil || legacy.Status != "ready" || legacy.RendererVersion != 1 {
		t.Fatalf("legacy export unavailable: %+v %v", legacy, err)
	}
	if latest, err := s.LatestExport(ctx, owner, request.ID, "markdown_zip"); err != nil || latest != nil {
		t.Fatalf("old payload offered as the current renderer: %+v %v", latest, err)
	}
	current, reused, err := s.CreateExport(ctx, owner, request.ID, "markdown_zip", "current")
	if err != nil || reused || current.ID == old.ID {
		t.Fatalf("new renderer reused legacy bytes: %+v %v %v", current, reused, err)
	}
	next, err := s.Export(ctx, owner, request.ID, current.ID)
	if err != nil || next.RendererVersion != RendererVersion {
		t.Fatalf("new renderer version was not persisted: %+v %v", next, err)
	}
	replayed, _, err := s.CreateExport(ctx, owner, request.ID, "markdown_zip", "legacy")
	if err != nil || replayed.ID != old.ID || !replayed.Replayed {
		t.Fatalf("legacy idempotency replay was rewritten: %+v %v", replayed, err)
	}
	preserved, err := s.Export(ctx, owner, request.ID, old.ID)
	if err != nil || preserved.Status != "ready" || *preserved.SHA256 != *legacy.SHA256 {
		t.Fatalf("legacy export was replaced: %+v %v", preserved, err)
	}
}

func TestConceptMapExportUsesHierarchyAndChapterLinks(t *testing.T) {
	a := domain.Article{Title: "主题", Source: domain.Source{CanonicalURL: "https://www.youtube.com/watch?v=abcdefghijk"}, Sections: []domain.Section{{ID: "isolation", Title: "隔离"}, {ID: "restore", Title: "恢复"}}, MindMap: &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{
		{ID: "leaf", ParentID: domain.Ptr("group"), Label: "快照恢复", SectionID: domain.Ptr("restore")},
		{ID: "group", ParentID: domain.Ptr("root"), Label: "降低重复开销"},
		{ID: "root", Label: "执行环境"},
	}}}
	md, err := Markdown(a, domain.GenerationModel{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "- 执行环境\n  - 降低重复开销\n    - [快照恢复](#section-2)\n"
	if !strings.Contains(string(md), want) {
		t.Fatalf("concept hierarchy or chapter target lost in export:\n%s", md)
	}
}
