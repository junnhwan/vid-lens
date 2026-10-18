package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestSourceDownloadsVideoAndAudioTogetherThenPublishesVerifiedTranscript(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	fixture := t.TempDir()
	video, audio := filepath.Join(fixture, "video.mp4"), filepath.Join(fixture, "audio.wav")
	for _, args := range [][]string{
		{"-f", "lavfi", "-i", "color=blue:size=160x90:rate=1", "-t", "2", "-c:v", "libx264", "-threads", "1", video},
		{"-f", "lavfi", "-i", "sine=sample_rate=16000", "-t", "2", "-threads", "1", audio},
	} {
		if out, err := exec.Command("ffmpeg", append([]string{"-nostdin", "-v", "error", "-filter_threads", "1"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
 *--dump-single-json*) printf '{"id":"abcdefghijk","title":"Parallel source fixture","duration":2,"subtitles":{},"automatic_captions":{}}';;
 *bestaudio*) sleep 0.6; cat %q;;
 *) sleep 0.6; cat %q;;
esac
`, audio, video)
	if err := os.WriteFile(filepath.Join(fixture, "yt-dlp"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
	asr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"The complete audio source."}`)
	}))
	defer asr.Close()
	cfg.Profile.ASRBaseURL = asr.URL
	cfg.Profile.ASRModel = "source-fixture"
	if _, err := db.SaveConfig(ctx, cfg.Profile, "", "fixture"); err != nil {
		t.Fatal(err)
	}
	_, p, err := db.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := db.Request(ctx, owner.ID, accepted.ID)
	claim, err := db.Claim(ctx, *req.ActiveRunID, 1, "parallel-source")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	ms, err := New(db, res, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = ms.Prepare(ctx, *claim, p); err != nil {
		t.Fatal(err)
	}
	req, err = db.Request(ctx, owner.ID, accepted.ID)
	if err != nil || req.SnapshotID == nil {
		t.Fatal("source not published", err)
	}
	transcript, err := db.Transcript(ctx, *req.SnapshotID)
	if err != nil || len(transcript) != 1 || transcript[0].Origin != "asr" || transcript[0].EndMS != 2000 {
		t.Fatal("missing verified ASR", transcript, err)
	}
	var overlap bool
	err = db.Pool.QueryRow(ctx, `WITH latest AS(SELECT DISTINCT ON(step_id) step FROM run_events WHERE run_id=$1 AND step_id IS NOT NULL ORDER BY step_id,seq DESC) SELECT EXISTS(SELECT 1 FROM latest v,latest a WHERE v.step->>'title'='正在获取用于观察的完整视频' AND a.step->>'title'='正在获取转写所需音轨' AND v.step->>'status'='completed' AND a.step->>'status'='completed' AND (v.step->>'started_at')::timestamptz<(a.step->>'finished_at')::timestamptz AND (a.step->>'started_at')::timestamptz<(v.step->>'finished_at')::timestamptz)`, claim.Run.ID).Scan(&overlap)
	if err != nil || !overlap {
		t.Fatal("source downloads were serialized or not observed", err)
	}
}

func TestSourceRetryKeepsSeparateAudioAttemptsAndClosesOldDownloads(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	fixture := t.TempDir()
	script := `#!/bin/sh
case "$*" in
 *--dump-single-json*) printf '{"id":"abcdefghijk","title":"Retry source fixture","duration":2,"subtitles":{},"automatic_captions":{}}';;
 *bestaudio*) printf 'complete audio';;
 *) /bin/sleep 0.1; printf 'unavailable' >&2; exit 1;;
esac
`
	if err := os.WriteFile(filepath.Join(fixture, "yt-dlp"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
	owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := db.Request(ctx, owner.ID, accepted.ID)
	ms, err := New(db, res, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		claim, err := db.Claim(ctx, *req.ActiveRunID, 1, "retry-source")
		if err != nil || claim == nil {
			t.Fatal("claim", err)
		}
		if err = ms.Prepare(ctx, *claim, cfg.Profile); err == nil {
			t.Fatal("fixture video unexpectedly downloaded")
		}
		if _, err = db.Fail(ctx, *claim, domain.Failure{Code: "SOURCE_UNAVAILABLE", Message: "下载暂时中断。", CanRetry: true}, attempt == 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	var audioSteps, openSteps, retryTitles int
	err = db.Pool.QueryRow(ctx, `WITH latest AS (
		SELECT DISTINCT ON(step_id) step FROM run_events WHERE run_id=$1 AND step_id IS NOT NULL ORDER BY step_id,seq DESC
	) SELECT count(*) FILTER(WHERE step->>'title'='正在获取转写所需音轨'),
		count(*) FILTER(WHERE step->>'status' IN ('running','queued')),
		count(*) FILTER(WHERE step->>'title' LIKE '正在重新准备%第 2 次尝试%') FROM latest`, *req.ActiveRunID).Scan(&audioSteps, &openSteps, &retryTitles)
	if err != nil || audioSteps != 2 || openSteps != 0 || retryTitles != 1 {
		t.Fatalf("source retries overwrote history or kept old downloads live: audio=%d active=%d retry=%d err=%v", audioSteps, openSteps, retryTitles, err)
	}
}
