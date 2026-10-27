package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/exporting"
	"vidlens/internal/store"
)

func deliveryTestMap(targets ...string) *domain.MindMap {
	tree := &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{{ID: "root", Label: "来源事实的联系"}, {ID: "group", Label: "从前提到结果", ParentID: domain.Ptr("root")}}}
	for i, target := range targets {
		tree.Nodes = append(tree.Nodes, domain.MapNode{ID: fmt.Sprint("concept-", i), ParentID: domain.Ptr("group"), Label: fmt.Sprint("来源要点", i+1), SectionID: domain.Ptr(target)})
	}
	return tree
}

func conceptMapWrite() loopSaveInput {
	in := loopGoodWrite()
	in.MindMap = deliveryTestMap("main")
	return in
}

func TestDeliveryConceptMapWireAcceptsConceptTree(t *testing.T) {
	s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}, ArticleLoop: &articleLoopState{ReviewRevision: domain.CurrentArticleDelivery}}}
	infos, err := s.loopTools(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name != loopSaveTool {
			continue
		}
		sc, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]any
		if err = json.Unmarshal(domain.JSON(sc), &root); err != nil {
			t.Fatal(err)
		}
		if root["properties"].(map[string]any)["mind_map"] == nil {
			t.Error("new article author cannot submit its concept map")
		}
	}
	var in loopSaveInput
	if err := loopDecode([]byte(`{"expected_version":0,"sections":[],"mind_map":{"root_id":"root","nodes":[{"id":"root","parent_id":null,"label":"主题","section_id":null}]}}`), &in); err != nil {
		t.Fatalf("actual chapter-save decoder rejects the concept map: %v", err)
	}
}

func TestDeliveryConceptMapKeepsHistoricalWire(t *testing.T) {
	s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}, ArticleLoop: &articleLoopState{ReviewRevision: domain.ArticleDeliveryV6}}}
	infos, err := s.loopTools(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name == loopSaveTool {
			sc, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]any
			_ = json.Unmarshal(domain.JSON(sc), &root)
			if root["properties"].(map[string]any)["mind_map"] != nil {
				t.Fatal("existing v6 execution received the new map protocol")
			}
		}
	}
}

func TestDeliveryConceptMapUsesFinalWriteWithoutExtraCall(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if string(p["mind_map_required"]) != "true" {
			t.Error("author was not told to include its concept map")
		}
		writeReviewWireArgs(w, loopSaveTool, conceptMapWrite())
	}, domain.CurrentArticleDelivery)
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil || run.ResultID == nil || calls.Load() != 1 {
		t.Fatalf("map required extra planning/provider calls: calls=%d run=%+v error=%v", calls.Load(), run, err)
	}
	a, err := s.engine.Store.PublishedArticle(ctx, *run.ResultID)
	if err != nil || a.MindMap == nil || *a.MindMap.Nodes[2].SectionID != a.Sections[0].ID {
		t.Fatalf("published map did not bind the newly created chapter: %+v %v", a.MindMap, err)
	}
	reviews, err := s.engine.Store.PendingArticleReviews(ctx)
	if err != nil || len(reviews) != 1 {
		t.Fatal("map policy lost independent automatic review", err)
	}
}

func TestDeliveryRegenerationPublishesWithReusableIdentity(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		calls.Add(1)
		writeReviewWireArgs(w, loopSaveTool, conceptMapWrite())
	}, domain.CurrentArticleDelivery)
	ctx := context.Background()
	// The store admission tests exercise creation; here install its frozen
	// identity on the current-delivery fixture to exercise the model and gate.
	s.spec.FreshArticleID = domain.ID()
	work := "article-v3:" + s.snapshot.ID + ":" + store.Hash(&s.spec)
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE generation_requests SET generation_spec=$2 WHERE active_run_id=$1", s.claim.Run.ID, domain.JSON(s.spec)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE job_runs SET work_key=$2 WHERE id=$1", s.claim.Run.ID, work); err != nil {
		t.Fatal(err)
	}
	s.claim.Run.WorkKey = work
	if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil || run.ResultID == nil || calls.Load() != 1 {
		t.Fatalf("fresh delivery did not publish: calls=%d run=%+v err=%v", calls.Load(), run, err)
	}
	canonical := s.spec
	canonical.FreshArticleID = ""
	want := "article-v3:" + s.snapshot.ID + ":" + store.Hash(&canonical)
	var key string
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT reuse_key FROM articles WHERE id=$1", *run.ResultID).Scan(&key); err != nil || key != want {
		t.Fatalf("fresh result cannot be reused under the ordinary specification: %v", err)
	}
}

func TestDeliveryConceptMapBatchesReplayPublicationAndExport(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model expected") }, domain.CurrentArticleDelivery)
	ctx := context.Background()
	s.cp.ArticleLoop.ReadSources = s.loopAllSourceAliases()
	first := visualTextWrite()
	first.Finish = false
	first.MindMap = deliveryTestMap("main")
	before := string(domain.JSON(first))
	if _, progress, err := s.saveLoopSections(ctx, "first", first); err != nil || !progress {
		t.Fatal("first mapped batch rejected", err)
	}
	if before != string(domain.JSON(first)) {
		t.Fatal("creation-key resolution changed the idempotency input")
	}
	_, d, err := s.loadLoopDraft(ctx)
	if err != nil || d.Article.MindMap == nil {
		t.Fatal("first map not durably saved", err)
	}
	mainID := d.Article.Sections[0].ID
	second := loopSaveInput{ExpectedVersion: 1, Sections: []loopSection{{Key: "end", Title: "最后的来源事实", Content: []loopContent{{Text: domain.Ptr("后半段保留最后一项来源事实。"), Sources: []string{"t25"}}}}}}
	if _, _, err = s.saveLoopSections(ctx, "second", second); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.saveLoopSections(ctx, "first", first); err != nil {
		t.Fatal("exact old replay failed", err)
	}
	if err = s.persist(ctx, nil); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	saved, d, err := s.loadLoopDraft(ctx)
	if err != nil || saved.Version != 2 || len(d.Article.Sections) != 2 || d.Article.MindMap == nil {
		t.Fatal("replay or worker reload discarded the map or later body", err)
	}
	if ok, err := s.freezeLoopCandidate(ctx, loopFinishInput{ExpectedVersion: 2}); err != nil || ok || !strings.Contains(string(s.cp.ArticleLoop.Feedback), d.Article.Sections[1].ID) {
		t.Fatal("incomplete concept map silently passed final delivery", err)
	}
	projection, err := s.loopAuthorDraft(d.Article, d.Notes, 2)
	if err != nil || projection.MindMap == nil {
		t.Fatal("author cannot see its saved concept map", err)
	}
	final := loopSaveInput{ExpectedVersion: 2, Sections: []loopSection{}, MindMap: deliveryTestMap(mainID, d.Article.Sections[1].ID)}
	if _, _, err = s.saveLoopSections(ctx, "map-only", final); err != nil {
		t.Fatal(err)
	}
	_, complete, err := s.loadLoopDraft(ctx)
	if err != nil || string(domain.JSON(complete.Article.Sections)) != string(domain.JSON(d.Article.Sections)) {
		t.Fatal("map-only save rewrote the prose", err)
	}
	if ok, err := s.freezeLoopCandidate(ctx, loopFinishInput{ExpectedVersion: 3}); err != nil || !ok {
		t.Fatal("complete mapped article did not freeze", err)
	}
	if err = s.persist(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.publishLoopCandidate(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil || run.ResultID == nil {
		t.Fatal(err)
	}
	a, err := s.engine.Store.PublishedArticle(ctx, *run.ResultID)
	if err != nil || string(domain.JSON(a.MindMap)) != string(domain.JSON(final.MindMap)) {
		t.Fatal("publication lost the saved concept tree", err)
	}
	md, err := exporting.Markdown(a, domain.GenerationModel{}, "", nil)
	if err != nil || !strings.Contains(string(md), "  - 从前提到结果\n    - [来源要点1](#section-1)") || !strings.Contains(string(md), "[来源要点2](#section-2)") {
		t.Fatalf("export lost hierarchy or navigation: %s %v", md, err)
	}
}

func TestDeliveryConceptMapInvalidTreePreservesProseAndAtomicDeletes(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model expected") }, domain.CurrentArticleDelivery)
	ctx := context.Background()
	s.cp.ArticleLoop.ReadSources = s.loopAllSourceAliases()
	first := visualTextWrite()
	first.Finish = false
	first.MindMap = deliveryTestMap("invented")
	result, _, err := s.saveLoopSections(ctx, "invalid-map", first)
	receipt, _ := result.(map[string]any)["receipt"].(*loopReceipt)
	if err != nil || receipt == nil || len(receipt.Accepted) != 1 || len(receipt.Rejected) != 1 || receipt.Rejected[0].Path != "/mind_map" {
		t.Fatal("map error did not preserve accepted prose with actionable feedback", result, err)
	}
	_, d, err := s.loadLoopDraft(ctx)
	if err != nil || d.Article.MindMap != nil || len(d.Article.Sections) != 1 {
		t.Fatal("invalid tree persisted or prose lost", err)
	}
	if ok, err := s.freezeLoopCandidate(ctx, loopFinishInput{ExpectedVersion: 1}); err != nil || ok {
		t.Fatal("missing map did not block a falsely complete result", err)
	}
	mainID := d.Article.Sections[0].ID
	second := loopSaveInput{ExpectedVersion: 1, Sections: []loopSection{{Key: "end", Title: "结尾", Content: []loopContent{{Text: domain.Ptr("结尾的事实保留。"), Sources: []string{"t25"}}}}}, MindMap: deliveryTestMap(mainID, "end")}
	if _, _, err = s.saveLoopSections(ctx, "valid-map", second); err != nil {
		t.Fatal(err)
	}
	saved, original, _ := s.loadLoopDraft(ctx)
	endID := original.Article.Sections[1].ID
	result, _, err = s.saveLoopSections(ctx, "dangling-delete", loopSaveInput{ExpectedVersion: 2, DeleteIDs: []string{endID}, Sections: []loopSection{}})
	if err != nil || result.(map[string]any)["error"] == nil {
		t.Fatal("delete left a concept pointing to a nonexistent chapter", result, err)
	}
	after, retained, _ := s.loadLoopDraft(ctx)
	if after.Version != saved.Version || domain.ArticleHash(retained.Article) != domain.ArticleHash(original.Article) {
		t.Fatal("rejected atomic delete modified the saved article")
	}
	if result, _, err = s.saveLoopSections(ctx, "mapped-delete", loopSaveInput{ExpectedVersion: 2, DeleteIDs: []string{endID}, Sections: []loopSection{}, MindMap: deliveryTestMap(mainID)}); err != nil || result.(map[string]any)["error"] != nil {
		t.Fatal("atomic chapter/map update failed", result, err)
	}
	_, retained, _ = s.loadLoopDraft(ctx)
	if len(retained.Article.Sections) != 1 || validateDeliveryConceptMap(retained.Article.MindMap, retained.Article.Sections) != nil {
		t.Fatal("atomic mapped deletion left an invalid tree")
	}
}

func TestDeliveryConceptMapMalformedSidecarPreservesValidProse(t *testing.T) {
	var raw map[string]any
	_ = json.Unmarshal(domain.JSON(visualTextWrite()), &raw)
	raw["mind_map"] = map[string]any{"root_id": 3, "nodes": "bad tree"}
	var in loopSaveInput
	if err := decodeDeliveryConceptMapWrite(domain.JSON(raw), &in); err != nil || len(in.Sections) != 1 || in.mindMapParseIssue == "" || in.MindMap != nil {
		t.Fatal("malformed map discarded otherwise valid chapters", err)
	}
}

func TestDeliveryConceptMapRejectsCyclesAndMissingChapters(t *testing.T) {
	chapters := []domain.Section{{ID: "first"}, {ID: "last"}}
	for _, bad := range []string{"cycle", "missing_chapter", "unknown_chapter", "flat_directory"} {
		t.Run(bad, func(t *testing.T) {
			tree := deliveryTestMap("first", "last")
			switch bad {
			case "cycle":
				tree.Nodes[1].ParentID = domain.Ptr(tree.Nodes[2].ID)
			case "missing_chapter":
				tree.Nodes = tree.Nodes[:3]
			case "unknown_chapter":
				tree.Nodes[3].SectionID = domain.Ptr("invented")
			case "flat_directory":
				tree.Nodes = append(tree.Nodes[:1], tree.Nodes[2:]...)
				for i := 1; i < len(tree.Nodes); i++ {
					tree.Nodes[i].ParentID = domain.Ptr("root")
				}
			}
			if validateDeliveryConceptMap(tree, chapters) == nil {
				t.Fatal("invalid concept map accepted")
			}
		})
	}
}
