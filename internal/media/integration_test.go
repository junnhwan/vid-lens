package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestYouTubeMetadataPreparationNeverLoadsOwnerCookies(t *testing.T) {
	s, r, cfg := testsupport.Services(t)
	// This fixture only reaches metadata; reserve a bounded test workspace.
	cfg.Profile.MaxDownloadBytes, cfg.Profile.WorkspaceBytes = 64<<20, 128<<20
	argsFile := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("EXTRACTOR_ARGUMENTS", argsFile)
	fakeExtractor(t, "printf '%s\\n' \"$@\" > \"$EXTRACTOR_ARGUMENTS\"\nprintf '%s\\n' \"ERROR: [youtube] anonfixture: Sign in to confirm you are not a bot\" >&2\nexit 1\n")
	cfg.CookieFile = "/private/owner-cookies.txt"
	ctx := context.Background()
	owner, err := s.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.CreateRequest(ctx, owner.ID, domain.ID(), "https://www.youtube.com/watch?v=anonfixture", "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.Request(ctx, owner.ID, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "test")
	if err != nil || claim == nil {
		t.Fatalf("claim: %v", err)
	}
	ms, err := New(s, r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = ms.Prepare(ctx, *claim, cfg.Profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "SOURCE_ACCESS_RESTRICTED" {
		t.Fatalf("metadata failure: %v", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	text := "\n" + string(args)
	if !strings.Contains(text, "\n--dump-single-json\n") || !strings.Contains(text, "\n--no-cookies\n") || !strings.Contains(text, "\n--no-cookies-from-browser\n") || strings.Contains(text, cfg.CookieFile) {
		t.Fatal("metadata preparation did not enforce anonymous acquisition")
	}
}

func TestVerifiedASRReuseRequiresExactBytesAndProviderConfiguration(t *testing.T) {
	for _, change := range []string{"video_bytes", "asr_model"} {
		t.Run(change, func(t *testing.T) {
			s, r, cfg := testsupport.Services(t)
			if _, err := exec.LookPath("ffmpeg"); err != nil {
				t.Skip("ffmpeg required")
			}
			var calls atomic.Int32
			asr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"text":"The same verified test video."}`)
			}))
			defer asr.Close()
			cfg.Profile.ASRBaseURL, cfg.Profile.ASRModel = asr.URL+"/audio/transcriptions", "asr-a"
			ctx := context.Background()
			if _, err := s.SaveConfig(ctx, cfg.Profile, "", "asr-test-key"); err != nil {
				t.Fatal(err)
			}
			_, storedProfile, err := s.CurrentConfig(ctx)
			cfg.Profile = storedProfile
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(cfg.DevMediaDir, "sample.mp4")
			cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=blue:size=160x90:rate=10", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000", "-t", "1", "-filter_threads", "1", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", file)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, b)
			}
			owner, err := s.NewSession(ctx, domain.ID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			ms, err := New(s, r, cfg)
			if err != nil {
				t.Fatal(err)
			}
			prepare := func() string {
				t.Helper()
				accepted, err := s.CreateRequest(ctx, owner.ID, domain.ID(), "local://sample.mp4", "", true)
				if err != nil {
					t.Fatal(err)
				}
				req, err := s.Request(ctx, owner.ID, accepted.ID)
				if err != nil {
					t.Fatal(err)
				}
				claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "test")
				if err != nil || claim == nil {
					t.Fatalf("claim %v", err)
				}
				if err = ms.Prepare(ctx, *claim, cfg.Profile); err != nil {
					t.Fatal(err)
				}
				req, err = s.Request(ctx, owner.ID, accepted.ID)
				if err != nil || req.SnapshotID == nil {
					t.Fatalf("prepared %v", err)
				}
				return *req.SnapshotID
			}
			first, second := prepare(), prepare()
			if calls.Load() != 1 || first != second {
				t.Fatalf("verified source retranscribed: submissions=%d same_snapshot=%v", calls.Load(), first == second)
			}
			if change == "video_bytes" {
				f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.Write([]byte{0, 0, 0, 0})
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				cfg.Profile.ASRModel = "asr-b"
				if _, err = s.SaveConfig(ctx, cfg.Profile, "", ""); err != nil {
					t.Fatal(err)
				}
				_, cfg.Profile, err = s.CurrentConfig(ctx)
				if err != nil {
					t.Fatal(err)
				}
			}
			prepare()
			if calls.Load() != 2 {
				t.Fatalf("changed identity reused old ASR: submissions=%d", calls.Load())
			}
		})
	}
}

func TestASRWindowProgressSurvivesReconciliation(t *testing.T) {
	s, r, cfg := testsupport.Services(t)
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	ctx := context.Background()
	var runID atomic.Value
	var calls atomic.Int32
	handlerErrors := make(chan error, 2)
	asr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if calls.Add(1) > 1 {
			id := runID.Load().(string)
			err := s.Repair(ctx, id, 1, "active", nil)
			if err == nil {
				var status string
				err = s.Pool.QueryRow(ctx, "SELECT status FROM job_runs WHERE id=$1", id).Scan(&status)
				if err == nil && status != "running" {
					err = fmt.Errorf("reconciliation stopped productive transcription: %s", status)
				}
			}
			if err != nil {
				handlerErrors <- err
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		// Compress a long transcription into a fast deterministic test: each
		// completed window arrives after five minutes of unchanged phase title.
		if _, err := s.Pool.Exec(ctx, "UPDATE job_runs SET updated_at=now()-interval '6 minutes' WHERE id=$1", runID.Load().(string)); err != nil {
			handlerErrors <- err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"A completed audio window."}`)
	}))
	defer asr.Close()
	cfg.Profile.ASRBaseURL, cfg.Profile.ASRModel = asr.URL, "asr-test"
	if _, err := s.SaveConfig(ctx, cfg.Profile, "", "asr-test-key"); err != nil {
		t.Fatal(err)
	}
	_, p, err := s.CurrentConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cfg.DevMediaDir, "progress.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=blue:size=160x90:rate=1", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000", "-t", "61", "-filter_threads", "1", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", file)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	owner, err := s.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.CreateRequest(ctx, owner.ID, "progress", "local://progress.mp4", "", true)
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.Request(ctx, owner.ID, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "transcriber")
	if err != nil || claim == nil {
		t.Fatalf("claim: %v", err)
	}
	runID.Store(claim.Run.ID)
	ms, err := New(s, r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = ms.Prepare(ctx, *claim, p); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-handlerErrors:
		t.Fatal(err)
	default:
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two actual audio windows, got %d", calls.Load())
	}
	if err = s.Repair(ctx, claim.Run.ID, 1, "active", nil); err != nil {
		t.Fatal(err)
	}
	req, err = s.Request(ctx, owner.ID, accepted.ID)
	if err != nil || req.Status != "generating" {
		t.Fatalf("transcription was not allowed to finish: %+v %v", req, err)
	}
	var updates, stepIDs int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT step_id) FROM run_events WHERE run_id=$1 AND step->>'status'='running' AND step->>'title' LIKE '正在转写音频（% / %）'`, claim.Run.ID).Scan(&updates, &stepIDs); err != nil {
		t.Fatal(err)
	}
	if updates != 2 || stepIDs != 1 {
		t.Fatalf("transcription did not update one persisted activity step per completed window: updates=%d step_ids=%d", updates, stepIDs)
	}
}
