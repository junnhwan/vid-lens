package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestOwnerDeletionUnlinksOriginalsAndKeepsPublishedFrames(t *testing.T) {
	s, r, cfg := testsupport.Services(t)
	ctx := context.Background()
	ms, e := New(s, r, cfg)
	if e != nil {
		t.Fatal(e)
	}
	owner, e := s.NewSession(ctx, domain.ID(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.CreateRequest(ctx, owner.ID, "manual", "local://manual.mp4", "", true)
	if e != nil {
		t.Fatal(e)
	}
	req, e := s.Request(ctx, owner.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Claim(ctx, *req.ActiveRunID, 1, "fixture")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	videoID := domain.ID()
	videoKey := "assets/" + videoID + ".mp4"
	if e = os.WriteFile(filepath.Join(cfg.DataDir, videoKey), []byte("video"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.AddAsset(ctx, *c, domain.Asset{ID: videoID, SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: videoKey, SHA256: "video", MIME: "video/mp4", Bytes: 5}); e != nil {
		t.Fatal(e)
	}
	duration := int64(1000)
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, VideoHash: "video", TextHash: "text", DurationMS: duration, Source: domain.Source{SourceID: c.Run.SourceID, DurationMS: &duration}, Metadata: []byte(`{}`)}
	if _, e = s.FinishSource(ctx, *c, snap, nil, videoID); e != nil {
		t.Fatal(e)
	}
	req, e = s.Request(ctx, owner.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, e = s.Claim(ctx, *req.ActiveRunID, 1, "fixture")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	frameID := domain.ID()
	frameKey := "assets/" + frameID + ".jpg"
	if e = os.WriteFile(filepath.Join(cfg.DataDir, frameKey), []byte("pixels"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.AddAsset(ctx, *c, domain.Asset{ID: frameID, SourceID: c.Run.SourceID, SnapshotID: c.Run.SnapshotID, RunID: c.Run.ID, Kind: "frame", StorageKey: frameKey, SHA256: "pixels", MIME: "image/jpeg", Bytes: 6}); e != nil {
		t.Fatal(e)
	}
	if _, e = ms.DeleteVideo(ctx, req.SourceID, []string{videoID}); e == nil {
		t.Fatal("active original deleted")
	}
	article := domain.Article{SnapshotID: snap.ID, Title: "immutable", Citations: map[string]domain.Citation{"frame": {ID: "frame", Kind: "video_frame", FrameID: frameID}}}
	if e = s.Publish(ctx, *c, article); e != nil {
		t.Fatal(e)
	}
	req, e = s.Request(ctx, owner.ID, a.ID)
	if e != nil || req.ArticleID == nil {
		t.Fatal(e)
	}
	before, e := s.PublishedArticle(ctx, *req.ArticleID)
	if e != nil {
		t.Fatal(e)
	}
	// A nonempty directory at an asset path forces unlink failure without
	// relying on root-dependent permission behavior.
	if e = os.Remove(filepath.Join(cfg.DataDir, videoKey)); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(cfg.DataDir, videoKey), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(cfg.DataDir, videoKey, "busy"), []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	v, e := ms.DeleteVideo(ctx, req.SourceID, []string{videoID})
	if e != nil || v.PendingFiles != 1 || v.DeletedBytes != 0 {
		t.Fatalf("failure: %+v %v", v, e)
	}
	asset, e := s.Asset(ctx, videoID)
	if e != nil || asset.State != "deleting" {
		t.Fatalf("failed unlink not persisted: %+v %v", asset, e)
	}
	if e = os.Remove(filepath.Join(cfg.DataDir, videoKey, "busy")); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(cfg.DataDir, videoKey)); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(cfg.DataDir, videoKey), []byte("video"), 0600); e != nil {
		t.Fatal(e)
	}
	v, e = ms.DeleteVideo(ctx, req.SourceID, []string{videoID})
	if e != nil || v.DeletedFiles != 1 || v.DeletedBytes != 5 {
		t.Fatalf("retry: %+v %v", v, e)
	}
	if _, e = os.Stat(filepath.Join(cfg.DataDir, videoKey)); !os.IsNotExist(e) {
		t.Fatal("original still on disk", e)
	}
	if _, e = os.Stat(filepath.Join(cfg.DataDir, frameKey)); e != nil {
		t.Fatal("published pixels removed", e)
	}
	after, e := s.PublishedArticle(ctx, *req.ArticleID)
	if e != nil || string(domain.JSON(before)) != string(domain.JSON(after)) {
		t.Fatal("immutable article changed", e)
	}
	v, e = ms.DeleteVideo(ctx, req.SourceID, []string{videoID})
	if e != nil || v.DeletedFiles != 0 {
		t.Fatalf("HTTP retry was not safe: %+v %v", v, e)
	}
}
