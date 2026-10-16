package exporting

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func exportFixture(t *testing.T) (*store.Store, config.Config, string, domain.Request, string) {
	t.Helper()
	s, _, cfg := testsupport.Services(t)
	ctx := context.Background()
	owner, e := s.NewSession(ctx, domain.ID(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.CreateRequest(ctx, owner.ID, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if e != nil {
		t.Fatal(e)
	}
	r, _ := s.Request(ctx, owner.ID, x.ID)
	c, e := s.Claim(ctx, *r.ActiveRunID, 1, "source")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: "assets/video.mp4", MIME: "video/mp4", SHA256: "fixture", Bytes: 1, ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if e = s.AddAsset(ctx, *c, video); e != nil {
		t.Fatal(e)
	}
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, VideoHash: "fixture", TextHash: "text", DurationMS: 1000, Source: domain.Source{SourceID: c.Run.SourceID, CanonicalURL: "https://youtube.com/watch?v=abcdefghijk"}, Metadata: []byte(`{}`)}
	if _, e = s.FinishSource(ctx, *c, snap, []domain.Segment{}, video.ID); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Request(ctx, owner.ID, x.ID)
	c, e = s.Claim(ctx, *r.ActiveRunID, 1, "article")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{G: 200, A: 255})
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, img)
	b := encoded.Bytes()
	frame := domain.Asset{ID: domain.ID(), SnapshotID: &snap.ID, SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "frame", StorageKey: "assets/frame.png", MIME: "image/png", SHA256: hash(b), Bytes: int64(len(b)), TimeMS: domain.Ptr(int64(500)), RequestedTimeMS: domain.Ptr(int64(500)), Width: 4, Height: 4}
	if e = s.AddAsset(ctx, *c, frame); e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Join(cfg.DataDir, "assets"), 0700); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(cfg.DataDir, frame.StorageKey)
	if e = os.WriteFile(file, b, 0600); e != nil {
		t.Fatal(e)
	}
	cite := domain.Citation{ID: "c", Kind: "video_frame", Label: "真实测试画面", SnapshotID: snap.ID, FrameID: frame.ID, TimeMS: domain.Ptr(int64(500))}
	blocks := []domain.Block{
		{ID: "p", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "正文 *星号* 与 [方括号]", Marks: []string{"bold"}}, {Type: "citation", CitationID: "c"}, {Type: "link", Text: "原视频", URL: snap.Source.CanonicalURL}}},
		{ID: "q", Type: "quote", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "引述\n换行"}}, CitationIDs: []string{"c"}},
		{ID: "l", Type: "list", Origin: "video", Ordered: domain.Ptr(true), Items: []domain.ListItem{{ID: "i", Content: []domain.Inline{{Type: "text", Text: "第一条"}}}}},
		{ID: "code", Type: "code", Origin: "background_explanation", Language: domain.Ptr("go"), Text: "a := `code`\n```"},
		{ID: "table", Type: "table", Origin: "video", Columns: []string{"条件", "结果"}, Rows: []domain.TableRow{{Cells: []string{"a | b", "保留\n换行"}, CitationIDs: []string{"c"}}}},
		{ID: "fig", Type: "figure", Origin: "video", FrameIDs: []string{frame.ID}, Layout: "single", Caption: "测试画面 * 保留图注", CitationIDs: []string{"c"}},
	}
	a := domain.Article{SnapshotID: snap.ID, Source: snap.Source, Title: "导出 / 测试", Sections: []domain.Section{{ID: "main", Title: "正文内容", Blocks: blocks}}, Citations: map[string]domain.Citation{"c": cite}, Limitations: []domain.Limitation{{Code: "not_observed", Message: "仅供测试"}}, Quality: "with_limitations"}
	if e = s.Publish(ctx, *c, a); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Request(ctx, owner.ID, x.ID)
	return s, cfg, owner.ID, r, file
}
func TestCompleteZIPAndMarkdownWithoutAnyModelWork(t *testing.T) {
	s, cfg, owner, r, imageFile := exportFixture(t)
	ctx := context.Background()
	service := &Service{Store: s, Config: cfg}
	before := 0
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE kind IN ('model','asr')").Scan(&before)
	x, reused, e := s.CreateExport(ctx, owner, r.ID, "markdown_zip", "zip-key")
	if e != nil || reused {
		t.Fatal(e)
	}
	re, _, e := s.CreateExport(ctx, owner, r.ID, "markdown_zip", "zip-key")
	if e != nil || !re.Replayed || re.ID != x.ID {
		t.Fatal("export replay changed resource")
	}
	v, e := s.ClaimExport(ctx, x.ID)
	if e != nil || v == nil {
		t.Fatal(e)
	}
	if e = service.Run(ctx, *v); e != nil {
		t.Fatal(e)
	}
	ready, e := s.Export(ctx, owner, r.ID, x.ID)
	if e != nil || ready.Status != "ready" {
		t.Fatalf("%+v %v", ready, e)
	}
	z, e := zip.OpenReader(filepath.Join(cfg.DataDir, *ready.StorageKey))
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	files := map[string][]byte{}
	for _, f := range z.File {
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") {
			t.Fatal("unsafe ZIP entry")
		}
		reader, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(reader)
		reader.Close()
		if e != nil {
			t.Fatal(e)
		}
		files[f.Name] = b
	}
	var manifest struct {
		Files []payload `json:"files"`
	}
	if e = json.Unmarshal(files["manifest.json"], &manifest); e != nil {
		t.Fatal(e)
	}
	for _, f := range manifest.Files {
		if int64(len(files[f.Path])) != f.Bytes || hash(files[f.Path]) != f.SHA256 {
			t.Fatal("offline payload checksum mismatch")
		}
	}
	md := string(files["article.md"])
	for _, text := range []string{"正文内容", "第一条", "引述", "a := `code`", "条件", "测试画面", "还没确认的部分", "images/"} {
		if !strings.Contains(md, text) {
			t.Fatalf("ZIP lost %s", text)
		}
	}
	// MD remains exportable even when an original image file is unavailable.
	if e = os.Remove(imageFile); e != nil {
		t.Fatal(e)
	}
	x, _, e = s.CreateExport(ctx, owner, r.ID, "markdown", "md-key")
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.ClaimExport(ctx, x.ID)
	if e != nil || v == nil {
		t.Fatal(e)
	}
	if e = service.Run(ctx, *v); e != nil {
		t.Fatal(e)
	}
	ready, e = s.Export(ctx, owner, r.ID, x.ID)
	if e != nil || ready.Status != "ready" {
		t.Fatalf("%+v %v", ready, e)
	}
	b, e := os.ReadFile(filepath.Join(cfg.DataDir, *ready.StorageKey))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte("images/")) || bytes.Contains(b, []byte("/api/")) || !bytes.Contains(b, []byte("完整配图请下载图文 ZIP")) {
		t.Fatal("standalone MD has authenticated/dangling images")
	}
	if _, e = s.Export(ctx, "other-session", r.ID, x.ID); e == nil {
		t.Fatal("export leaked across sessions")
	}
	after := 0
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE kind IN ('model','asr')").Scan(&after)
	if before != after {
		t.Fatal("export triggered paid work")
	}
	lease, e := s.ExportDownload(ctx, ready)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = s.Pool.Exec(ctx, "UPDATE article_exports SET expires_at=now()-interval '1 second' WHERE id=$1", ready.ID)
	if e = service.Cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(cfg.DataDir, *ready.StorageKey)); e != nil {
		t.Fatal("cleaner removed an active download")
	}
	if e = s.ReleaseDownload(ctx, lease); e != nil {
		t.Fatal(e)
	}
	if e = service.Cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(cfg.DataDir, *ready.StorageKey)); !os.IsNotExist(e) {
		t.Fatal("expired copy survived cleanup")
	}
}
func TestZIPMissingImageFailsAndUnknownBlockIsRejected(t *testing.T) {
	s, cfg, owner, r, file := exportFixture(t)
	ctx := context.Background()
	_ = os.Remove(file)
	x, _, e := s.CreateExport(ctx, owner, r.ID, "markdown_zip", domain.ID())
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.ClaimExport(ctx, x.ID)
	if e != nil || v == nil {
		t.Fatal(e)
	}
	if e = (&Service{Store: s, Config: cfg}).Run(ctx, *v); e != nil {
		t.Fatal(e)
	}
	out, _ := s.Export(ctx, owner, r.ID, x.ID)
	if out.Status != "failed" || out.Failure == nil || out.Failure.Code != "EXPORT_ASSET_UNAVAILABLE" {
		t.Fatalf("incomplete ZIP was published: %+v", out)
	}
	if _, e = Markdown(domain.Article{Title: "test", Source: domain.Source{CanonicalURL: "https://example.com"}, Sections: []domain.Section{{Blocks: []domain.Block{{Type: "future_type"}}}}}, domain.GenerationModel{}, "", nil); e == nil {
		t.Fatal("unknown block silently lost")
	}
}
