package media

import (
	"context"
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
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func TestBilibiliPreparationUsesOnlyAnonymousAPIAndSeparatesOldCache(t *testing.T) {
	for _, subtitles := range []bool{true, false} {
		t.Run(fmt.Sprintf("public_subtitles_%v", subtitles), func(t *testing.T) {
			s, resources, cfg := testsupport.Services(t)
			ctx := context.Background()
			var asrCalls atomic.Int32
			asr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asrCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"text":"A complete anonymous source fixture."}`)
			}))
			defer asr.Close()
			cfg.Profile.ASRBaseURL, cfg.Profile.ASRModel = asr.URL+"/audio/transcriptions", "fixture"
			if _, err := s.SaveConfig(ctx, cfg.Profile, "", "fixture-asr-key"); err != nil {
				t.Fatal(err)
			}
			_, profile, err := s.CurrentConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			cfg.CookieFile = "/private/owner-cookie-must-not-be-read.txt"
			// A Bilibili metadata/subtitle/audio path that invokes yt-dlp is a failure.
			fakeDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(fakeDir, "yt-dlp"), []byte("#!/bin/sh\nexit 97\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			video := filepath.Join(t.TempDir(), "fixture.mp4")
			cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=blue:size=160x90:rate=10", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000", "-t", "2", "-filter_threads", "1", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", video)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v %s", err, output)
			}
			payload, err := os.ReadFile(video)
			if err != nil {
				t.Fatal(err)
			}
			client := bilibiliTestClient(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/x/web-interface/view":
					return `{"code":0,"data":{"title":"Anonymous fixture","pages":[{"page":1,"cid":1,"duration":2}]}}`
				case "/x/player/playurl":
					return `{"code":0,"data":{"timelength":2000,"dash":{"video":[{"id":16,"width":160,"height":90,"baseUrl":"https://cdn.example.test/video"}]}}}`
				case "/x/player/v2":
					if subtitles {
						return `{"code":0,"data":{"subtitle":{"subtitles":[{"lan":"zh-CN","subtitle_url":"https://cdn.example.test/subtitle"}]}}}`
					}
					return `{"code":0,"data":{"need_login_subtitle":true}}`
				case "/subtitle":
					return `{"body":[{"from":0,"to":2,"content":"A complete public transcript."}]}`
				case "/video":
					return string(payload)
				default:
					t.Fatalf("unexpected source request %s", r.URL.Path)
					return ""
				}
			})
			owner, err := s.NewSession(ctx, domain.ID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			input := "https://www.bilibili.com/video/BV1bK411W797?p=1"
			accepted, err := s.CreateRequest(ctx, owner.ID, domain.ID(), input, "", false)
			if err != nil {
				t.Fatal(err)
			}
			req, err := s.Request(ctx, owner.ID, accepted.ID)
			if err != nil {
				t.Fatal(err)
			}
			claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "fixture")
			if err != nil || claim == nil {
				t.Fatalf("claim: %v", err)
			}
			ms, err := New(s, resources, cfg)
			if err != nil {
				t.Fatal(err)
			}
			ms.bilibiliHTTP = client
			if err := ms.Prepare(ctx, *claim, profile); err != nil {
				t.Fatal(err)
			}
			req, err = s.Request(ctx, owner.ID, accepted.ID)
			if err != nil || req.Status != "generating" || req.SnapshotID == nil {
				t.Fatalf("prepare: %+v %v", req, err)
			}
			var policy, extractor, kind, availability string
			if err = s.Pool.QueryRow(ctx, "SELECT metadata->>'access_policy',metadata->>'extractor',metadata->>'subtitle_kind',metadata->>'subtitle_availability' FROM source_snapshots WHERE id=$1", *req.SnapshotID).Scan(&policy, &extractor, &kind, &availability); err != nil {
				t.Fatal(err)
			}
			if policy != "anonymous" || extractor != "bilibili_public_api_v1" || kind != "platform_unspecified" || (subtitles && availability != "available") || (!subtitles && availability != "login_required") {
				t.Fatal("anonymous provenance lost")
			}
			if (subtitles && asrCalls.Load() != 0) || (!subtitles && asrCalls.Load() != 1) {
				t.Fatalf("subtitle/ASR fallback incorrect: %d", asrCalls.Load())
			}
			// Simulate a prior authenticated extractor cache using its original key.
			oldPipeline := "source-60s-mp3-48k16k-v1"
			oldKey := req.SourceID + ":prepare-v2:" + store.Hash(map[string]any{"asr_endpoint": profile.ASRBaseURL, "asr_model": profile.ASRModel, "asr_key_ref": profile.ASRKeyRef, "max_video_ms": profile.MaxVideoMS, "max_download_bytes": profile.MaxDownloadBytes, "pipeline": oldPipeline})
			if _, err = s.Pool.Exec(ctx, "UPDATE job_runs SET work_key=$2 WHERE id=$1", claim.Run.ID, oldKey); err != nil {
				t.Fatal(err)
			}
			second, err := s.CreateRequest(ctx, owner.ID, domain.ID(), input, "", false)
			if err != nil {
				t.Fatal(err)
			}
			next, err := s.Request(ctx, owner.ID, second.ID)
			if err != nil || next.SnapshotID != nil || next.ActiveRunID == nil || *next.ActiveRunID == claim.Run.ID {
				t.Fatal("old extractor cache was treated as anonymous")
			}
			var newKey string
			if err = s.Pool.QueryRow(ctx, "SELECT work_key FROM job_runs WHERE id=$1", *next.ActiveRunID).Scan(&newKey); err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(newKey) == oldKey {
				t.Fatal("acquisition policy absent from work identity")
			}
		})
	}
}
