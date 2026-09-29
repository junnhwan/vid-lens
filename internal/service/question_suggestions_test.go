package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type questionTestClient struct {
	calls    int
	response string
	err      error
	messages []ai.ChatMessage
}

func (c *questionTestClient) Chat(_ context.Context, messages []ai.ChatMessage) (string, error) {
	c.calls++
	c.messages = messages
	return c.response, c.err
}

type questionTestFactory struct{ client ai.ChatClient }

func (f questionTestFactory) NewChatClient(ai.Profile) (ai.ChatClient, error) { return f.client, nil }

func TestSuggestedVideoQuestionsGenerationIsExplicitCachedAndContentVersioned(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, Filename: "lesson.mp4", Title: "向量检索", FileMD5: "abababababababababababababababab"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	transcript := &model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "本课程介绍向量检索如何帮助定位视频中的相关片段。"}
	if err := repos.Transcription.Create(transcript); err != nil {
		t.Fatal(err)
	}
	client := &questionTestClient{response: "```json\n{\"questions\":[\"向量检索怎么定位视频片段？\",\"检索结果不准时怎么办？\"]}\n```"}
	svc := NewQuestionSuggestionService(repos, stubConversationProfileProvider{}, questionTestFactory{client})
	ctx := context.Background()
	first, err := svc.VideoQuestions(ctx, 7, task.ID, false)
	if err != nil || client.calls != 0 || !strings.Contains(first.Questions[0].Question, "向量检索") {
		t.Fatalf("GET = %+v, calls=%d err=%v", first, client.calls, err)
	}
	generated, err := svc.VideoQuestions(ctx, 7, task.ID, true)
	if err != nil || client.calls != 1 || generated.Questions[0].Question != "向量检索怎么定位视频片段？" {
		t.Fatalf("POST = %+v calls=%d err=%v", generated, client.calls, err)
	}
	if _, err := svc.VideoQuestions(ctx, 8, task.ID, true); err == nil || client.calls != 1 {
		t.Fatal("cache bypassed video ownership")
	}
	if err := repos.Task.UpdateStatus(task.ID, model.TaskStatusRunning, ""); err != nil {
		t.Fatal(err)
	}
	again, err := svc.VideoQuestions(ctx, 7, task.ID, true)
	if err != nil || client.calls != 1 || again.ContentVersion != generated.ContentVersion {
		t.Fatalf("progress invalidated content cache: calls=%d err=%v", client.calls, err)
	}
	if err := repos.Task.UpdateTitle(task.ID, "检索原理"); err != nil {
		t.Fatal(err)
	}
	renamed, err := svc.VideoQuestions(ctx, 7, task.ID, true)
	if err != nil || client.calls != 2 || renamed.ContentVersion == generated.ContentVersion {
		t.Fatalf("title cache not refreshed: calls=%d err=%v", client.calls, err)
	}
	transcript.Content = "本课程介绍画面文字如何帮助定位视频中的相关片段。"
	if err := repos.Transcription.Upsert(transcript); err != nil {
		t.Fatal(err)
	}
	changed, err := svc.VideoQuestions(ctx, 7, task.ID, false)
	if err != nil || changed.ContentVersion == renamed.ContentVersion || client.calls != 2 || !strings.Contains(changed.Questions[0].Question, "画面文字") {
		t.Fatalf("content stale: %+v calls=%d err=%v", changed, client.calls, err)
	}
}

func TestSuggestedQuestionParserKeepsNaturalShortQuestionsAndRejectsTemplates(t *testing.T) {
	output := `{"questions":["**向量检索怎么工作？**","向量检索怎么工作?","视频中关于向量检索有哪些具体说明？","` + strings.Repeat("长", 33) + `？","` + strings.Repeat("x", 61) + `?","OCR和画面描述怎么选"]}`
	questions, err := parseSuggestedQuestions(output, false, "")
	if err != nil || len(questions) != 2 {
		t.Fatalf("questions=%+v err=%v", questions, err)
	}
	if questions[0].Question != "向量检索怎么工作？" || questions[1].Question != "OCR和画面描述怎么选？" {
		t.Fatalf("questions=%+v", questions)
	}
	followup, err := parseSuggestedQuestions(`{"questions":["向量检索怎么工作？","稀疏检索有什么优势？","能举个反例吗？","哪些情况会失效？","继续说说？"]}`, true, "向量检索怎么工作？")
	if err != nil || len(followup) != 3 || followup[0].Question == "向量检索怎么工作？" {
		t.Fatalf("followup=%+v err=%v", followup, err)
	}
}

func TestSuggestedQuestionCacheCoalescesParallelRequestsAndHonorsCancellation(t *testing.T) {
	svc := NewQuestionSuggestionService(nil, nil, nil)
	var calls atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	entered := make(chan struct{})
	result := VideoQuestionResult{Status: "ready", Questions: []VideoQuestion{{Question: "该怎样应用？"}}}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := svc.generateCached(context.Background(), "same", result, func() (VideoQuestionResult, bool) {
				if calls.Add(1) == 1 {
					close(entered)
				}
				<-start
				return result, true
			})
			if err != nil || len(got.Questions) != 1 {
				t.Errorf("got=%+v err=%v", got, err)
			}
		}()
	}
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.generateCached(ctx, "same", result, func() (VideoQuestionResult, bool) { t.Error("cancelled request generated"); return result, true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	// Expiration allows one new generation; eviction never exceeds 256 entries.
	svc.cache["same"] = questionCacheEntry{result: result, expires: time.Now().Add(-time.Second)}
	if _, err := svc.generateCached(context.Background(), "same", result, func() (VideoQuestionResult, bool) { calls.Add(1); return result, true }); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expired calls=%d", calls.Load())
	}
	for i := 0; i < 300; i++ {
		key := strings.Repeat("x", i+1)
		if _, err := svc.generateCached(context.Background(), key, result, func() (VideoQuestionResult, bool) { return result, true }); err != nil {
			t.Fatal(err)
		}
	}
	if len(svc.cache) > 256 {
		t.Fatalf("unbounded cache=%d", len(svc.cache))
	}
}

func TestFollowUpQuestionsUseOnlyLatestSavedOwnedPairAndCache(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, Filename: "lesson.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeVideo, TaskID: task.ID}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	client := &questionTestClient{response: `{"questions":["RLCD和RLHF有什么差别？","如何评估决策质量？"]}`}
	svc := NewQuestionSuggestionService(repos, stubConversationProfileProvider{}, questionTestFactory{client})
	ctx := context.Background()
	if empty, err := svc.FollowUpQuestions(ctx, 7, session.ID, 123); err != nil || len(empty.Questions) != 0 || client.calls != 0 {
		t.Fatalf("unsaved=%+v err=%v calls=%d", empty, err, client.calls)
	}
	question := &model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "user", Content: "RLCD是什么？"}
	answer := &model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "assistant", Content: "视频称RLCD用于强化学习校准决策，细节尚未公开。"}
	if err := repos.Chat.CreateMessage(question); err != nil {
		t.Fatal(err)
	}
	if err := repos.Chat.CreateMessage(answer); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FollowUpQuestions(ctx, 8, session.ID, answer.ID); err == nil {
		t.Fatal("foreign user read followups")
	}
	result, err := svc.FollowUpQuestions(ctx, 7, session.ID, answer.ID)
	if err != nil || result.MessageID != answer.ID || len(result.Questions) != 2 || client.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, client.calls)
	}
	if !strings.Contains(client.messages[1].Content, question.Content) || !strings.Contains(client.messages[1].Content, answer.Content) {
		t.Fatalf("pair not in prompt=%+v", client.messages)
	}
	if _, err := svc.FollowUpQuestions(ctx, 7, session.ID, answer.ID); err != nil || client.calls != 1 {
		t.Fatalf("cache calls=%d err=%v", client.calls, err)
	}
	if err := repos.Chat.CreateMessage(&model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "user", Content: "再展开解释"}); err != nil {
		t.Fatal(err)
	}
	if empty, err := svc.FollowUpQuestions(ctx, 7, session.ID, answer.ID); err != nil || len(empty.Questions) != 0 || client.calls != 1 {
		t.Fatalf("in-progress=%+v err=%v calls=%d", empty, err, client.calls)
	}
}

func TestSuggestedQuestionsModelFailureIsCachedAsNaturalFallback(t *testing.T) {
	svc := NewQuestionSuggestionService(nil, nil, nil)
	fallback := VideoQuestionResult{Status: "ready", Questions: []VideoQuestion{{Question: "向量检索怎么理解？"}}}
	calls := 0
	generate := func() (VideoQuestionResult, bool) { calls++; return fallback, false }
	for i := 0; i < 2; i++ {
		got, err := svc.generateCached(context.Background(), "failed", fallback, generate)
		if err != nil || got.Questions[0].Question != fallback.Questions[0].Question {
			t.Fatalf("fallback=%+v err=%v", got, err)
		}
	}
	if calls != 1 || time.Until(svc.cache["failed"].expires) > 5*time.Minute {
		t.Fatalf("failure repeated or bad ttl calls=%d", calls)
	}
}

func TestQuestionTextSamplingIncludesBeginningMiddleAndEndWithinBudget(t *testing.T) {
	text := "开始" + strings.Repeat("甲", 500) + "中间" + strings.Repeat("乙", 500) + "结束"
	sampled := sampleQuestionText(text, 120)
	if utf8.RuneCountInString(sampled) > 120 || !strings.Contains(sampled, "开始") || !strings.Contains(sampled, "中间") || !strings.Contains(sampled, "结束") {
		t.Fatalf("sample=%q", sampled)
	}
}

func TestFollowUpQuestionsDoNotGenerateForCancelledSavedSnapshot(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, Filename: "lesson.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeVideo, TaskID: task.ID}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(AgentSnapshot{Version: 2, StopReason: "cancelled", Steps: []AgentSnapshotStep{}})
	snapshot := string(encoded)
	question := &model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "user", Content: "能解释检索过程吗？"}
	answer := &model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "assistant", Content: "尚未完成的解释", RetrievalSnapshot: &snapshot}
	if err := repos.Chat.CreateMessage(question); err != nil {
		t.Fatal(err)
	}
	if err := repos.Chat.CreateMessage(answer); err != nil {
		t.Fatal(err)
	}
	client := &questionTestClient{response: `{"questions":["哪些情况适用？","能举个例子吗？"]}`}
	svc := NewQuestionSuggestionService(repos, stubConversationProfileProvider{}, questionTestFactory{client})
	got, err := svc.FollowUpQuestions(context.Background(), 7, session.ID, answer.ID)
	if err != nil || len(got.Questions) != 0 || client.calls != 0 {
		t.Fatalf("cancelled got=%+v err=%v calls=%d", got, err, client.calls)
	}
}

func TestFollowUpQuestionsRejectRemovedKnowledgeBaseSourceBeforeModelOrCache(t *testing.T) {
	repos := newMediaTestRepositories(t)
	task := &model.VideoTask{UserID: 7, Filename: "lesson.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	kb := &model.KnowledgeBase{UserID: 7, Name: "检索课程"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(AgentSnapshot{Version: 2, Steps: []AgentSnapshotStep{}, Citations: []Citation{{TaskID: task.ID, Content: "向量检索"}}})
	snapshot := string(encoded)
	question := &model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "user", Content: "向量检索如何工作？"}
	answer := &model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "assistant", Content: "先生成向量，再检索相似片段。", RetrievalSnapshot: &snapshot}
	if err := repos.Chat.CreateExchange(7, question, answer, []int64{task.ID}); err != nil {
		t.Fatal(err)
	}
	client := &questionTestClient{response: `{"questions":["哪些场景适合向量检索？","检索不准确时怎么办？"]}`}
	svc := NewQuestionSuggestionService(repos, stubConversationProfileProvider{}, questionTestFactory{client})
	if got, err := svc.FollowUpQuestions(context.Background(), 7, session.ID, answer.ID); err != nil || len(got.Questions) != 2 || client.calls != 1 {
		t.Fatalf("owned got=%+v err=%v calls=%d", got, err, client.calls)
	}
	if err := repos.KnowledgeBase.RemoveVideoForUser(7, kb.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.FollowUpQuestions(context.Background(), 7, session.ID, answer.ID)
	if err == nil || len(got.Questions) != 0 || client.calls != 1 {
		t.Fatalf("removed got=%+v err=%v calls=%d", got, err, client.calls)
	}
}
