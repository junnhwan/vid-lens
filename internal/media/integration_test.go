package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestRealFrameTimesCropReloadAndCleanup(t *testing.T) {
	s, r, cfg := testsupport.Services(t)
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg required")
	}
	ctx := context.Background()
	file := filepath.Join(cfg.DevMediaDir, "sample.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=25", "-t", "2", "-filter_threads", "1", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", file)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, b)
	}
	owner, e := s.NewSession(ctx, domain.ID(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	accepted, e := s.CreateRequest(ctx, owner.ID, "media", "local://sample.mp4", "", true)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := s.Request(ctx, owner.ID, accepted.ID)
	claim, e := s.Claim(ctx, *req.ActiveRunID, 1, "test")
	if e != nil || claim == nil {
		t.Fatal(e)
	}
	ms, e := New(s, r, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = ms.Prepare(ctx, *claim, cfg.Profile); e != nil {
		t.Fatal(e)
	}
	req, e = s.Request(ctx, owner.ID, accepted.ID)
	if e != nil || req.Status != "generating" {
		t.Fatalf("%+v %v", req, e)
	}
	claim, e = s.Claim(ctx, *req.ActiveRunID, 1, "test")
	if e != nil || claim == nil {
		t.Fatal(e)
	}
	ins, e := ms.Inspect(ctx, *claim, "real-window", "Compare ordered frames", Window{StartMS: 1111, EndMS: 1333, Count: 2}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(ins.FrameIDs) != 2 {
		t.Fatal(ins)
	}
	for _, id := range ins.FrameIDs {
		a, b, e := ms.ReadImage(ctx, *claim, id)
		if e != nil || len(b) == 0 || *a.TimeMS < *a.RequestedTimeMS || *a.TimeMS-*a.RequestedTimeMS > 41 {
			t.Fatalf("actual PTS mismatch: %+v %v", a, e)
		}
	}
	parent, _ := s.Asset(ctx, ins.FrameIDs[0])
	crop, e := ms.Inspect(ctx, *claim, "crop", "Read detail", Window{Crop: &domain.Crop{ParentFrameID: parent.ID, X: 20, Y: 30, Width: 100, Height: 80}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	a, b, e := ms.ReadImage(ctx, *claim, crop.FrameIDs[0])
	if e != nil || a.Width != 100 || a.Height != 80 || len(b) == 0 || a.TimeMS == nil || *a.TimeMS != *parent.TimeMS {
		t.Fatalf("crop: %+v %v", a, e)
	}
	p, _ := ms.Path(parent.StorageKey)
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if _, _, e = ms.ReadImage(ctx, *claim, parent.ID); !errors.Is(e, ErrMediaMissing) {
		t.Fatal("missing pixels masqueraded as loaded")
	}
	fresh, b, e := ms.Reload(ctx, *claim, parent.ID, "test-reload")
	if e != nil || fresh.ID == parent.ID || fresh.ReplacesFrameID == nil || *fresh.ReplacesFrameID != parent.ID || len(b) == 0 {
		t.Fatalf("rematerialization: %+v %v", fresh, e)
	}
	if e = s.CancelRequest(ctx, owner.ID, accepted.ID); e != nil {
		t.Fatal(e)
	}
	run, _ := s.Run(ctx, claim.Run.ID)
	if e = s.Repair(ctx, run.ID, run.DispatchGeneration, "unknown", nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE media_assets SET expires_at=now()-interval '1 second' WHERE run_id=$1", claim.Run.ID); e != nil {
		t.Fatal(e)
	}
	if e = ms.Cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	a, e = s.Asset(ctx, fresh.ID)
	if e != nil || a.State != "deleted" {
		t.Fatalf("cleanup: %+v %v", a, e)
	}
}
