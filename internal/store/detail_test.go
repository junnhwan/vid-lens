package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"vidlens/internal/domain"
)

func TestDetailFreezesPolicySharesSourceAndSeparatesArticles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	url := "https://youtube.com/watch?v=abcdefghijk"
	requests := []domain.Request{}
	for _, level := range []string{"concise", "standard", "detailed"} {
		a, err := s.CreateRequestWithInput(ctx, owner, level, domain.RequestInput{VideoURL: url, DetailLevel: &level}, "", false)
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.Request(ctx, owner, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if r.GenerationSpec.Version != 2 || r.GenerationSpec.DetailLevel != level || r.GenerationSpec.DetailPolicy == nil || r.GenerationSpec.DetailPolicyRevision == "" {
			t.Fatalf("unfrozen policy: %+v", r.GenerationSpec)
		}
		requests = append(requests, r)
	}
	for _, r := range requests {
		if *r.ActiveRunID != *requests[0].ActiveRunID {
			t.Fatal("detail fragmented source acquisition")
		}
	}
	standard, err := s.CreateRequestWithInput(ctx, owner, "standard", domain.RequestInput{VideoURL: url}, "", false)
	if err != nil || !standard.Replayed || standard.ID != requests[1].ID {
		t.Fatalf("omitted != explicit standard: %+v %v", standard, err)
	}
	if _, err = s.CreateRequestWithInput(ctx, owner, "standard", domain.RequestInput{VideoURL: url, DetailLevel: domain.Ptr("detailed")}, "", false); err == nil {
		t.Fatal("detail changed under same idempotency key")
	}
	for _, level := range []string{"", "verbose", " standard"} {
		if _, err = s.CreateRequestWithInput(ctx, owner, domain.ID(), domain.RequestInput{VideoURL: url, DetailLevel: &level}, "", false); err == nil {
			t.Fatalf("invalid level accepted: %q", level)
		}
	}
	c, err := s.Claim(ctx, *requests[0].ActiveRunID, 1, "prepare")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	asset := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "fixture", MIME: "video/mp4", Bytes: 1, ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if err = s.AddAsset(ctx, *c, asset); err != nil {
		t.Fatal(err)
	}
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, DurationMS: 2100000, VideoHash: "fixture", TextHash: "text", Metadata: json.RawMessage(`{}`)}
	if _, err = s.FinishSource(ctx, *c, snap, []domain.Segment{}, asset.ID); err != nil {
		t.Fatal(err)
	}
	runs := map[string]bool{}
	for _, old := range requests {
		r, _ := s.Request(ctx, owner, old.ID)
		if runs[*r.ActiveRunID] {
			t.Fatal("different detail reused article generation")
		}
		runs[*r.ActiveRunID] = true
	}
	before, _ := s.Request(ctx, owner, requests[2].ID)
	if err = s.CancelRequest(ctx, owner, before.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(ctx, *before.ActiveRunID, 1, "cancel"); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateRequest(ctx, owner, "retry-detail", "", before.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.Request(ctx, owner, retry.ID)
	if Hash(before.GenerationSpec) != Hash(after.GenerationSpec) {
		t.Fatal("retry changed frozen model/detail/policy")
	}
	view, _, err := s.RequestView(ctx, owner, after.ID)
	if err != nil {
		t.Fatal(err)
	}
	v := view["request"].(map[string]any)
	if v["detail_level"] != "detailed" || v["detail_policy"] != nil {
		t.Fatal("safe view missing detail or exposing policy")
	}
}

func TestHistoricalRequestKeyReplaysHistoricalSpec(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	url := "https://youtube.com/watch?v=abcdefghijk"
	for _, purpose := range []string{"", "keep steps"} {
		key := domain.ID()
		in := domain.RequestInput{VideoURL: url, Purpose: purpose}
		a, err := s.CreateRequestWithInput(ctx, owner, key, in, "", false)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := s.Request(ctx, owner, a.ID)
		old := *r.GenerationSpec
		old.Version = 1
		old.DetailLevel = ""
		old.DetailPolicy = nil
		old.DetailPolicyRevision = ""
		old.SourcePipelineRevision = ""
		h := Hash(map[string]any{"video_url": url})
		if purpose != "" {
			h = Hash(map[string]any{"version": 2, "video_url": url, "purpose": purpose, "model_id": "", "model_revision_id": ""})
		}
		if _, err = s.Pool.Exec(ctx, "UPDATE generation_requests SET generation_spec=$2 WHERE id=$1", a.ID, domain.JSON(old)); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Pool.Exec(ctx, "UPDATE idempotency SET hash=$2 WHERE resource_id=$1", a.ID, h); err != nil {
			t.Fatal(err)
		}
		in.DetailLevel = domain.Ptr("standard")
		replay, err := s.CreateRequestWithInput(ctx, owner, key, in, "", false)
		if err != nil || !replay.Replayed || replay.ID != a.ID {
			t.Fatalf("historical replay: %+v %v", replay, err)
		}
		got, _ := s.Request(ctx, owner, a.ID)
		if got.GenerationSpec.Version != 1 || got.GenerationSpec.DetailLevel != "" {
			t.Fatal("legacy spec rewritten")
		}
		in.DetailLevel = domain.Ptr("detailed")
		if _, err = s.CreateRequestWithInput(ctx, owner, key, in, "", false); err == nil {
			t.Fatal("historical key upgraded to another detail")
		}
	}
}
