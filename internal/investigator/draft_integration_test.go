package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/testsupport"
)

// Real PG/Redis, FFmpeg and Eino. The provider emits the incident's exact
// structural defect in a bounded chapter; previously saved chapters survive.
func TestBoundedDraftRepairsBatchAndResumesWithoutRewriting(t *testing.T) {
	for _, fault := range []string{"malformed_batch", "worker_recovery", "final_validation"} {
		t.Run(fault, func(t *testing.T) {
			db, resources, cfg := testsupport.Services(t)
			ctx := context.Background()
			var phase atomic.Int32
			var failed atomic.Int32
			var firstWrites atomic.Int32
			var frames []string
			re := regexp.MustCompile(`frame_id=([a-f0-9]+)`)
			prose := func(id, text string) domain.Section {
				return domain.Section{ID: id, Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: text}, {Type: "citation", CitationID: "t1"}}}}}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages json.RawMessage `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if phase.Load() == 3 && fault == "worker_recovery" && failed.Add(1) <= 2 {
					w.WriteHeader(500)
					return
				}
				n := phase.Add(1) - 1
				name := ""
				var args any
				switch n {
				case 0:
					seen := map[string]bool{}
					for _, m := range re.FindAllStringSubmatch(string(input.Messages), -1) {
						if !seen[m[1]] {
							frames = append(frames, m[1])
							seen[m[1]] = true
						}
					}
					name = "record_observation"
					args = observation{InspectionID: "overview", FrameIDs: frames, VisibleFacts: []string{"画面持续为蓝色。"}, Status: "supported"}
					if fault == "final_validation" {
						args = observation{InspectionID: "overview", FrameIDs: frames, VisibleFacts: []string{"蓝色"}, Status: "supported", Unresolved: []string{"画面以外未知"}}
					}
				case 1:
					name = "begin_article"
					args = beginArticleInput{Title: "蓝色视频说明", Sections: []sectionPlan{{ID: "one", Title: "前半段", StartMS: 0, EndMS: 1000}, {ID: "two", Title: "后半段", StartMS: 1000, EndMS: 2000}}}
				case 2:
					firstWrites.Add(1)
					name = "save_article_sections"
					sec := prose("one", "视频前半段呈现持续的蓝色背景，真实取帧支持这一颜色描述，没有证据说明画面之外的其他事件或物体。")
					sec.Blocks = append(sec.Blocks, domain.Block{Type: "figure", Origin: "video", FrameIDs: []string{"f1"}, Layout: "single", Caption: "实际蓝色画面", CitationIDs: []string{"f1"}})
					args = saveSectionsInput{Version: 1, Sections: []domain.Section{sec}}
				case 3:
					name = "save_article_sections"
					args = saveSectionsInput{Version: 2, Sections: []domain.Section{prose("two", "视频后半段继续展示同样的蓝色画面。视频没有提供更丰富的对象和事件，不能据此推断其他未出现的视觉内容。")}}
				case 4:
					if fault == "malformed_batch" {
						if !strings.Contains(string(input.Messages), "json_syntax") || !strings.Contains(string(input.Messages), "one") {
							t.Error("batch repair lost error or accepted chapter identity")
						}
						name = "save_article_sections"
						args = saveSectionsInput{Version: 2, Sections: []domain.Section{prose("two", "视频后半段继续展示同样的蓝色画面。视频没有提供更丰富的对象和事件，不能据此推断其他未出现的视觉内容。")}}
					} else {
						name = "submit_article_draft"
						args = submitDraftInput{Version: 3}
					}
				case 5:
					if fault == "final_validation" {
						name = "update_article_limitations"
						args = map[string]any{"version": 3, "limitations": []domain.Limitation{{Code: "not_observed", Message: "画面以外内容未知。", SectionIDs: []string{}, CitationIDs: []string{}}}}
					} else {
						name = "submit_article_draft"
						args = submitDraftInput{Version: 3}
					}
				case 6:
					name = "submit_article_draft"
					args = submitDraftInput{Version: 4}
				default:
					t.Error("unexpected extra model call")
					w.WriteHeader(500)
					return
				}
				raw := string(domain.JSON(args))
				if n == 3 && fault == "malformed_batch" {
					raw += `, {"id":"extra"}`
				}
				w.Header().Set("Content-Type", "text/event-stream")
				// Stream tool arguments in multiple deltas, preserving Unicode and call identity.
				runes := []rune(raw)
				for i := 0; i < len(runes); i += 31 {
					delta := map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": string(runes[i:min(i+31, len(runes))])}}}}
					if i == 0 {
						delta["role"] = "assistant"
						delta["tool_calls"] = []any{map[string]any{"index": 0, "id": fmt.Sprint("draft-", n), "type": "function", "function": map[string]any{"name": name, "arguments": string(runes[i:min(i+31, len(runes))])}}}
					}
					fmt.Fprintf(w, "data: %s\n\n", domain.JSON(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}}))
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			cfg.Profile.ModelBaseURL = server.URL
			if _, err := db.SaveConfig(ctx, cfg.Profile, "fixture-key", ""); err != nil {
				t.Fatal(err)
			}
			_, cfg.Profile, _ = db.CurrentConfig(ctx)
			file := filepath.Join(cfg.DevMediaDir, "draft.mp4")
			command := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=blue:size=160x90:rate=25", "-t", "2", "-filter_threads", "1", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", file)
			if b, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, b)
			}
			if err := os.WriteFile(filepath.Join(cfg.DevMediaDir, "draft.vtt"), []byte("WEBVTT\n\n00:00.000 --> 00:02.000\n画面持续为蓝色。\n"), 0600); err != nil {
				t.Fatal(err)
			}
			owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "local://draft.mp4", "", true)
			if err != nil {
				t.Fatal(err)
			}
			req, _ := db.Request(ctx, owner.ID, accepted.ID)
			claim, err := db.Claim(ctx, *req.ActiveRunID, 1, "source")
			if err != nil {
				t.Fatal(err)
			}
			ms, err := media.New(db, resources, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = ms.Prepare(ctx, *claim, cfg.Profile); err != nil {
				t.Fatal(err)
			}
			req, _ = db.Request(ctx, owner.ID, accepted.ID)
			claim, err = db.Claim(ctx, *req.ActiveRunID, 1, "writer")
			if err != nil {
				t.Fatal(err)
			}
			engine := &Engine{Store: db, Media: ms, Resources: resources, Config: cfg}
			err = engine.Run(ctx, *claim, cfg.Profile)
			if fault == "worker_recovery" {
				if err == nil {
					t.Fatal("provider outage did not interrupt writer")
				}
				saved, e := db.ArticleDraft(ctx, claim.Run.ID)
				if e != nil || saved.Version != 2 {
					t.Fatalf("chapter was not persisted before outage: %+v %v", saved, e)
				}
				if _, e = db.Pool.Exec(ctx, "UPDATE job_runs SET lease_expires_at=now()-interval '1 second' WHERE id=$1", claim.Run.ID); e != nil {
					t.Fatal(e)
				}
				next, e := db.Claim(ctx, claim.Run.ID, 1, "new-writer")
				if e != nil || next == nil {
					t.Fatal(e)
				}
				if _, e = db.SaveArticleDraft(ctx, *claim, "stale-writer", map[string]int{}, 2, map[string]int{}); e != domain.ErrFenced {
					t.Fatalf("late draft write escaped fencing: %v", e)
				}
				claim = next
				err = engine.Run(ctx, *claim, cfg.Profile)
			}
			if err != nil {
				t.Fatal(err)
			}
			req, err = db.Request(ctx, owner.ID, accepted.ID)
			if err != nil || req.ArticleID == nil || req.Status != "completed" {
				t.Fatalf("not published: %+v %v", req, err)
			}
			article, err := db.PublishedArticle(ctx, *req.ArticleID)
			if err != nil || len(article.Sections) != 2 || firstWrites.Load() != 1 {
				t.Fatalf("chapters regenerated/lost: %v %d", err, firstWrites.Load())
			}
			var count int
			if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM articles WHERE generation_run_id=$1", claim.Run.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("publication count %d: %v", count, err)
			}
			if fault == "malformed_batch" {
				var invalid int
				if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='candidate_attempt' AND result->>'status'='invalid'`, claim.Run.ID).Scan(&invalid); err != nil || invalid != 1 {
					t.Fatalf("invalid raw batch not retained: %d %v", invalid, err)
				}
			}
		})
	}
}
