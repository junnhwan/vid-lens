package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"vidlens/internal/domain"
)

// Replays the production failure: a revision supplies both identity fields and
// finish=true after a previously accepted write. The current rejection must
// reach the next author call, never freeze/review the old draft's receipt.
func TestArticleLoopRejectedFinishRetainsCurrentWriteError(t *testing.T) {
	var calls atomic.Int32
	var stable string
	var reviewed atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		n := calls.Add(1)
		if p["scope"] != nil {
			reviewed.Add(1)
			if n != 4 {
				t.Errorf("stale draft reached review at call %d", n)
			}
			writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
			return
		}
		in := loopGoodWrite()
		if n == 1 {
			in.Finish = false
		} else {
			// Stable IDs are part of the authoritative author's workspace.
			var workspace loopSaveInput
			_ = json.Unmarshal(p["current_article"], &workspace)
			if stable == "" && len(workspace.Sections) > 0 {
				stable = workspace.Sections[0].SectionID
			}
			in.ExpectedVersion = 1
			in.Sections[0].SectionID = stable
			if n == 3 {
				if !strings.Contains(string(p["feedback"]), "ambiguous_section_identity") {
					t.Errorf("current write error was lost: %s", p["feedback"])
				}
				in.Sections[0].Key = ""
			}
		}
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 || reviewed.Load() != 1 {
		t.Fatalf("calls=%d reviews=%d", calls.Load(), reviewed.Load())
	}
}

func TestArticleLoopDeliveredPixelsReachActivity(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		var args map[string]any
		if p["scope"] != nil {
			_ = json.Unmarshal(domain.JSON(loopGoodReport(t, p)), &args)
			args["activity_title"] = "核对配图中的可见事实"
			writeReviewWireArgs(w, loopReviewTool, args)
		} else {
			_ = json.Unmarshal(domain.JSON(loopGoodWrite()), &args)
			args["activity_title"] = "整理实验过程与主要结论"
			writeReviewWireArgs(w, loopSaveTool, args)
		}
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.engine.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM run_events WHERE run_id=$1 AND step->>'kind'='model_submission' AND jsonb_array_length(step->'frame_ids')>0`, s.claim.Run.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("real pixels were delivered but no frame IDs reached the visitor activity")
	}
	if err := s.engine.Store.Pool.QueryRow(context.Background(), `SELECT count(DISTINCT step->>'purpose') FROM run_events WHERE run_id=$1 AND step->>'kind'='model_response' AND step->>'purpose' LIKE '准备：%'`, s.claim.Run.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("distinct AI action titles were lost: count=%d err=%v", count, err)
	}
}

func TestArticleLoopV2OffersPublicTitleOnEveryAction(t *testing.T) {
	s := &session{}
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ArticleLoopV2}
	for _, review := range []bool{false, true} {
		infos, err := s.loopTools(review)
		if err != nil {
			t.Fatal(err)
		}
		for _, info := range infos {
			sc, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]any
			_ = json.Unmarshal(domain.JSON(sc), &root)
			props, _ := root["properties"].(map[string]any)
			if props["activity_title"] == nil {
				t.Errorf("%s omits the public action title", info.Name)
			}
		}
	}
}
