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
	for _, fault := range []string{"malformed_batch", "worker_recovery", "final_validation", "long_detailed", "provider_reused_call_id", "duplicate_candidate", "multi_inspect_batch", "plan_repair", "plan_repair_recovery"} {
		t.Run(fault, func(t *testing.T) {
			db, resources, cfg := testsupport.Services(t)
			ctx := context.Background()
			var phase atomic.Int32
			var failed atomic.Int32
			var firstWrites atomic.Int32
			var frames []string
			duration := int64(2000)
			if fault == "long_detailed" {
				duration = 2100000
			}
			re := regexp.MustCompile(`frame_id=([a-f0-9]+)`)
			prose := func(id, text string) domain.Section {
				citation := "t1"
				if fault == "long_detailed" && id == "two" {
					citation = "t2"
				}
				return domain.Section{ID: id, Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: text}, {Type: "citation", CitationID: citation}}}}}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages json.RawMessage `json:"messages"`
					Tools    json.RawMessage `json:"tools"`
					Choice   json.RawMessage `json:"tool_choice"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if ((phase.Load() == 3 && fault == "worker_recovery") || (phase.Load() == 2 && fault == "plan_repair_recovery")) && failed.Add(1) <= 2 {
					w.WriteHeader(500)
					return
				}
				n := phase.Add(1) - 1
				planTurn := false
				if fault == "plan_repair" || fault == "plan_repair_recovery" {
					planTurn = n == 2
					if n > 2 {
						n--
					}
				}
				if fault == "multi_inspect_batch" {
					if n == 1 {
						calls := []any{}
						for i := 0; i < 2; i++ {
							calls = append(calls, map[string]any{"index": i, "id": fmt.Sprint("batch-", i), "type": "function", "function": map[string]any{"name": "inspect_video_window", "arguments": string(domain.JSON(map[string]any{"question_id": fmt.Sprint("batch-", i), "question": "多看一帧", "window": map[string]any{"start_ms": 0, "end_ms": 1000, "count": 1}}))}})
						}
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: %s\n\n", domain.JSON(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": calls}}}}))
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
						return
					}
					if n > 1 {
						n--
					}
				}
				if fault == "duplicate_candidate" && n >= 2 {
					n = 1
				}
				complete := n == 4 && fault != "malformed_batch" || n == 5 && fault == "malformed_batch" || n == 6
				if complete && (!strings.Contains(string(input.Tools), "submit_article_draft") || strings.Contains(string(input.Tools), "update_article_limitations") || len(input.Choice) == 0) {
					t.Errorf("complete draft did not restrict tools and force final submission at phase %d", n)
				}

				name := ""
				var args any
				switch n {
				case 0:
					if fault == "long_detailed" && (!strings.Contains(string(input.Messages), "这是视频结尾的真实字幕") || !strings.Contains(string(input.Messages), "detailed")) {
						t.Error("long run truncated final transcript or lost detail policy")
					}
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
					lastEvidence := "t1"
					if fault == "long_detailed" {
						lastEvidence = "t2"
					}
					args = beginArticleInput{Title: "蓝色视频说明", Sections: []sectionPlan{{ID: "one", Title: "前半段", StartMS: 0, EndMS: duration / 2, KeyPoints: []string{"前半段呈现蓝色"}, EvidenceIDs: []string{"t1"}}, {ID: "two", Title: "后半段", StartMS: duration / 2, EndMS: duration, KeyPoints: []string{"后半段继续为蓝色，不能推断画外物体"}, EvidenceIDs: []string{lastEvidence}}}}
					if fault == "plan_repair" || fault == "plan_repair_recovery" {
						plan := args.(beginArticleInput)
						for len(plan.Sections[0].EvidenceIDs) < 25 {
							plan.Sections[0].EvidenceIDs = append(plan.Sections[0].EvidenceIDs, "t1")
						}
						plan.Sections[1].EvidenceIDs = []string{"t999"}
						args = plan
						if !strings.Contains(string(input.Tools), `"maxItems":24`) {
							t.Error("provider did not receive bounded plan contract")
						}
					}
				case 2:
					if planTurn {
						if !strings.Contains(string(input.Tools), "repair_article_plan") {
							t.Error("rejected plan did not expose the repair tool")
						}
						if !strings.Contains(string(input.Messages), "evidence_count_exceeded") || !strings.Contains(string(input.Messages), "unknown_evidence") {
							t.Error("provider lost structured issues before local plan repair")
						}
						candidate := retainedPlanCandidate(input.Messages)
						name = "repair_article_plan"
						args = candidatePatchInput{CandidateID: candidate["candidate_id"], ExpectedHash: candidate["expected_hash"], Patches: []fieldPatch{
							{Op: "replace", Path: "/sections/0/evidence_ids", Value: domain.JSON([]string{"t1"})},
							{Op: "replace", Path: "/sections/1/evidence_ids", Value: domain.JSON([]string{"t1"})},
						}}
						break
					}
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
				providerCallID := fmt.Sprint("draft-", n)
				if fault == "provider_reused_call_id" {
					providerCallID = "call_0_same_provider_id"
				}
				for i := 0; i < len(runes); i += 31 {
					delta := map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": string(runes[i:min(i+31, len(runes))])}}}}
					if i == 0 {
						delta["role"] = "assistant"
						delta["tool_calls"] = []any{map[string]any{"index": 0, "id": providerCallID, "type": "function", "function": map[string]any{"name": name, "arguments": string(runes[i:min(i+31, len(runes))])}}}
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
			rate := "25"
			if fault == "long_detailed" {
				rate = "2"
			}
			command := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=blue:size=160x90:rate="+rate, "-t", fmt.Sprint(duration/1000), "-filter_threads", "1", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", file)
			if b, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, b)
			}
			vtt := "WEBVTT\n\n00:00.000 --> 00:02.000\n画面持续为蓝色。\n"
			if fault == "long_detailed" {
				vtt = "WEBVTT\n\n00:00.000 --> 17:30.000\n画面持续为蓝色。\n\n17:30.000 --> 35:00.000\n这是视频结尾的真实字幕，后半段继续为蓝色。\n"
			}
			if err := os.WriteFile(filepath.Join(cfg.DevMediaDir, "draft.vtt"), []byte(vtt), 0600); err != nil {
				t.Fatal(err)
			}
			owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			level := "standard"
			if fault == "long_detailed" {
				level = "detailed"
			}
			accepted, err := db.CreateRequestWithInput(ctx, owner.ID, domain.ID(), domain.RequestInput{VideoURL: "local://draft.mp4", DetailLevel: &level}, "", true)
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
			if fault == "duplicate_candidate" {
				policy, _ := domain.InterpretationPolicy(level)
				if err == nil || !strings.Contains(err.Error(), "MODEL_RESULT_INVALID") || phase.Load() != int32(3+policy.RepairLimit) {
					t.Fatalf("duplicate candidate bypassed shared correction budget: calls=%d err=%v", phase.Load(), err)
				}
				draft, e := db.ArticleDraft(ctx, claim.Run.ID)
				if e != nil || draft.Version != 1 {
					t.Fatal("duplicate candidate changed saved draft", e)
				}
				return
			}
			if fault == "worker_recovery" || fault == "plan_repair_recovery" {
				if err == nil {
					t.Fatal("provider outage did not interrupt writer")
				}
				saved, e := db.ArticleDraft(ctx, claim.Run.ID)
				wantedVersion := 2
				if fault == "plan_repair_recovery" {
					wantedVersion = 0
				}
				if e != nil || saved.Version != wantedVersion {
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
			if fault == "multi_inspect_batch" {
				var executed int
				if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='inspect' AND operation_key NOT LIKE 'overview:%'`, claim.Run.ID).Scan(&executed); e != nil || executed != 0 {
					t.Fatalf("multiple inspect response partially executed: %d %v", executed, e)
				}
			}
			var rawPlan []byte
			if e := db.Pool.QueryRow(ctx, `SELECT step->'plan' FROM run_events WHERE run_id=$1 AND step->>'kind'='execution_plan' ORDER BY seq DESC LIMIT 1`, claim.Run.ID).Scan(&rawPlan); e != nil {
				t.Fatal("execution plan did not survive the full pipeline", e)
			}
			var plan domain.ExecutionPlan
			if e := json.Unmarshal(rawPlan, &plan); e != nil || len(plan.Items) < 6 || len(plan.Sections) != 2 || plan.Sections[0].Status != "done" || plan.Items[3].Status != "done" || plan.Items[3].Detail != "已保存 2 / 2 章" {
				t.Fatalf("plan not based on accepted chapters: %+v %v", plan, e)
			}
			if fault == "plan_repair" || fault == "plan_repair_recovery" {
				var repairs int
				if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='draft_correction'`, claim.Run.ID).Scan(&repairs); e != nil || repairs != 1 {
					t.Fatalf("plan repair ledger/recovery changed logical budget: %d %v", repairs, e)
				}
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

func TestIdenticalDraftWriteStopsWithoutCreatingAnotherRevision(t *testing.T) {
	db, _, cfg := testsupport.Services(t)
	ctx := context.Background()
	owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := db.Request(ctx, owner.ID, a.ID)
	c, err := db.Claim(ctx, *req.ActiveRunID, 1, "source")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "hash", MIME: "video/mp4", Bytes: 42, ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if err = db.AddAsset(ctx, *c, video); err != nil {
		t.Fatal(err)
	}
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, DurationMS: 1000, VideoHash: "hash", TextHash: "text", Metadata: []byte(`{}`)}
	if _, err = db.FinishSource(ctx, *c, snap, []domain.Segment{}, video.ID); err != nil {
		t.Fatal(err)
	}
	req, _ = db.Request(ctx, owner.ID, a.ID)
	c, err = db.Claim(ctx, *req.ActiveRunID, 1, "writer")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	s := &session{engine: &Engine{Store: db}, claim: *c, profile: cfg.Profile}
	draft := articleDraft{Title: "saved", Plan: []sectionPlan{{ID: "one", Title: "one", StartMS: 0, EndMS: 1000}}, Sections: map[string]domain.Section{}, Citations: map[string]domain.Citation{}}
	if _, err = s.writeDraft(context.WithValue(ctx, callIDKey{}, "first"), map[string]int{"version": 0}, 0, draft, "save"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.writeDraft(context.WithValue(ctx, callIDKey{}, "same"), map[string]int{"version": 1}, 1, draft, "save"); err == nil {
		t.Fatal("identical revision allowed unproductive loop")
	}
	saved, err := db.ArticleDraft(ctx, c.Run.ID)
	if err != nil || saved.Version != 1 {
		t.Fatalf("no-op changed saved revision: %+v %v", saved, err)
	}
}
