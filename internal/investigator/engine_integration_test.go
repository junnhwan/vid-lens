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
	for _, fault := range []string{"malformed_json", "independent_errors", "observation_arguments", "caller_cancels", "normal_stop", "repeated_stop", "provider_failover"} {
		t.Run(fault, func(t *testing.T) {
			s, resources, cfg := testsupport.Services(t)
			if _, err := exec.LookPath("ffmpeg"); err != nil {
				t.Skip("ffmpeg required")
			}
			var calls atomic.Int32
			var qaMode atomic.Bool
			var qaCalls atomic.Int32
			runCtx, cancelRun := context.WithCancel(context.Background())
			defer cancelRun()
			meta := regexp.MustCompile(`snapshot_id=([a-f0-9]+) inspection_id=\S+ frame_id=([a-f0-9]+) actual_time_ms=([0-9]+)`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Model    string `json:"model"`
					Stream   bool   `json:"stream"`
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
				if qaMode.Load() {
					wantModel, wantAuth := cfg.Profile.ModelName, "Bearer fixture-key"
					if fault == "provider_failover" {
						wantModel, wantAuth = "z-ai/glm-5.3-flash", "Bearer fixture-backup"
					}
					if input.Model != wantModel || r.Header.Get("Authorization") != wantAuth {
						t.Error("Q&A changed the frozen model or credential")
					}
					n := qaCalls.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					if n == 1 {
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"尚无额外证据。\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
						return
					}
					if n != 2 {
						t.Error("Q&A correction exceeded one call")
					}
					if !strings.Contains(string(domain.JSON(input.Messages)), "必须调用") {
						t.Error("Q&A did not receive the formal-submit correction")
					}
					args := domain.JSON(answerInput{Kind: "insufficient_evidence", Summary: "当前视频不足以核实额外细节。", Blocks: []domain.Block{{ID: "answer", Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "text", Text: "当前来源没有提供这项细节，因此不能给出确定结论。"}}}}, Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{}})
					chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "qa-final", "type": "function", "function": map[string]any{"name": "submit_answer", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}}
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
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
				if n == 1 && (fault == "normal_stop" || fault == "repeated_stop") || n == 2 && fault == "repeated_stop" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"材料已经足够。\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					return
				}
				if n == 2 && fault == "normal_stop" && !strings.Contains(string(wire), "必须调用") {
					t.Error("normal stop repair lacks an explicit submission instruction")
				}
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
					if n == 1 && (fault == "malformed_json" || fault == "provider_failover") {
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
			if fault == "provider_failover" {
				cfg.Profile.ModelName = "glm-5.3-flash"
			}
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
			var failedCalls atomic.Int32
			frozenEndpoint := server.URL
			input := domain.RequestInput{VideoURL: "local://blue.mp4"}
			if fault == "provider_failover" {
				failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					failedCalls.Add(1)
					w.WriteHeader(503)
				}))
				defer failed.Close()
				primary := verifiedFixtureRoute(t, s, "primary", failed.URL, "glm-5.3-flash")
				verifiedFixtureRoute(t, s, "backup", server.URL, "z-ai/glm-5.3-flash")
				frozenEndpoint, input.ModelID, input.ModelRevisionID = failed.URL, primary.ID, primary.RevisionID
			}
			accepted, err := s.CreateRequestWithInput(ctx, owner.ID, domain.ID(), input, "", true)
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
			if fault == "repeated_stop" {
				if err == nil || calls.Load() != 3 {
					t.Fatalf("unbounded/missing normal-stop correction: calls=%d error=%v", calls.Load(), err)
				}
				run, _ := s.Run(ctx, claim.Run.ID)
				var cp checkpoint
				_ = json.Unmarshal(run.Checkpoint, &cp)
				if cp.Repairs != 1 || cp.Calls != 3 {
					t.Fatalf("correction budget lost: %+v", cp)
				}
				claim.Run = run
				if again := engine.Run(ctx, *claim, cfg.Profile); again == nil || calls.Load() != 3 {
					t.Fatalf("resume reset correction: calls=%d error=%v", calls.Load(), again)
				}
				return
			}
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
			if fault == "normal_stop" || fault == "provider_failover" {
				qaMode.Store(true)
				var otherCalls atomic.Int32
				other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherCalls.Add(1); w.WriteHeader(500) }))
				defer other.Close()
				changed := cfg.Profile
				changed.ModelBaseURL, changed.ModelName = other.URL, "other-model"
				if _, err = s.SaveConfig(ctx, changed, "other-key", ""); err != nil {
					t.Fatal(err)
				}
				con, _, err := s.EnsureConversation(ctx, owner.ID, req.ID)
				if err != nil {
					t.Fatal(err)
				}
				accepted, err := s.CreateTurn(ctx, owner.ID, con.ID, domain.ID(), domain.QuestionInput{Question: "视频未交代的额外细节是什么？"}, "")
				if err != nil {
					t.Fatal(err)
				}
				turn, err := s.Turn(ctx, owner.ID, con.ID, accepted.ID)
				if err != nil {
					t.Fatal(err)
				}
				qa, err := s.Claim(ctx, turn.RunID, 1, "qa-fixture")
				if err != nil || qa == nil {
					t.Fatal(err)
				}
				frozen, err := s.Profile(ctx, qa.Run.ConfigID)
				if err != nil {
					t.Fatal(err)
				}
				if frozen.ModelBaseURL != frozenEndpoint {
					t.Fatal("Q&A config loaded the current default")
				}
				if err = engine.Run(ctx, *qa, frozen); err != nil {
					t.Fatal(err)
				}
				turn, err = s.Turn(ctx, owner.ID, con.ID, turn.ID)
				if err != nil || turn.Status != "completed" || turn.Answer == nil || qaCalls.Load() != 2 || otherCalls.Load() != 0 {
					t.Fatalf("Q&A finalization/frozen endpoint failed: %+v %v, original=%d other=%d", turn, err, qaCalls.Load(), otherCalls.Load())
				}
				if fault == "provider_failover" && failedCalls.Load() != 2 {
					t.Fatalf("article/Q&A repeatedly called the failed supplier: %d", failedCalls.Load())
				}
			}
		})
	}
}
