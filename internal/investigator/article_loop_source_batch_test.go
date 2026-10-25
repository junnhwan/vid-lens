package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"vidlens/internal/domain"
)

// The actual engine must partition semantic work before submitting model:0,
// not wait for a time/token failure and replay that same frozen input.
func TestArticleLoopSourceWorkIsBoundedBeforeSendAndPreservesAllOriginals(t *testing.T) {
	batches, assessed := 0, map[string]bool{}
	var s *session
	s = freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if string(p["material_role"]) == `"source_requirements"` {
			batches++
			var originals []loopOriginal
			if err := json.Unmarshal(p["originals"], &originals); err != nil {
				t.Error(err)
			}
			work := 0
			for _, original := range originals {
				work += utf8.RuneCountInString(original.Segment.Text)
				if assessed[original.Alias] {
					t.Error("source requirement batch replayed", original.Alias)
				}
				assessed[original.Alias] = true
				if original.Segment.Text != strings.Repeat("source condition, action and exception. ", 55) {
					t.Error("original truncated or paraphrased")
				}
			}
			if work > 16000 {
				t.Errorf("single semantic source workload too large before send: %d runes", work)
			}
			writeReviewWireArgs(w, loopSourceTool, sufficiencyPlan(t, p))
			return
		}
		if len(assessed) != 25 || batches < 2 {
			t.Error("author/reviewer reached before all bounded source batches were assessed", batches, len(assessed))
		}
		if p["scope"] != nil {
			writeReviewWireArgs(w, loopReviewTool, sufficiencyReport(t, p))
			return
		}
		writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
	})
	ctx := context.Background()
	_, err := s.engine.Store.Pool.Exec(ctx, "UPDATE transcript_segments SET text=$2 WHERE snapshot_id=$1", s.snapshot.ID, strings.Repeat("source condition, action and exception. ", 55))
	if err != nil {
		t.Fatal(err)
	}
	s.transcript, err = s.engine.Store.Transcript(ctx, s.snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	if len(s.cp.ArticleLoop.SourcePlans) != batches || len(s.cp.ArticleLoop.PlannedSources) != 25 {
		t.Fatal("partition receipts lost across checkpoint")
	}
}

func TestArticleLoopSourceWorkKeepsV8FrozenSelection(t *testing.T) {
	s := freshSufficiencyFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("preflight must not send a model request") })
	s.cp.ArticleLoop.ReviewRevision = domain.ArticleConvergenceReviewV8
	ctx := context.Background()
	_, err := s.engine.Store.Pool.Exec(ctx, "UPDATE transcript_segments SET text=$2 WHERE snapshot_id=$1", s.snapshot.ID, strings.Repeat("unchanged original condition. ", 100))
	if err != nil {
		t.Fatal(err)
	}
	s.transcript, err = s.engine.Store.Transcript(ctx, s.snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !s.loopCompleteFirstPass() {
		t.Fatal("v8 full-body coverage behavior changed")
	}
	p, messages, _, err := s.prepareLoopCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != "source" || len(p.Sources) != 25 {
		t.Fatal("new partition changed old v8 frozen selection", p)
	}
	for _, m := range messages {
		var data map[string]json.RawMessage
		_ = json.Unmarshal([]byte(m.Content), &data)
		if data["source_work_limit_runes"] != nil || data["context_originals"] != nil {
			t.Fatal("v9 fields leaked into old v8")
		}
	}
}
