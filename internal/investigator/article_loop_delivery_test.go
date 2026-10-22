package investigator

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/exporting"
	"vidlens/internal/httpapi"
)

func TestArticleLoopT14ReadingSSEExportsAndQAMatrix(t *testing.T) {
	var articleCalls, qaCalls atomic.Int32
	var qaImage, qaObserved atomic.Bool
	var qaFrame atomic.Value
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var in map[string]json.RawMessage
		if err = json.Unmarshal(body, &in); err != nil {
			t.Error(err)
		}
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		_ = json.Unmarshal(in["tools"], &tools)
		loop := false
		for _, tool := range tools {
			loop = loop || tool.Function.Name == loopSaveTool || tool.Function.Name == loopReviewTool
		}
		if loop {
			r.Body = io.NopCloser(bytes.NewReader(body))
			p := loopPayload(t, r)
			articleCalls.Add(1)
			if p["scope"] != nil {
				writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
			} else {
				writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			}
			return
		}
		if respondSupportedEvidenceReview(t, w, in) {
			return
		}

		if qaImage.Load() && qaObserved.CompareAndSwap(false, true) {
			writeReviewWireArgs(w, "record_observation", observation{InspectionID: "question-context", FrameIDs: []string{qaFrame.Load().(string)}, VisibleFacts: []string{"合成黑色画面"}, Status: "supported"})
			return
		}
		qaCalls.Add(1)
		writeReviewWireArgs(w, "submit_answer", map[string]any{"kind": "insufficient_evidence", "summary": "合成测试来源没有提供这个细节。", "blocks": []domain.Block{{ID: "reason", Type: "paragraph", Origin: "background_explanation", Content: []domain.Inline{{Type: "text", Text: "合成材料不足以支持更具体的判断，保留证据边界。"}}}}, "parts": []domain.AnswerPart{{Title: "证据边界", BlockIDs: []string{"reason"}}}, "limitations": []domain.Limitation{}})
	})
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	var requestID, owner string
	if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT id,session_id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&requestID, &owner); err != nil {
		t.Fatal(err)
	}
	request, err := s.engine.Store.Request(ctx, owner, requestID)
	if err != nil || request.ArticleID == nil || request.Status != "completed" {
		t.Fatal("request did not point to immutable article", err)
	}
	a, err := s.engine.Store.PublishedArticle(ctx, *request.ArticleID)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"markdown", "markdown_zip"} {
		accepted, _, err := s.engine.Store.CreateExport(ctx, owner, requestID, format, domain.ID())
		if err != nil {
			t.Fatal(err)
		}
		export, err := s.engine.Store.ClaimExport(ctx, accepted.ID)
		if err != nil || export == nil {
			t.Fatal(err)
		}
		service := exporting.Service{Store: s.engine.Store, Config: s.engine.Config}
		if err = service.Run(ctx, *export); err != nil {
			t.Fatal(err)
		}
		ready, err := s.engine.Store.Export(ctx, owner, requestID, export.ID)
		if err != nil || ready.Status != "ready" || ready.StorageKey == nil {
			t.Fatalf("unreadable export %+v %v", ready, err)
		}
		data, err := os.ReadFile(filepath.Join(s.engine.Config.DataDir, *ready.StorageKey))
		if err != nil {
			t.Fatal(err)
		}
		if format == "markdown" {
			if !bytes.Contains(data, []byte(a.Title)) || !bytes.Contains(data, []byte(a.Sections[0].Blocks[1].Caption)) {
				t.Fatal("markdown lost semantic content")
			}
		} else {
			z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			md, picture := false, false
			for _, file := range z.File {
				md = md || file.Name == "article.md"
				picture = picture || strings.HasPrefix(file.Name, "images/")
			}
			if !md || !picture {
				t.Fatal("ZIP lost article or actual pixels")
			}
		}
	}
	frames := a.Sections[0].Blocks[1].FrameIDs
	qaFrame.Store(frames[0])
	// Ordinary, image and external-reference QA all retain the existing V9
	// adapter when the article uses the new loop. No article-loop state leaks.
	for _, variant := range []string{"ordinary", "image", "external"} {
		con, _, err := s.engine.Store.EnsureConversation(ctx, owner, requestID)
		if err != nil {
			t.Fatal(err)
		}
		question := domain.QuestionInput{Question: "这段合成来源没有提供的参数是什么？", ExpectedLastTurnID: con.LastTurnID, AllowExternalSources: variant == "external"}
		qaImage.Store(variant == "image")
		if variant == "image" {
			question.Context.FrameID = &frames[0]
		}
		accepted, err := s.engine.Store.CreateTurn(ctx, owner, con.ID, domain.ID(), question, "")
		if err != nil {
			t.Fatal(err)
		}
		turn, err := s.engine.Store.Turn(ctx, owner, con.ID, accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := s.engine.Store.Claim(ctx, turn.RunID, 1, "qa-"+variant)
		if err != nil || claim == nil {
			t.Fatal(err)
		}
		var frozen checkpoint
		if err = json.Unmarshal(claim.Run.Checkpoint, &frozen); err != nil {
			t.Fatal(err)
		}
		if frozen.ExecutionContract == nil || frozen.ExecutionContract.Version != domain.ExecutionContractV9 || frozen.ArticleLoop != nil {
			t.Fatalf("QA contract changed due to article: %s", claim.Run.Checkpoint)
		}
		if err = s.engine.Run(ctx, *claim, s.profile); err != nil {
			t.Fatalf("%s QA failed: %v", variant, err)
		}
		result, err := s.engine.Store.TurnForRun(ctx, turn.RunID)
		if err != nil || result.Answer == nil {
			t.Fatalf("%s QA unreadable: %v", variant, err)
		}
	}
	if articleCalls.Load() != 2 || qaCalls.Load() != 3 {
		t.Fatalf("unexpected main chain calls %d/%d", articleCalls.Load(), qaCalls.Load())
	}
	// Use the real HTTP server and authenticated terminal SSE, not a mocked API.
	token := "article-loop-browser-fixture-only"
	hash := sha256.Sum256([]byte(token))
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE anonymous_sessions SET token_hash=$2 WHERE id=$1", owner, hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	apiCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	api := httpapi.New(apiCtx, s.engine.Store, s.engine.Config)
	handler := api.Handler()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fixture/login" {
			http.SetCookie(w, &http.Cookie{Name: "vidlens_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.Redirect(w, r, "http://127.0.0.1:15173/r/"+requestID, http.StatusFound)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer backend.Close()
	api.Config.Origin = backend.URL
	client := &http.Client{Timeout: 3 * time.Second}
	for _, path := range []string{"/api/v1/requests/" + requestID, "/api/v1/requests/" + requestID + "/article", "/api/v1/requests/" + requestID + "/frames/" + frames[0] + "/content", "/api/v1/requests/" + requestID + "/events"} {
		req, _ := http.NewRequest(http.MethodGet, backend.URL+path, nil)
		req.AddCookie(&http.Cookie{Name: "vidlens_session", Value: token})
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(path, err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: status=%d %v %s", path, resp.StatusCode, err, data)
		}
		if strings.HasSuffix(path, "/events") && !bytes.Contains(data, []byte(`"status":"completed"`)) {
			t.Fatalf("terminal SSE did not reflect transaction: %s", data)
		}
	}
	// This opt-in harness keeps only this test's isolated schema/server alive for
	// desktop acceptance. Normal CI never waits or starts a browser.
	if manifest := os.Getenv("VIDLENS_BROWSER_FIXTURE"); manifest != "" {
		if err = os.WriteFile(manifest, domain.JSON(map[string]any{"backend": backend.URL, "login_url": backend.URL + "/fixture/login", "request_id": requestID, "article_id": a.ID, "frame_id": frames[0], "article_calls": articleCalls.Load()}), 0600); err != nil {
			t.Fatal(err)
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		timeout := time.After(20 * time.Minute)
		for {
			select {
			case <-ticker.C:
				if _, err = os.Stat(manifest + ".done"); err == nil {
					return
				}
			case <-timeout:
				t.Fatal("browser acceptance fixture expired")
				return
			}
		}
	}
}
