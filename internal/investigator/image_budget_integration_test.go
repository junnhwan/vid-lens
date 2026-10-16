package investigator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/testsupport"
)

func TestRetainedOverviewRejectsAdditionalFramesBeforeSideEffects(t *testing.T) {
	store, resources, cfg := testsupport.Services(t)
	ctx := context.Background()
	file := filepath.Join(cfg.DevMediaDir, "comparison.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=blue:size=160x90:rate=2", "-t", "90", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", file)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(cfg.DevMediaDir, "comparison.vtt"), []byte("WEBVTT\n\n00:00.000 --> 01:30.000\nA complete blue fixture.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owner, err := store.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := store.CreateRequest(ctx, owner.ID, domain.ID(), "local://comparison.mp4", "", true)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := store.Request(ctx, owner.ID, accepted.ID)
	claim, err := store.Claim(ctx, *req.ActiveRunID, 1, "fixture")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	ms, err := media.New(store, resources, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = ms.Prepare(ctx, *claim, cfg.Profile); err != nil {
		t.Fatal(err)
	}
	req, _ = store.Request(ctx, owner.ID, accepted.ID)
	claim, err = store.Claim(ctx, *req.ActiveRunID, 1, "fixture")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	snap, err := store.Snapshot(ctx, *claim.Run.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{engine: &Engine{Store: store, Media: ms, Resources: resources, Config: cfg}, claim: *claim, profile: cfg.Profile, snapshot: snap, cp: checkpoint{SchemaVersion: 1, Protocol: protocol, Inspections: map[string][]string{}, Repeated: map[string]int{}}}
	if err = s.initial(ctx); err != nil {
		t.Fatal(err)
	}
	if len(s.cp.OverviewIDs) != 12 {
		t.Fatalf("fixture needs twelve overview frames, got %d", len(s.cp.OverviewIDs))
	}
	obs := observation{InspectionID: "overview", FrameIDs: s.cp.OverviewIDs, VisibleFacts: []string{"Blue frames."}, Status: "supported", RetainForComparison: true}
	if err = store.RecordObservation(ctx, *claim, "keep-overview", "overview", obs); err != nil {
		t.Fatal(err)
	}
	if _, err = s.makeTools(); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = store.Pool.QueryRow(ctx, "SELECT count(*) FROM media_assets WHERE run_id=$1", claim.Run.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	args := &schema.ToolArgument{Text: `{"question_id":"detail","question":"Read the middle detail","window":{"start_ms":30000,"end_ms":45000,"count":6,"mode":"detail"}}`}
	result, err := s.tools["inspect_video_window"].InvokableRun(context.WithValue(ctx, callIDKey{}, "more-frames"), args)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(domain.JSON(result))
	if !strings.Contains(wire, "correct_arguments_and_try_again") || !strings.Contains(wire, "retain_for_comparison") {
		t.Fatalf("overflow reached frame delivery instead of returning an actionable tool error: parts=%d", len(result.Parts))
	}
	var after, questions int
	if err = store.Pool.QueryRow(ctx, "SELECT count(*) FROM media_assets WHERE run_id=$1", claim.Run.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err = store.Pool.QueryRow(ctx, "SELECT count(*) FROM evidence_questions WHERE run_id=$1", claim.Run.ID).Scan(&questions); err != nil {
		t.Fatal(err)
	}
	if after != before || questions != 0 {
		t.Fatalf("rejected inspection left side effects: frames %d->%d questions=%d", before, after, questions)
	}
	obs.RetainForComparison = false
	if err = store.RecordObservation(ctx, *claim, "release-overview", "overview", obs); err != nil {
		t.Fatal(err)
	}
	result, err = s.tools["inspect_video_window"].InvokableRun(context.WithValue(ctx, callIDKey{}, "allowed-frames"), args)
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for _, part := range result.Parts {
		if part.Type == schema.ToolPartTypeImage {
			images++
		}
	}
	if images != 6 {
		t.Fatalf("released comparison budget did not allow actual new pixels: %d", images)
	}
}
