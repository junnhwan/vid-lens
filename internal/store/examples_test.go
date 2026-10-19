package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestPublicExampleFrameURLStaysOnTheExampleRoute(t *testing.T) {
	articleID := strings.Repeat("ab", 16)
	frameID := strings.Repeat("cd", 16)
	frame := publicFrame(domain.Asset{ID: frameID, MIME: "image/jpeg", TimeMS: domain.Ptr(int64(1500))}, articleID)
	if frame.SnapshotID != "" || frame.ExpiresAt != nil || frame.URLs["full"] == nil || frame.URLs["thumbnail"] == nil {
		t.Fatalf("%+v", frame)
	}
	if *frame.URLs["full"] != *frame.URLs["thumbnail"] || *frame.URLs["full"] != exampleFrameURL(articleID, frameID) {
		t.Fatalf("url %s", *frame.URLs["full"])
	}
	if strings.Contains(*frame.URLs["full"], "/api/v1/requests/") {
		t.Fatal("public frame uses a private request path")
	}
	other := publicFrame(domain.Asset{ID: strings.Repeat("ef", 16), MIME: "image/jpeg"}, strings.Repeat("01", 16))
	if *other.URLs["full"] == *frame.URLs["full"] {
		t.Fatal("frame urls alias one another")
	}
}

func TestReplaceExampleShowcasesRejectsInvalidChoicesBeforeWriting(t *testing.T) {
	s := &Store{}
	tooMany := make([]string, maxExampleShowcases+1)
	for i := range tooMany {
		tooMany[i] = domain.ID()
	}
	if e := s.ReplaceExampleShowcases(context.Background(), tooMany); !invalid(e) {
		t.Fatal(e)
	}
	if e := s.ReplaceExampleShowcases(context.Background(), []string{"not-an-article"}); !invalid(e) {
		t.Fatal(e)
	}
	id := domain.ID()
	if e := s.ReplaceExampleShowcases(context.Background(), []string{id, id}); !invalid(e) {
		t.Fatal(e)
	}
}

func TestExampleShowcasesChoosePublishedArticles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first := publishExample(t, s, "第一篇", "第一节")
	second := publishExample(t, s, "第二篇", "第二节")

	cards, e := s.PublicExamples(ctx)
	if e != nil || len(cards) != 0 {
		t.Fatalf("unpublished examples leaked: %+v %v", cards, e)
	}
	if _, e = s.PublicExample(ctx, first.articleID); !notFound(e) {
		t.Fatalf("hidden article readable: %v", e)
	}
	if _, e = s.PublicExampleFrame(ctx, first.articleID, first.frameID); !notFound(e) {
		t.Fatalf("hidden frame readable: %v", e)
	}

	if e = s.ReplaceExampleShowcases(ctx, []string{second.articleID, first.articleID}); e != nil {
		t.Fatal(e)
	}
	cards, e = s.PublicExamples(ctx)
	if e != nil || len(cards) != 2 || cards[0].ID != second.articleID || cards[1].ID != first.articleID {
		t.Fatalf("showcase order: %+v %v", cards, e)
	}
	if cards[0].Cover != exampleFrameURL(second.articleID, second.frameID) || cards[0].Description != "第二节" {
		t.Fatalf("card: %+v", cards[0])
	}
	example, e := s.PublicExample(ctx, second.articleID)
	if e != nil || example.Article.Title != "第二篇" || len(example.Frames) != 1 || len(example.Questions) != 0 || len(example.GenerationSteps) != 0 {
		t.Fatalf("public example: %+v %v", example, e)
	}
	if example.Frames[0].URLs["full"] == nil || *example.Frames[0].URLs["full"] != cards[0].Cover {
		t.Fatalf("frame url: %+v", example.Frames[0])
	}
	raw, e := json.Marshal(example)
	if e != nil {
		t.Fatal(e)
	}
	text := string(raw)
	for _, forbidden := range []string{"request_id", "storage_key", "session_id", "conversation_id", "sha256", "api_key", "config_id", second.storageKey} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public example contains %s", forbidden)
		}
	}
	asset, e := s.PublicExampleFrame(ctx, second.articleID, second.frameID)
	if e != nil || asset.ID != second.frameID || asset.StorageKey != second.storageKey {
		t.Fatalf("frame asset: %+v %v", asset, e)
	}
	if _, e = s.PublicExampleFrame(ctx, second.articleID, first.frameID); !notFound(e) {
		t.Fatalf("cross-article frame: %v", e)
	}

	if e = s.ReplaceExampleShowcases(ctx, []string{}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublicExample(ctx, second.articleID); !notFound(e) {
		t.Fatalf("cleared showcase still public: %v", e)
	}
	missing := strings.Repeat("ab", 16)
	if e = s.ReplaceExampleShowcases(ctx, []string{missing}); !notFound(e) {
		t.Fatalf("missing article: %v", e)
	}
	if cards, e = s.PublicExamples(ctx); e != nil || len(cards) != 0 {
		t.Fatalf("failed save changed showcase: %+v %v", cards, e)
	}
	tooMany := make([]string, 25)
	for i := range tooMany {
		tooMany[i] = domain.ID()
	}
	if e = s.ReplaceExampleShowcases(ctx, tooMany); !invalid(e) {
		t.Fatalf("limit: %v", e)
	}
	if e = s.ReplaceExampleShowcases(ctx, []string{first.articleID, first.articleID}); !invalid(e) {
		t.Fatalf("duplicate: %v", e)
	}
	if e = s.ReplaceExampleShowcases(ctx, []string{"not-an-article"}); !invalid(e) {
		t.Fatalf("malformed: %v", e)
	}
}

type publishedExample struct {
	articleID  string
	frameID    string
	storageKey string
}

func publishExample(t *testing.T, s *Store, title, section string) publishedExample {
	t.Helper()
	ctx := context.Background()
	owner := session(t, s)
	// Each published example needs its own source: the product correctly reuses
	// an existing article for another request with the same generation spec.
	r, c := prepared(t, s, owner, "https://youtube.com/watch?v="+domain.ID()[:11])
	frameID := domain.ID()
	storageKey := "frames/" + frameID + ".jpg"
	frame := domain.Asset{
		ID: frameID, SnapshotID: r.SnapshotID, SourceID: c.Run.SourceID, RunID: c.Run.ID,
		Kind: "frame", StorageKey: storageKey, SHA256: strings.Repeat("a", 64), MIME: "image/jpeg",
		Bytes: 12, TimeMS: domain.Ptr(int64(1500)), Width: 32, Height: 18,
	}
	if e := s.AddAsset(ctx, c, frame); e != nil {
		t.Fatal(e)
	}
	article := domain.Article{
		SchemaVersion: 1, SnapshotID: *r.SnapshotID, Title: title,
		Source:    domain.Source{Platform: "youtube", CanonicalURL: "https://youtube.com/watch?v=abcdefghijk", Title: domain.Ptr(title), DurationMS: domain.Ptr(int64(1000))},
		Sections:  []domain.Section{{ID: "section", Title: section, Blocks: []domain.Block{{ID: "figure", Type: "figure", Origin: "model", FrameIDs: []string{frameID}}}}},
		Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{},
	}
	if e := s.Publish(ctx, c, article); e != nil {
		t.Fatal(e)
	}
	r, e := s.Request(ctx, owner, r.ID)
	if e != nil || r.ArticleID == nil {
		t.Fatal(e)
	}
	return publishedExample{articleID: *r.ArticleID, frameID: frameID, storageKey: storageKey}
}

func TestPublicExampleFreezesGenerationAndHidesOwnerQuestions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	request, claim := prepared(t, s, owner, "https://youtube.com/watch?v="+domain.ID()[:11])
	articleFrame := domain.ID()
	processFrame := domain.ID()
	for _, frame := range []domain.Asset{
		{ID: articleFrame, SnapshotID: request.SnapshotID, SourceID: claim.Run.SourceID, RunID: claim.Run.ID, Kind: "frame", StorageKey: "frames/" + articleFrame + ".jpg", SHA256: strings.Repeat("a", 64), MIME: "image/jpeg", Bytes: 12, TimeMS: domain.Ptr(int64(1500)), Width: 32, Height: 18},
		{ID: processFrame, SnapshotID: request.SnapshotID, SourceID: claim.Run.SourceID, RunID: claim.Run.ID, Kind: "frame", StorageKey: "frames/" + processFrame + ".jpg", SHA256: strings.Repeat("b", 64), MIME: "image/jpeg", Bytes: 12, TimeMS: domain.Ptr(int64(2400)), Width: 32, Height: 18},
	} {
		if e := s.AddAsset(ctx, claim, frame); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Step(ctx, claim, domain.Step{ID: "look", Phase: "investigate", Title: "查看手写数字", Status: "completed", FrameIDs: []string{processFrame}}); e != nil {
		t.Fatal(e)
	}
	article := domain.Article{
		SchemaVersion: 1, SnapshotID: *request.SnapshotID, Title: "公开示例",
		Source:    domain.Source{Platform: "youtube", CanonicalURL: "https://youtube.com/watch?v=abcdefghijk", Title: domain.Ptr("公开示例"), DurationMS: domain.Ptr(int64(1000))},
		Sections:  []domain.Section{{ID: "section", Title: "第一节", Blocks: []domain.Block{{ID: "figure", Type: "figure", Origin: "model", FrameIDs: []string{articleFrame}}}}},
		Citations: map[string]domain.Citation{}, Limitations: []domain.Limitation{},
	}
	if e := s.Publish(ctx, claim, article); e != nil {
		t.Fatal(e)
	}
	request, e := s.Request(ctx, owner, request.ID)
	if e != nil || request.ArticleID == nil {
		t.Fatal(e)
	}
	if e = s.ReplaceExampleShowcases(ctx, []string{*request.ArticleID}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE example_showcases SET process_frozen=false, process=$2 WHERE article_id=$1", *request.ArticleID, []byte(`{"status":"unavailable","steps":[]}`)); e != nil {
		t.Fatal(e)
	}
	before, e := s.PublicExample(ctx, *request.ArticleID)
	if e != nil || before.ProcessStatus != "unavailable" || len(before.GenerationSteps) != 0 {
		t.Fatalf("unfrozen example %+v %v", before.ProcessStatus, e)
	}
	if e = s.FreezeExampleProcess(ctx, *request.ArticleID); e != nil {
		t.Fatal(e)
	}
	example, e := s.PublicExample(ctx, *request.ArticleID)
	if e != nil || example.ProcessStatus != "ready" || len(example.GenerationSteps) != 1 || example.GenerationSteps[0].Title != "查看手写数字" {
		t.Fatalf("frozen process %+v %v", example.GenerationSteps, e)
	}
	if example.GenerationSteps[0].RunID != "attempt-1" && example.GenerationSteps[0].RunID != "attempt-0" {
		t.Fatalf("run id %s", example.GenerationSteps[0].RunID)
	}
	raw, e := json.Marshal(example)
	if e != nil {
		t.Fatal(e)
	}
	text := string(raw)
	for _, forbidden := range []string{claim.Run.ID, request.ID, "站长私人追问不应公开", "/api/v1/requests/", "frames/" + processFrame + ".jpg"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public example contains %s", forbidden)
		}
	}
	if !strings.Contains(text, exampleFrameURL(*request.ArticleID, processFrame)) {
		t.Fatalf("process frame url missing: %s", text)
	}
	asset, e := s.PublicExampleFrame(ctx, *request.ArticleID, processFrame)
	if e != nil || asset.ID != processFrame {
		t.Fatalf("process frame %+v %v", asset, e)
	}

	conversation, _, e := s.EnsureConversation(ctx, owner, request.ID)
	if e != nil {
		t.Fatal(e)
	}
	accepted, e := s.CreateTurn(ctx, owner, conversation.ID, domain.ID(), domain.QuestionInput{Question: "站长私人追问不应公开"}, "")
	if e != nil {
		t.Fatal(e)
	}
	turn, e := s.Turn(ctx, owner, conversation.ID, accepted.ID)
	if e != nil {
		t.Fatal(e)
	}
	answerClaim, e := s.Claim(ctx, turn.RunID, 1, "owner-question")
	if e != nil || answerClaim == nil {
		t.Fatal(e)
	}
	if e = s.CompleteAnswer(ctx, *answerClaim, domain.Answer{Kind: "direct", Summary: "私人回答", Blocks: []domain.Block{}, Citations: map[string]domain.Citation{}, Frames: []domain.Frame{}, Limitations: []domain.Limitation{}}); e != nil {
		t.Fatal(e)
	}
	example, e = s.PublicExample(ctx, *request.ArticleID)
	if e != nil {
		t.Fatal(e)
	}
	raw, e = json.Marshal(example)
	if e != nil || strings.Contains(string(raw), "站长私人追问不应公开") || len(example.Questions) != 0 {
		t.Fatalf("owner question leaked: %s", raw)
	}

	if _, e = s.Pool.Exec(ctx, "DELETE FROM run_events"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReplaceExampleShowcases(ctx, []string{*request.ArticleID}); e != nil {
		t.Fatal(e)
	}
	example, e = s.PublicExample(ctx, *request.ArticleID)
	if e != nil || len(example.GenerationSteps) != 1 || example.GenerationSteps[0].Title != "查看手写数字" {
		t.Fatalf("resave lost process %+v %v", example.GenerationSteps, e)
	}
}

func TestVisitorQuestionsStayOnThePublishedExample(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	published := publishExample(t, s, "可提问的示例", "第一节")
	if e := s.ReplaceExampleShowcases(ctx, []string{published.articleID}); e != nil {
		t.Fatal(e)
	}
	visitor := session(t, s)
	other := session(t, s)
	before, e := s.SiteStats(ctx)
	if e != nil {
		t.Fatal(e)
	}
	first, created, e := s.OpenExampleConversation(ctx, visitor, published.articleID)
	if e != nil || !created || first.ArticleID != published.articleID || first.RequestID == "" {
		t.Fatalf("open %+v %v %v", first, created, e)
	}
	again, created, e := s.OpenExampleConversation(ctx, visitor, published.articleID)
	if e != nil || created || again.ID != first.ID {
		t.Fatalf("reopen %+v %v %v", again, created, e)
	}
	second, _, e := s.OpenExampleConversation(ctx, other, published.articleID)
	if e != nil || second.ID == first.ID || second.RequestID == first.RequestID {
		t.Fatalf("sessions share a conversation %+v %+v %v", first, second, e)
	}
	if _, _, e = s.OpenExampleConversation(ctx, visitor, strings.Repeat("ab", 16)); !notFound(e) {
		t.Fatalf("hidden example %v", e)
	}
	history, e := s.RequestHistory(ctx, visitor, 20, 0)
	if e != nil || len(history.Items) != 0 {
		t.Fatalf("example question listed as a generation %+v %v", history.Items, e)
	}
	after, e := s.SiteStats(ctx)
	if e != nil || after["completed_readings"] != before["completed_readings"] {
		t.Fatalf("opening an example counted a reading %v -> %v", before["completed_readings"], after["completed_readings"])
	}
	shadow, e := s.Request(ctx, visitor, first.RequestID)
	if e != nil || shadow.Reuse != "example_question" || shadow.Status != "completed" || shadow.ArticleID == nil || *shadow.ArticleID != published.articleID {
		t.Fatalf("shadow request %+v %v", shadow, e)
	}
	var latest string
	var expected *string
	for i := 0; i < ExampleQuestionLimit; i++ {
		accepted, e := s.CreateTurn(ctx, visitor, first.ID, domain.ID(), domain.QuestionInput{Question: fmt.Sprintf("示例问题 %d", i+1), ExpectedLastTurnID: expected}, "")
		if e != nil {
			t.Fatal(e)
		}
		turn, e := s.Turn(ctx, visitor, first.ID, accepted.ID)
		if e != nil {
			t.Fatal(e)
		}
		claimed, e := s.Claim(ctx, turn.RunID, 1, "example-question")
		if e != nil || claimed == nil {
			t.Fatal(e)
		}
		if _, e = s.Fail(ctx, *claimed, domain.Failure{Code: "STOPPED", Message: "停在这里", CanRetry: i == ExampleQuestionLimit-1}, false, 0); e != nil {
			t.Fatal(e)
		}
		latest = turn.ID
		expected = &latest
	}
	if _, e = s.CreateTurn(ctx, visitor, first.ID, domain.ID(), domain.QuestionInput{Question: "超出的问题", ExpectedLastTurnID: expected}, ""); problemCode(e) != "EXAMPLE_QUESTION_LIMIT" {
		t.Fatal(e)
	}
	if _, e = s.CreateTurn(ctx, visitor, first.ID, domain.ID(), domain.QuestionInput{ExpectedLastTurnID: expected}, latest); e != nil {
		t.Fatalf("retry after the limit: %v", e)
	}
	if e = s.ReplaceExampleShowcases(ctx, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateTurn(ctx, other, second.ID, domain.ID(), domain.QuestionInput{Question: "示例已撤下"}, ""); !notFound(e) {
		t.Fatal(e)
	}
}

func problemCode(e error) string {
	var p *domain.Problem
	if errors.As(e, &p) {
		return p.Code
	}
	return ""
}

func notFound(e error) bool {
	var p *domain.Problem
	return errors.As(e, &p) && p.Status == 404
}

func invalid(e error) bool {
	var p *domain.Problem
	return errors.As(e, &p) && p.Status == 400
}
