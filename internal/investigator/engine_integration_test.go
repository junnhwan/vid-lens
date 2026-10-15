package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/testsupport"
)

// The model service is a fixture; PostgreSQL, Redis, FFmpeg, the Eino HTTP
// adapter, candidate repair and immutable publication all use their real paths.
func TestEinoCandidateRepairPublishesOnceWithRealEvidence(t *testing.T) {
	for _, fault := range []string{"malformed_json", "independent_errors", "observation_arguments", "caller_cancels"} {
		t.Run(fault, func(t *testing.T) {
			s, resources, cfg := testsupport.Services(t)
			if _, err := exec.LookPath("ffmpeg"); err != nil {
				t.Skip("ffmpeg required")
			}
			var calls atomic.Int32
			runCtx, cancelRun := context.WithCancel(context.Background())
			defer cancelRun()
			meta := regexp.MustCompile(`snapshot_id=([a-f0-9]+) inspection_id=\S+ frame_id=([a-f0-9]+) actual_time_ms=([0-9]+)`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Stream   bool `json:"stream"`
					Messages []struct {
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if !input.Stream {
					t.Error("model invocation did not stream")
					w.WriteHeader(400)
					return
				}
				frames := []string{}
				times := map[string]int64{}
				snapshot := ""
				wire, _ := json.Marshal(input.Messages)
				for _, match := range meta.FindAllStringSubmatch(string(wire), -1) {
					if _, ok := times[match[2]]; ok {
						continue
					}
					snapshot = match[1]
					frames = append(frames, match[2])
					times[match[2]], _ = strconv.ParseInt(match[3], 10, 64)
				}
				// Recorded images are reference-only in later calls. The first
				// image metadata stays as text and continues to carry exact IDs/times.
				if snapshot == "" || len(frames) == 0 {
					t.Error("model did not receive source-bound frame metadata")
					w.WriteHeader(400)
					return
				}
				n := calls.Add(1) - 1
				if fault == "caller_cancels" {
					cancelRun()
					<-r.Context().Done()
					return
				}
				name := "submit_article"
				args := ""
				if n == 0 || fault == "observation_arguments" && n == 1 {
					name = "record_observation"
					args = string(domain.JSON(observation{InspectionID: "overview", FrameIDs: frames, VisibleFacts: []string{"画面为纯蓝色，未显示其他对象。"}, Status: "supported"}))
					if fault == "observation_arguments" && n == 0 {
						args = `{"inspection_id":"overview","unresolved":"not an array","status":"completed"}`
					}
				} else {
					citation := domain.Citation{ID: "frame", Kind: "video_frame", Label: "实际蓝色画面", SnapshotID: snapshot, FrameID: frames[0], TimeMS: domain.Ptr(times[frames[0]])}
					candidate := articleInput{Title: "测试视频的蓝色画面", Sections: []domain.Section{{ID: "main", Title: "画面内容", Blocks: []domain.Block{
						{ID: "prose", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "这段短视频持续展示蓝色画面，取自视频的实际图像支持这一描述。它没有展示其他对象，也不能据此推断视频以外的事件。"}, {Type: "citation", CitationID: "frame"}}},
						{ID: "figure", Type: "figure", Origin: "video", FrameIDs: []string{frames[0]}, Layout: "single", Caption: "视频中的实际蓝色画面", CitationIDs: []string{"frame"}},
					}}}, Citations: map[string]domain.Citation{"frame": citation}, Limitations: []domain.Limitation{}}
					if n == 1 && fault == "independent_errors" {
						for _, id := range []string{"quote_a", "quote_b"} {
							candidate.Sections[0].Blocks = append(candidate.Sections[0].Blocks, domain.Block{ID: id, Type: "quote", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "缺少引用的引述"}}})
						}
					} else if n == 2 && fault == "independent_errors" && (!strings.Contains(string(wire), "quote_a") || !strings.Contains(string(wire), "quote_b")) {
						t.Error("repair did not receive both block failures")
					}
					args = string(domain.JSON(candidate))
					if n == 1 && fault == "malformed_json" {
						args = args[:len(args)-1]
					}
					if n > 2 {
						t.Error("more than one candidate repair")
						w.WriteHeader(400)
						return
					}
				}
				chunk := map[string]any{"id": "fixture-response", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call-%d", n), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
			}))
			defer server.Close()
			cfg.Profile.ModelBaseURL = server.URL
			ctx := context.Background()
			if _, err := s.SaveConfig(ctx, cfg.Profile, "fixture-key", ""); err != nil {
				t.Fatal(err)
			}
			_, storedProfile, err := s.CurrentConfig(ctx)
			cfg.Profile = storedProfile
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(cfg.DevMediaDir, "blue.mp4")
			cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=blue:size=160x90:rate=25", "-t", "2", "-filter_threads", "1", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", file)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, b)
			}
			if err := os.WriteFile(filepath.Join(cfg.DevMediaDir, "blue.vtt"), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:02.000\n画面持续为蓝色。\n"), 0600); err != nil {
				t.Fatal(err)
			}
			owner, err := s.NewSession(ctx, domain.ID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := s.CreateRequest(ctx, owner.ID, domain.ID(), "local://blue.mp4", "", true)
			if err != nil {
				t.Fatal(err)
			}
			req, _ := s.Request(ctx, owner.ID, accepted.ID)
			claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "fixture")
			if err != nil || claim == nil {
				t.Fatal(err)
			}
			ms, err := media.New(s, resources, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = ms.Prepare(ctx, *claim, cfg.Profile); err != nil {
				t.Fatal(err)
			}
			req, _ = s.Request(ctx, owner.ID, accepted.ID)
			claim, err = s.Claim(ctx, *req.ActiveRunID, 1, "fixture")
			if err != nil || claim == nil {
				t.Fatal(err)
			}
			engine := &Engine{Store: s, Media: ms, Resources: resources, Config: cfg}
			err = engine.Run(runCtx, *claim, cfg.Profile)
			if fault == "caller_cancels" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("interrupted ownership became an incompatible-provider failure: %v", err)
				}
				op, err := s.Operation(ctx, claim.Run.ID, "model:0")
				if err != nil || op.Status == "completed" || op.Submissions != 1 {
					t.Fatalf("canceled remote outcome was finalized or lost: %+v %v", op, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			req, err = s.Request(ctx, owner.ID, accepted.ID)
			if err != nil || req.Status != "completed" || req.ArticleID == nil || calls.Load() != 3 {
				t.Fatalf("publication status=%s calls=%d error=%v", req.Status, calls.Load(), err)
			}
			run, err := s.Run(ctx, claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(run.Checkpoint), "base64_data") {
				t.Fatal("checkpoint retained pixels")
			}
			var published int
			if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM articles WHERE generation_run_id=$1", claim.Run.ID).Scan(&published); err != nil || published != 1 {
				t.Fatalf("published %d times: %v", published, err)
			}
		})
	}
}
