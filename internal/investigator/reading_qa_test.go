package investigator

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestReadyOutlineUsesCompositionOutputReservation(t *testing.T) {
	p := config.DefaultProfile()
	p.MaxOutputTokens = 65536
	p.InputCharsLimit = 300000
	p.ContextTokens = 1048576
	policy, _ := domain.InterpretationPolicy("standard")
	s := session{profile: p, compositionReady: true, cp: checkpoint{HarnessRevision: 9}, spec: domain.GenerationSpec{DetailPolicy: &policy}}
	b, err := s.budget(context.Background(), []*schema.Message{{Role: schema.User, Content: "ready to save the full-video topic outline"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if b.OutputTokens != policy.CompositionOutputTokens {
		t.Fatalf("outline silently capped: phase=%s output=%d want=%d", b.Phase, b.OutputTokens, policy.CompositionOutputTokens)
	}
	s.cp.HarnessRevision = 7
	legacy, err := s.budget(context.Background(), nil, 0)
	if err != nil || legacy.OutputTokens != 8192 {
		t.Fatalf("frozen older outline budget changed: %+v %v", legacy, err)
	}
}

func TestConceptRelationsAreIndependentOfChapterOutline(t *testing.T) {
	sections := map[string]bool{"isolation": true, "snapshot": true}
	m := &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{{ID: "root", Label: "执行环境"}, {ID: "theme", Label: "控制影响范围", ParentID: domain.Ptr("root")}, {ID: "vm", Label: "隔离内核", ParentID: domain.Ptr("theme"), SectionID: domain.Ptr("isolation")}, {ID: "clone", Label: "快照复用状态", ParentID: domain.Ptr("vm"), SectionID: domain.Ptr("snapshot")}}}
	if err := validateConceptMap(m, sections); err != nil {
		t.Fatal(err)
	}
	flat := &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{{ID: "root", Label: "目录"}, {ID: "one", Label: "隔离", ParentID: domain.Ptr("root"), SectionID: domain.Ptr("isolation")}, {ID: "two", Label: "快照", ParentID: domain.Ptr("root"), SectionID: domain.Ptr("snapshot")}}}
	if err := validateConceptMap(flat, sections); err == nil {
		t.Fatal("flat directory accepted as a concept map")
	}
	m.Nodes[3].SectionID = domain.Ptr("invented")
	if err := validateConceptMap(m, sections); err == nil {
		t.Fatal("concept linked to an invented section")
	}
}

func TestAnswerPartsKeepCanonicalBlocksAndRejectDuplicateMembership(t *testing.T) {
	a := domain.Answer{Blocks: []domain.Block{{ID: "why"}, {ID: "how"}}, Parts: []domain.AnswerPart{{Title: "隔离边界", BlockIDs: []string{"why"}}, {Title: "恢复成本", BlockIDs: []string{"how"}}}}
	if err := validateAnswerParts(&a); err != nil {
		t.Fatal(err)
	}
	a.Parts[1].BlockIDs = []string{"why"}
	if err := validateAnswerParts(&a); err == nil {
		t.Fatal("same body block rendered twice")
	}
	a.Parts = nil
	if err := validateAnswerParts(&a); err != nil {
		t.Fatal("legacy answer required fabricated sections", err)
	}
}
