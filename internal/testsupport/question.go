package testsupport

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

// Question prepares durable, isolated source/article/turn rows without models,
// media downloads or evaluation videos, at the historical interactive adapter
// used by existing external-evidence fixtures. CurrentQuestion tests admission.
func Question(t *testing.T, s *store.Store) (domain.Turn, domain.Claim) {
	return question(t, s, false)
}

func CurrentQuestion(t *testing.T, s *store.Store) (domain.Turn, domain.Claim) {
	return question(t, s, true)
}

func question(t *testing.T, s *store.Store, current bool) (domain.Turn, domain.Claim) {
	t.Helper()
	ctx := context.Background()
	owner, e := s.NewSession(ctx, domain.ID(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.CreateRequest(ctx, owner.ID, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Request(ctx, owner.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Claim(ctx, *r.ActiveRunID, 1, "prepare-fixture")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "fixture", MIME: "video/mp4", Bytes: 1, ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if e = s.AddAsset(ctx, *c, video); e != nil {
		t.Fatal(e)
	}
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, VideoHash: "fixture", TextHash: "fixture", DurationMS: 1000, Source: domain.Source{SourceID: c.Run.SourceID, CanonicalURL: r.InputURL}, Metadata: []byte(`{}`)}
	if _, e = s.FinishSource(ctx, *c, snap, []domain.Segment{{Ordinal: 0, StartMS: 0, EndMS: 1000, Text: "fixture text", Origin: "platform_subtitles"}}, video.ID); e != nil {
		t.Fatal(e)
	}
	r, e = s.Request(ctx, owner.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, e = s.Claim(ctx, *r.ActiveRunID, 1, "article-fixture")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	if e = s.Publish(ctx, *c, domain.Article{SnapshotID: snap.ID, Source: snap.Source, Title: "fixture article", Sections: []domain.Section{}, Citations: map[string]domain.Citation{}}); e != nil {
		t.Fatal(e)
	}
	con, _, e := s.EnsureConversation(ctx, owner.ID, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	a, e = s.CreateTurn(ctx, owner.ID, con.ID, domain.ID(), domain.QuestionInput{Question: "Check public reference", AllowExternalSources: true}, "")
	if e != nil {
		t.Fatal(e)
	}
	turn, e := s.Turn(ctx, owner.ID, con.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !current {
		var cp map[string]any
		var raw []byte
		if e = s.Pool.QueryRow(ctx, "SELECT checkpoint FROM job_runs WHERE id=$1", turn.RunID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(raw, &cp); e != nil {
			t.Fatal(e)
		}
		cp["execution_contract"] = domain.ExecutionContract{Version: domain.ExecutionContractV9, RunID: turn.RunID}
		if _, e = s.Pool.Exec(ctx, "UPDATE job_runs SET checkpoint=$2 WHERE id=$1", turn.RunID, domain.JSON(cp)); e != nil {
			t.Fatal(e)
		}
	}
	c, e = s.Claim(ctx, turn.RunID, 1, "reference-fixture")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	return turn, *c
}
