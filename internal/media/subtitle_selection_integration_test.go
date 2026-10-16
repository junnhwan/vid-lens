package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestSubtitleAdoptionUsesFrozenMetadataTriesNextTrackAndPersistsDecision(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateRequest(ctx, owner.ID, "subtitle-adoption", "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := db.Request(ctx, owner.ID, a.ID)
	c, err := db.Claim(ctx, *r.ActiveRunID, 1, "subtitle-fixture")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	work := t.TempDir()
	// The actual command wrapper must not re-extract metadata, inspect account
	// cookies, or count a retained video against the isolated subtitle quota.
	script := `#!/bin/sh
case "$*" in *--load-info-json*) ;; *) exit 9;; esac
case "$*" in *--no-cookies*) ;; *) exit 8;; esac
while [ "$#" -gt 0 ]; do
  case "$1" in
    --sub-langs) lang="$2"; shift 2;;
    -o) out="$2"; shift 2;;
    *) shift;;
  esac
done
if [ "$lang" = "zh" ]; then exit 1; fi
sleep 1.1
printf 'WEBVTT\n\n00:00.000 --> 00:04.000\nThe actual fallback subtitle.\n' > "$(dirname "$out")/sub.en.vtt"
`
	if err = os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err = os.WriteFile(filepath.Join(work, "retained-video.mp4"), make([]byte, 17<<20), 0600); err != nil {
		t.Fatal(err)
	}
	infoFile := filepath.Join(work, "info.json")
	if err = os.WriteFile(infoFile, []byte(`{"id":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(db, res, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.sourceMetadata(ctx, *c, metadata{Title: "提前可见的标题", Duration: 4}); err != nil {
		t.Fatal(err)
	}
	view, _, err := db.RequestView(ctx, owner.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	source := view["source"].(*domain.Source)
	if source.Title == nil || *source.Title != "提前可见的标题" || source.DurationMS == nil || *source.DurationMS != 4000 {
		t.Fatal("metadata unavailable until final source publication")
	}
	segs, lang, decision, err := svc.extractorSubtitles(ctx, *c, metadata{Duration: 4, Subtitles: map[string][]subformat{"zh": {{Ext: "vtt"}}}, Auto: map[string][]subformat{"en": {{Ext: "vtt"}}}}, infoFile, work)
	if err != nil || lang == nil || *lang != "en" || len(segs) != 1 || !decision.Usable || len(decision.Attempts) != 2 || decision.Attempts[0].Download != "failed" {
		t.Fatalf("usable subtitle unnecessarily sent to ASR: %+v %v", decision, err)
	}
	diagnostics, err := db.RunDiagnostics(ctx, c.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(domain.JSON(diagnostics))
	if !strings.Contains(wire, "subtitle-adoption-v2") || strings.Contains(wire, "arguments") || strings.Contains(wire, work) || strings.Contains(wire, "test-key") {
		t.Fatal("diagnostics missing evidence or leaking private payload", wire)
	}
	var count int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='asr'", c.Run.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("subtitle retrieval submitted ASR", err)
	}
	if _, err = json.Marshal(decision); err != nil {
		t.Fatal(err)
	}
}
