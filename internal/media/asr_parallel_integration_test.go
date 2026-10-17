package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestASRBoundedParallelWindowsOrderedProgressAndCheckpointReplay(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	var active, peak, calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for current > peak.Load() {
			old := peak.Load()
			if current <= old || peak.CompareAndSwap(old, current) {
				break
			}
		}
		if n == 1 {
			time.Sleep(400 * time.Millisecond)
		} else {
			time.Sleep(80 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"text":"window %d"}`, n)
	}))
	defer provider.Close()
	cfg.Profile.ASRBaseURL = provider.URL
	cfg.Profile.ASRModel = "parallel-fixture"
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
	accepted, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "local://parallel.mp4", "", true)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := db.Request(ctx, owner.ID, accepted.ID)
	claim, err := db.Claim(ctx, *req.ActiveRunID, 1, "parallel-asr")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	media, err := New(db, res, cfg)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "input.wav")
	if out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000", "-t", "121", "-threads", "1", input).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	progress := []int64{}
	segments, err := media.Transcribe(ctx, *claim, p, input, 121000, t.TempDir(), func(done int64) error { progress = append(progress, done); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 2 || calls.Load() != 3 || len(segments) != 3 || fmt.Sprint(progress) != "[60000 120000 121000]" {
		t.Fatalf("parallelism/order/progress wrong: peak=%d calls=%d segments=%+v progress=%v", peak.Load(), calls.Load(), segments, progress)
	}
	for i, segment := range segments {
		if segment.Ordinal != i || segment.StartMS != int64(i)*60000 {
			t.Fatal("out-of-order segment", segments)
		}
	}
	if _, err = media.Transcribe(ctx, *claim, p, input, 121000, t.TempDir(), nil); err != nil || calls.Load() != 3 {
		t.Fatal("completed ASR windows were resubmitted", err, calls.Load())
	}
}
