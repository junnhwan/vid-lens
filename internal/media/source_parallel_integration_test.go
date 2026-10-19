package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestSourceDownloadsVideoAndAudioTogetherThenPublishesVerifiedTranscript(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	fixture := t.TempDir()
	trace := filepath.Join(fixture, "extractor-arguments.log")
	t.Setenv("EXTRACTOR_TRACE", trace)
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
printf '%%s\n' "$*" >> "$EXTRACTOR_TRACE"
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
	arguments, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	extractions := 0
	for _, line := range strings.Split(strings.TrimSpace(string(arguments)), "\n") {
		if strings.Contains(line, "--dump-single-json") {
			extractions++
			continue
		}
		if !strings.Contains(line, "--load-info-json") || strings.Contains(line, "https://") {
			t.Fatal("video/audio re-extracted the page instead of using the same metadata", line)
		}
	}
	if extractions != 1 {
		t.Fatal("source extraction was repeated", extractions)
	}
}

func TestSourceRejectsTruncatedExtractorVideo(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	fixture := t.TempDir()
	video := filepath.Join(fixture, "short.mp4")
	if out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=blue:size=160x90:rate=1", "-f", "lavfi", "-i", "sine=sample_rate=16000", "-t", "2", "-c:v", "libx264", "-threads", "1", "-c:a", "aac", video).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	script := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in *--dump-single-json*) printf '{\"id\":\"abcdefghijk\",\"title\":\"Twenty second source\",\"duration\":20}';; *) cat %q;; esac\n", video)
	if err := os.WriteFile(filepath.Join(fixture, "yt-dlp"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
	asrCalls := 0
	asr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asrCalls++
		fmt.Fprint(w, `{"text":"Only the first two seconds."}`)
	}))
	defer asr.Close()
	p := cfg.Profile
	p.ASRBaseURL, p.ASRModel = asr.URL, "truncation-fixture"
	if _, err := db.SaveConfig(ctx, p, "", "fixture"); err != nil {
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
	a, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := db.Request(ctx, owner.ID, a.ID)
	c, err := db.Claim(ctx, *req.ActiveRunID, 1, "truncation-fixture")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	s, err := New(db, res, cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Prepare(ctx, *c, p)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "SOURCE_UNAVAILABLE" || asrCalls != 0 {
		t.Fatalf("truncated full-video source reached ASR/publication: error=%v asr_calls=%d", err, asrCalls)
	}
}

func TestSourceRetryKeepsSeparateAudioAttemptsAndClosesOldDownloads(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	// The fake extractor emits tiny strings rather than a multi-GB source.
	cfg.Profile.MaxDownloadBytes, cfg.Profile.WorkspaceBytes = 64<<20, 128<<20
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
