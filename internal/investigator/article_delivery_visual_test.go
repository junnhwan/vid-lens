package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func visualTextWrite() loopSaveInput {
	in := loopGoodWrite()
	in.Sections[0].Content = in.Sections[0].Content[:1]
	return in
}

func TestDeliveryVisualSaveBeforeCaptureFailureStillPublishes(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if calls.Add(1) == 1 {
			in := visualTextWrite()
			in.Finish = false
			in.VisualRequests = []deliveryVisualRequest{{Key: "board", SectionID: "main", Question: "本章中的板书", StartMS: 0, EndMS: 1000}}
			writeReviewWireArgs(w, loopSaveTool, in)
			return
		}
		var records []deliveryVisualRecord
		if json.Unmarshal(p["visual_requests"], &records) != nil || len(records) != 1 || records[0].Status != "capture_unavailable_prose_preserved" {
			t.Error("failed capture not returned as optional", string(p["visual_requests"]))
		}
		var version int
		_ = json.Unmarshal(p["draft_version"], &version)
		if version != 1 {
			t.Error("prose was not saved before screenshot", version)
		}
		writeReviewWireArgs(w, loopSaveTool, loopSaveInput{ExpectedVersion: version, Finish: true, Sections: []loopSection{}})
	}, domain.ArticleDeliveryV6)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, _ := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if run.Status != "succeeded" || calls.Load() != 2 {
		t.Fatalf("optional failure blocked delivery: %+v calls=%d", run, calls.Load())
	}
}

func TestDeliveryVisualPixelsOnceAndPlacementWithoutRewriting(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		var pixels []any
		_ = json.Unmarshal(p["delivered_pixels"], &pixels)
		if calls.Add(1) == 1 {
			if len(pixels) != 1 {
				t.Error("initial real pixels missing", string(p["delivered_pixels"]))
			}
			in := visualTextWrite()
			in.Finish = false
			writeReviewWireArgs(w, loopSaveTool, in)
			return
		}
		if len(pixels) != 0 {
			t.Error("overview pixels repeated on another writing call")
		}
		var a domain.Article
		_ = json.Unmarshal(p["current_article"], &a)
		var version int
		_ = json.Unmarshal(p["draft_version"], &version)
		// current_article is a reading projection; use the stable chapter from
		// the program's deterministic creation identity returned in the receipt.
		var feedback struct {
			Receipt *loopReceipt `json:"receipt"`
		}
		_ = json.Unmarshal(p["feedback"], &feedback)
		id := ""
		if feedback.Receipt != nil {
			id = feedback.Receipt.Created["main"]
		}
		if id == "" && len(a.Sections) > 0 {
			id = a.Sections[0].ID
		}
		writeReviewWireArgs(w, loopSaveTool, loopSaveInput{ExpectedVersion: version, Finish: true, Sections: []loopSection{}, FigurePlacements: []deliveryFigurePlacement{{SectionID: id, Figure: loopFigure{Frames: []string{"f1"}, Caption: "合成黑色画面。", VisibleFacts: []string{"黑色画面"}}}}})
	}, domain.ArticleDeliveryV6)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, _ := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if run.ResultID == nil {
		t.Fatal("not published")
	}
	a, err := s.engine.Store.PublishedArticle(context.Background(), *run.ResultID)
	if err != nil || len(a.Sections) != 1 || len(a.Sections[0].Blocks) != 2 || a.Sections[0].Blocks[1].Type != "figure" {
		t.Fatalf("placement lost prose or figure: %+v %v", a, err)
	}
	usage, err := s.engine.Store.ArticleVisualUsage(context.Background(), s.claim.Run.ID)
	if err != nil || usage.Images != 1 {
		t.Fatalf("repeated billed image input: %+v %v", usage, err)
	}
}

func visualVideo(t *testing.T, s *session) {
	t.Helper()
	file := filepath.Join(s.engine.Config.DataDir, "visual-test.mp4")
	out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-t", "6", "-filter_threads", "1", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv420p", file).CombinedOutput()
	if err != nil {
		t.Fatalf("synthetic source: %v %s", err, out)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(b)
	asset := domain.Asset{ID: domain.ID(), SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID, Kind: "video", StorageKey: "visual-test.mp4", SHA256: hex.EncodeToString(digest[:]), MIME: "video/mp4", Bytes: int64(len(b))}
	if err = s.engine.Store.AddAsset(context.Background(), s.claim, asset); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryVisualTimeRefinementIsActualAndDurablyBounded(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model expected") }, domain.ArticleDeliveryV6)
	ctx := context.Background()
	s.snapshot.DurationMS = 6000
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE source_snapshots SET duration_ms=6000 WHERE id=$1", s.snapshot.ID); err != nil {
		t.Fatal(err)
	}
	visualVideo(t, s)
	r, err := s.captureDeliveryVisual(ctx, "first", deliveryVisualRequest{Key: "diagram", Question: "板书关键状态", StartMS: 0, EndMS: 3000}, false)
	if err != nil || len(r.Frames) != 3 || r.Status != "captured" {
		t.Fatalf("initial extraction %+v %v", r, err)
	}
	parent := r.Frames[1]
	s.cp.ArticleLoop.SeenFrames = appendUnique(s.cp.ArticleLoop.SeenFrames, parent)
	alias := fmt.Sprint("f", slices.Index(s.cp.ArticleLoop.Frames, parent)+1)
	refine := deliveryVisualRequest{Key: "diagram", Question: "画面过早，向后微调", StartMS: 2000, EndMS: 4000, RefineFrom: alias, Detail: true, ActivityTitle: "向后微调至板书写完"}
	second, err := s.captureDeliveryVisual(ctx, "refine", refine, false)
	if err != nil || len(second.Frames) != 1 || !second.Refinement || second.Window.Mode != "detail" {
		t.Fatalf("refinement %+v %v", second, err)
	}
	a, err := s.engine.Store.Asset(ctx, second.Frames[0])
	if err != nil || a.TimeMS == nil || *a.TimeMS < 2000 || *a.TimeMS > 4000 {
		t.Fatalf("actual decoder timeline %+v %v", a, err)
	}
	var refined domain.Step
	var event []byte
	if err = s.engine.Store.Pool.QueryRow(ctx, `SELECT step FROM run_events WHERE run_id=$1 AND step->>'title'=$2 AND step->>'status'='completed' ORDER BY seq DESC LIMIT 1`, s.claim.Run.ID, "微调取帧：向后微调至板书写完").Scan(&event); err != nil {
		t.Fatal("AI refinement heading missing", err)
	}
	if json.Unmarshal(event, &refined) != nil || refined.SourceRange == nil || refined.SourceRange.StartMS != 2000 || len(refined.FrameIDs) != 1 {
		t.Fatal("refinement heading lost actual timeline or frames")
	}
	if err = s.persist(ctx, nil); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	third, err := s.captureDeliveryVisual(ctx, "again", refine, false)
	if err != nil || third.Status != "goal_refinement_limit" || third.Attempted {
		t.Fatalf("refinement cap reset after reload: %+v %v", third, err)
	}
	before := len(s.cp.ArticleLoop.Visual.Records)
	_, err = s.captureDeliveryVisual(ctx, "refine", refine, false)
	if err != nil || len(s.cp.ArticleLoop.Visual.Records) != before {
		t.Fatal("replayed extraction consumed quota", err)
	}
}

func TestDeliveryVisualBudgetReservesProseAndFinishIgnoresSearch(t *testing.T) {
	for _, fault := range []string{"calls", "time", "images", "interrupted"} {
		t.Run(fault, func(t *testing.T) {
			s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no provider expected") }, domain.ArticleDeliveryV6)
			ctx := context.Background()
			req := deliveryVisualRequest{Key: "board", Question: "可选板书", StartMS: 0, EndMS: 1000}
			want := ""
			switch fault {
			case "calls":
				s.cp.Calls = 18
				want = "prose_calls_reserved"
			case "time":
				s.cp.ArticleLoop.Budget.ActiveSeconds = 10
				want = "prose_time_reserved"
			case "images":
				s.cp.ArticleLoop.Visual.PixelsClosed = true
				want = "image_input_limit"
			case "interrupted":
				_, err := s.engine.Store.BeginOperation(ctx, s.claim, "article-visual:attempt", "article_visual", req, false)
				if err != nil {
					t.Fatal(err)
				}
				want = "interrupted_capture_omitted"
			}
			r, err := s.captureDeliveryVisual(ctx, "attempt", req, false)
			if err != nil || r.Status != want {
				t.Fatalf("missing budget fallback %+v %v", r, err)
			}
		})
	}
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		calls.Add(1)
		in := visualTextWrite()
		in.VisualRequests = []deliveryVisualRequest{{Key: "last", SectionID: "main", Question: "最后找一张", StartMS: 0, EndMS: 1000}}
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.ArticleDeliveryV6)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, _ := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if run.Status != "succeeded" || calls.Load() != 1 {
		t.Fatal("finish was delayed for a new image", run.Status, calls.Load())
	}
}

func TestDeliveryVisualInvalidPlacementCannotDiscardProse(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		in := visualTextWrite()
		in.FigurePlacements = []deliveryFigurePlacement{{SectionID: "main", Figure: loopFigure{Frames: []string{"f999"}, Caption: "不存在的图", VisibleFacts: []string{"不应接受"}}}}
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.ArticleDeliveryV6)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, _ := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if run.Status != "succeeded" {
		t.Fatal("optional invalid placement blocked prose", run.Status)
	}
}

func TestDeliveryVisualStandaloneLimitSurvivesReload(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no provider expected") }, domain.ArticleDeliveryV6)
	ctx := context.Background()
	if err := s.prepareLoopFrames(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.viewDeliveryFrames(ctx, articleLoopCall{Slot: i}, loopViewInput{Question: "检查已有画面", FrameIDs: []string{"f1"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.persist(ctx, nil); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	_, err := s.viewDeliveryFrames(ctx, articleLoopCall{Slot: 3}, loopViewInput{Question: "继续找图", StartMS: 0, EndMS: 1000})
	if err != nil || len(s.cp.ArticleLoop.Visual.StandaloneSlots) != 3 {
		t.Fatal("unbounded standalone views", err)
	}
	infos, err := s.loopCallTools(articleLoopCall{Mode: "author", Sources: s.loopAllSourceAliases()})
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name == "view_frames" {
			t.Fatal("exhausted standalone tool still offered")
		}
	}
}

func TestDeliveryVisualProseReserveRemovesStandaloneToolBeforeCalling(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no provider expected") }, domain.ArticleDeliveryV6)
	s.cp.Calls = 18
	p, _, infos, err := s.prepareLoopCall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.OptionalVisualAllowed == nil || *p.OptionalVisualAllowed {
		t.Fatal("prose reserve not frozen into the actual call")
	}
	for _, info := range infos {
		if info.Name == "view_frames" {
			t.Fatal("optional tool could consume the last prose slots")
		}
	}
}

func TestDeliveryVisualPhysicalBudgetAfterFreezeContinuesTextWithoutSendingPixels(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		var images []any
		_ = json.Unmarshal(p["delivered_pixels"], &images)
		if len(images) > 0 {
			t.Error("exhausted visual budget still sent pixels")
		}
		in := visualTextWrite()
		_ = json.Unmarshal(p["draft_version"], &in.ExpectedVersion)
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.ArticleDeliveryV6)
	ctx := context.Background()
	p, input, infos, err := s.prepareLoopCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Frames) == 0 {
		t.Fatal("fixture did not freeze actual pixels")
	}
	args := map[string]any{"messages": stripPixels(input), "budget": s.articleCallBudget, "tools_hash": digest(domain.JSON(infos)), "policy_revision": domain.ArticleLoopPolicy, "parallel_tool_calls": false}
	if _, err = s.freezeModelInput(ctx, "model:0", input, args, s.articleCallBudget, infos); err != nil {
		t.Fatal(err)
	}
	s.cp.ArticleLoop.Pending = p
	s.cp.Calls = 1
	// A physical send with an unknown result must still exhaust the image
	// allowance, even though no completion receipt/checkpoint was produced.
	op, err := s.engine.Store.BeginModelSubmission(ctx, s.claim, "unknown-visual-send", map[string]any{"fixture": true}, s.claim.Run.ConfigID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.RecordSubmission(ctx, s.claim, "unknown-visual-send", op.Submissions, map[string]any{"budget": callBudget{ImageCount: deliveryVisualImages, EstimatedImageTokens: deliveryVisualTokens, ImageBytes: deliveryVisualBytes}}); err != nil {
		t.Fatal(err)
	}
	if err = s.persist(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil || run.Status != "succeeded" || calls.Load() != 1 {
		t.Fatalf("visual cap blocked text: %+v calls=%d err=%v", run, calls.Load(), err)
	}
	stats, err := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
	if err != nil || stats.Submissions != 2 {
		t.Fatalf("refused pixels reserved another physical send: %+v %v", stats, err)
	}
}

func TestDeliveryVisualMalformedOptionalSidecarStillPublishesProse(t *testing.T) {
	for _, field := range []string{"visual_requests", "figure_placements"} {
		t.Run(field, func(t *testing.T) {
			s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				_ = loopPayload(t, r)
				raw := map[string]any{}
				if err := json.Unmarshal(domain.JSON(visualTextWrite()), &raw); err != nil {
					t.Error(err)
				}
				raw[field] = "malformed optional array"
				writeReviewWireArgs(w, loopSaveTool, raw)
			}, domain.ArticleDeliveryV6)
			if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
				t.Fatal(err)
			}
			run, _ := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
			if run.Status != "succeeded" {
				t.Fatal("optional JSON shape blocked complete prose", run.Status)
			}
		})
	}
}

func TestDeliveryVisualBroadGoalNarrowsLocallyWithoutSpendingModelCalls(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model expected") }, domain.ArticleDeliveryV6)
	s.snapshot.DurationMS = 180000
	s.cp.Calls = 18
	r, err := s.captureDeliveryVisual(context.Background(), "broad", deliveryVisualRequest{Key: "diagram", Question: "这一段的示意图", StartMS: 0, EndMS: 120000}, false)
	if err != nil || r.Status != "prose_calls_reserved" || r.Window.StartMS != 30000 || r.Window.EndMS != 90000 || r.RequestedRange == nil || r.RequestedRange.EndMS != 120000 || r.Attempted {
		t.Fatalf("broad goal was rejected or took prose budget: %+v %v", r, err)
	}
}

func TestDeliveryVisualAITitleAppearsInExecutionHistoryWithFramesAndSkip(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model expected") }, domain.ArticleDeliveryV6)
	ctx := context.Background()
	visualVideo(t, s)
	r, err := s.captureDeliveryVisual(ctx, "heading", deliveryVisualRequest{Key: "diagram", ActivityTitle: "查看邮件分类结果", Question: "看清邮件分类界面与运行状态", StartMS: 0, EndMS: 1000, Count: 1}, false)
	if err != nil || r.ActivityTitle != "按需取帧：查看邮件分类结果" || len(r.Frames) != 1 {
		t.Fatalf("missing title or real frame %+v %v", r, err)
	}
	var count int
	if err = s.engine.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE run_id=$1 AND step->>'title'=$2 AND step->>'status'='running'`, s.claim.Run.ID, r.ActivityTitle).Scan(&count); err != nil || count != 1 {
		t.Fatalf("running AI action missing: %d %v", count, err)
	}
	var request, owner string
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT id,session_id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&request, &owner); err != nil {
		t.Fatal(err)
	}
	page, err := s.engine.Store.ActivityPage(ctx, owner, "request", request, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var steps []domain.Step
	if json.Unmarshal(domain.JSON(page["items"]), &steps) != nil {
		t.Fatal("execution page contract")
	}
	index := slices.IndexFunc(steps, func(step domain.Step) bool { return step.Title == r.ActivityTitle })
	if index < 0 || steps[index].Status != "completed" || len(steps[index].Frames) != 1 || steps[index].SourceRange == nil {
		t.Fatalf("visitor execution history lost screenshot heading/thumbnail: %+v", steps)
	}
	before := len(steps)
	if _, err = s.captureDeliveryVisual(ctx, "heading", deliveryVisualRequest{Key: "diagram", ActivityTitle: "换个标题描述邮件分类结果", Question: "看清邮件分类界面与运行状态", StartMS: 0, EndMS: 1000, Count: 1}, false); err != nil {
		t.Fatal(err)
	}
	page, err = s.engine.Store.ActivityPage(ctx, owner, "request", request, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(domain.JSON(page["items"]), &steps)
	if len(steps) != before {
		t.Fatal("replay duplicated the public capture action")
	}
	s.cp.Calls = 18
	_, err = s.captureDeliveryVisual(ctx, "skip-heading", deliveryVisualRequest{Key: "next", ActivityTitle: "查看最终处理结果", Question: "查看最后的界面", StartMS: 0, EndMS: 1000, Count: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE run_id=$1 AND step->>'title'=$2 AND step->'outcome'->>'message' LIKE '%优先用于完成正文%'`, s.claim.Run.ID, "略过取帧：查看最终处理结果").Scan(&count); err != nil || count != 1 {
		t.Fatalf("budget skip was misreported as capture success: %d %v", count, err)
	}
	stats, err := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
	if err != nil || stats.Submissions != 0 {
		t.Fatal("titles introduced a model call", stats, err)
	}
}
