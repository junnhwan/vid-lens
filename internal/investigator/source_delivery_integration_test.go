package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestOverviewBatchesPersistUnseenWorkAndUnloadObservedPixels(t *testing.T) {
	s, article := publicationReviewFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("pixel batching must not contact a provider") })
	ctx := context.Background()
	s.cp.ExecutionContract.Version = domain.ExecutionContractV4
	original, err := s.engine.Store.Asset(ctx, article.Citations["f1"].FrameID)
	if err != nil {
		t.Fatal(err)
	}
	pixels, err := os.ReadFile(filepath.Join(s.engine.Config.DataDir, original.StorageKey))
	if err != nil {
		t.Fatal(err)
	}
	s.cp.OverviewIDs, s.cp.CitationFrames = nil, nil
	s.cp.Inspections = map[string][]string{}
	for i := 0; i < 6; i++ {
		asset := original
		asset.ID = domain.ID()
		asset.StorageKey = asset.ID + ".jpg"
		asset.TimeMS = domain.Ptr(int64(i * 100))
		asset.RequestedTimeMS = asset.TimeMS
		if err := os.WriteFile(filepath.Join(s.engine.Config.DataDir, asset.StorageKey), pixels, 0600); err != nil {
			t.Fatal(err)
		}
		if err := s.engine.Store.AddAsset(ctx, s.claim, asset); err != nil {
			t.Fatal(err)
		}
		s.cp.PendingOverview = append(s.cp.PendingOverview, asset.ID)
	}
	msgs := []*schema.Message{schema.SystemMessage("frozen V4 fixture")}
	if err := s.deliverOverviewBatch(ctx, &msgs); err != nil {
		t.Fatal(err)
	}
	if len(s.cp.OverviewIDs) != 3 || len(s.cp.PendingOverview) != 3 || len(s.unobserved(ctx)) != 3 {
		t.Fatal("undelivered frames became observations", s.cp)
	}
	if err := s.persist(ctx, msgs); err != nil {
		t.Fatal(err)
	}
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(run.Checkpoint, &s.cp); err != nil {
		t.Fatal(err)
	}
	before := len(msgs)
	if err := s.deliverOverviewBatch(ctx, &msgs); err != nil || len(msgs) != before {
		t.Fatal("unobserved pixels were evicted for another batch", err)
	}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	if result, err := planCall(s, "first-observed", "record_observation", observation{InspectionID: "overview-batch:0", FrameIDs: append([]string{}, s.cp.OverviewIDs...), VisibleFacts: []string{"合成黑色画面。"}, Status: "supported"}); err != nil || result["saved"] != true {
		t.Fatal(result, err)
	}
	m := middleware{s: s}
	_, state, err := m.BeforeModelRewriteState(ctx, &adk.ChatModelAgentState{Messages: msgs}, nil)
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for _, msg := range state.Messages {
		for _, part := range msg.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeImageURL {
				images++
			}
		}
	}
	if images != 3 || len(s.cp.PendingOverview) != 0 || len(s.cp.OverviewIDs) != 6 || len(s.unobserved(ctx)) != 3 {
		t.Fatal("completed pixels accumulated or queued work disappeared", images, s.cp)
	}
	var overviewStatus string
	if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT step->'plan'->'items'->0->>'status' FROM run_events WHERE run_id=$1 AND step_id=$2 ORDER BY seq DESC LIMIT 1", s.claim.Run.ID, "plan_"+s.claim.Run.ID).Scan(&overviewStatus); err != nil || overviewStatus != "active" {
		t.Fatal("public plan claimed complete coverage while pixels await observation", overviewStatus, err)
	}
}

func TestV4VisualReviewLimitsBatchesWithoutBreakingAtomicComparisons(t *testing.T) {
	s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV4}}}
	s.profile.MaxOutputTokens = 32768
	units := []reviewUnit{{ID: "comparison", FrameIDs: []string{"a", "b", "c", "d"}}, {ID: "another", FrameIDs: []string{"e", "f"}}}
	tasks, err := s.planReviewTasks(units)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Mode == "visual" && slices.ContainsFunc(task.Units, func(u reviewUnit) bool { return u.ID == "comparison" }) {
			if len(task.Units) != 1 || len(reviewTaskFrames(task)) != 4 {
				t.Fatal("required comparison pixels were split or grouped with unrelated work", task)
			}
		}
	}
}

func TestV4PublicationRejectsUnfinishedSourceDeliveryEvenWithApprovedReview(t *testing.T) {
	for _, pending := range []string{"none", "overview", "observation", "source"} {
		t.Run(pending, func(t *testing.T) {
			s, article := publicationReviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var in reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				reviewWireReply(t, w, in, false)
			})
			s.cp.ExecutionContract.Version = domain.ExecutionContractV4
			ctx := context.Background()
			if err := s.reviewArticle(ctx, article); err != nil {
				t.Fatal(err)
			}
			switch pending {
			case "overview":
				s.cp.PendingOverview = []string{"waiting-delivery"}
			case "observation":
				s.cp.Inspections["waiting-observation"] = []string{"waiting-observation"}
			case "source":
				for i := range s.transcript {
					s.transcript[i].Text = strings.Repeat("尚未整理的完整原文。", 400)
				}
			}
			candidate := map[string]any{"title": article.Title, "sections": article.Sections, "limitations": article.Limitations}
			err := s.requirePublicationReview(ctx, candidate, article.Citations)
			if pending == "none" && err != nil || pending != "none" && err == nil {
				t.Fatal("approved review bypassed unfinished delivery", pending, err)
			}
		})
	}
}
