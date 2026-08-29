package service

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// docs/architecture/retrieval.md (A段) 行为验收：同一问题在不同 intent/scope 下走不同检索参数。
// 复用 chat_ask_test.go 的 fake retriever / scripted chat client 范式，只测外部可观察
// 差异（是否检索、BM25 开关、跨视频召回、recent 历史关断），不测 ExecutionPolicy
// 内部 struct 字段赋值细节。

// newPolicyChatFixture 建一个单视频会话 + 摘要 + 可空检索器，用于断言 policy 路由行为。
func newPolicyChatFixture(t *testing.T, retriever RAGRetriever) (*ChatService, *model.ChatSession, *model.VideoTask) {
	t.Helper()
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "p0p0p0p0p0p0p0p0p0p0p0p0p0p0p0p0", Filename: "policy.mp4", FileURL: "videos/policy.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: task.ID, Content: "视频摘要：分布式锁 owner 校验与租约恢复。", ModelName: "chat"}); err != nil {
		t.Fatalf("create summary: %v", err)
	}
	svc := NewChatService(repos, retriever, ChatConfig{TopK: 5, MinScore: 0.3, RecentTurns: 8})
	return svc, session, task
}

func TestExecutionPolicyVideoOverviewSkipsRetrieval(t *testing.T) {
	// video_overview intent：关检索走视频上下文直拼，不该打 embedding/检索。
	retriever := &fakeRetriever{}
	embedding := &fakeEmbeddingClient{dim: 3}
	chat := &scriptedChatClient{responses: []string{"概览回答"}}
	svc, session, _ := newPolicyChatFixture(t, retriever)

	result, err := svc.AskWithMode(context.Background(), ChatModeVideoAssistant, 7, session.ID, "简要讲这个视频说了什么", 0, embedding, chat, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err != nil {
		t.Fatalf("AskWithMode() error = %v", err)
	}
	if result.Answer != "概览回答" {
		t.Fatalf("answer = %q", result.Answer)
	}
	if len(embedding.inputs) != 0 {
		t.Fatalf("overview should skip embedding/retrieval, got inputs=%+v", embedding.inputs)
	}
	if len(result.Citations) != 0 {
		t.Fatalf("overview should produce no citations, got %+v", result.Citations)
	}
	joined := ""
	for _, msg := range chat.messages[0] {
		joined += msg.Content + "\n"
	}
	if !strings.Contains(joined, "视频摘要") {
		t.Fatalf("overview prompt should include summary, got %s", joined)
	}
}

func TestExecutionPolicyDirectQARunsRetrieval(t *testing.T) {
	// direct_qa intent：开检索 + rerank，embedding 被打、有 citations。
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{TaskID: 1, ChunkID: 9, ChunkIndex: 0, Score: 0.8, Content: "owner 校验释放锁"},
	}}
	embedding := &fakeEmbeddingClient{dim: 3}
	chat := &scriptedChatClient{responses: []string{"direct_qa 需要 owner。[C1]"}}
	svc, session, _ := newPolicyChatFixture(t, retriever)

	result, err := svc.AskWithMode(context.Background(), ChatModeStrictRAG, 7, session.ID, "谁要校验 owner", 0, embedding, chat, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err != nil {
		t.Fatalf("AskWithMode() error = %v", err)
	}
	if len(embedding.inputs) == 0 {
		t.Fatal("direct_qa should run embedding/retrieval")
	}
	if len(result.Citations) != 1 {
		t.Fatalf("direct_qa citations = %+v", result.Citations)
	}
}

func TestExecutionPolicyKnowledgeBaseIsCollectionScopeAndPureVector(t *testing.T) {
	// topic_compare/series_locate intent → Scope=collection：检索过滤到 KB 内 video_ids，
	// BM25 关（多 task 不支持）、纯向量；recent 历史关断（member-safe）。
	repos := newChatServiceTestRepositories(t)
	taskA := &model.VideoTask{UserID: 7, FileMD5: "pa1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a", Filename: "a.mp4", FileURL: "videos/a.mp4", Title: "视频 A"}
	taskB := &model.VideoTask{UserID: 7, FileMD5: "pb2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2", Filename: "b.mp4", FileURL: "videos/b.mp4"}
	if err := repos.Task.Create(taskA); err != nil {
		t.Fatal(err)
	}
	if err := repos.Task.Create(taskB); err != nil {
		t.Fatal(err)
	}
	for _, task := range []*model.VideoTask{taskA, taskB} {
		if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: 7, TaskID: task.ID, FileMD5: task.FileMD5, EmbeddingModel: "embed", EmbeddingDim: 3, Status: model.RAGIndexStatusIndexed}); err != nil {
			t.Fatal(err)
		}
	}
	kb := &model.KnowledgeBase{UserID: 7, Name: "KB"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{taskA.ID, taskB.ID} {
		if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID, Title: kb.Name}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	// KB 历史里塞一条应被关断的旧答案。
	if err := repos.Chat.CreateMessage(&model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "assistant", Content: "被移除视频的旧答案不该回灌"}); err != nil {
		t.Fatal(err)
	}

	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{
			{{TaskID: taskA.ID, EvidenceID: "a-1", ChunkID: 11, Content: "A 的 owner 校验"}, {TaskID: taskB.ID, EvidenceID: "b-1", ChunkID: 21, Content: "B 的租约恢复"}},
	}}
	chat := &scriptedChatClient{responses: []string{`{"queries":["对比 owner"]}`, "A 校验 owner。[C1] B 租约恢复。[C2]"}}
	cfg := DefaultRAGRetrievalConfig()
	cfg.NeighborRadius = 0
	svc := NewChatService(repos, retriever, ChatConfig{TopK: 5, Retrieval: &cfg})
	memory := &fakeChatMemoryStore{recent: []model.ChatMessage{{Role: "assistant", Content: "KB Redis 旧历史也不该回灌"}}}
	svc.SetMemoryStore(memory)

	result, err := svc.AskWithMode(context.Background(), ChatModeVideoAssistant, 7, session.ID, "两个视频怎么处理失败恢复", 0, &fakeEmbeddingClient{dim: 3}, chat, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err != nil {
		t.Fatalf("AskWithMode() error = %v", err)
	}
	// Scope=collection → 过滤到 KB 内 video_ids（两成员）。
	wantIDs := []int64{taskA.ID, taskB.ID}
	if len(retriever.requests) == 0 || !reflect.DeepEqual(retriever.requests[0].TaskIDs, wantIDs) {
		t.Fatalf("collection scope task ids = %+v, want %v", retriever.requests, wantIDs)
	}
	// recent 历史关断：KB 成员的旧答案不该进 prompt。
	for _, call := range chat.messages {
		for _, msg := range call {
			if strings.Contains(msg.Content, "被移除视频") || strings.Contains(msg.Content, "KB Redis 旧历史") {
				t.Fatalf("KB history leaked into generation: %+v", chat.messages)
			}
		}
	}
	if memory.getCalls != 0 {
		t.Fatalf("collection scope should cut recent memory, get calls=%d", memory.getCalls)
	}
	if len(result.Citations) != 2 {
		t.Fatalf("cross-video citations = %+v", result.Citations)
	}
}

func TestExecutionPolicyCollectionScopeDisablesBM25(t *testing.T) {
	// Scope=collection 直接断言 BM25 被关（policy→config 行为可观察差异）。
	// 用 seedVideoChunks + 真 BM25 路径：若 BM25 仍开，多 task 会在 Retrieve 内报错
	// "multi-task retrieval does not support BM25"；关掉则只走向量不报错。
	repos := newChatServiceTestRepositories(t)
	seedVideoChunks(t, repos, 7, 1, "embed", []string{"kw-one"})
	seedVideoChunks(t, repos, 7, 2, "embed", []string{"kw-two"})
	retriever := &pipelineTestRetriever{results: [][]RetrievedChunk{{{EvidenceID: "v1", ChunkID: 1, Content: "向量片段"}}}}
	cfg := DefaultRAGRetrievalConfig()
	cfg.NeighborRadius = 0
	pipeline := &RetrievalPipeline{repos: repos, retriever: retriever, Config: &cfg}

	// 不 applyPolicy（仍 BM25=on）：多 task 应报错。
	_, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{UserID: 7, TaskIDs: []int64{1, 2}, Question: "q", EmbeddingModel: "embed", Embedding: &fakeEmbeddingClient{dim: 3}})
	if err == nil || !strings.Contains(err.Error(), "multi-task retrieval does not support BM25") {
		t.Fatalf("baseline multi-task BM25 err = %v, want BM25 unsupported", err)
	}

	// applyPolicy(collection) 关 BM25：多 task 走纯向量不报错。
	pipeline2 := &RetrievalPipeline{repos: repos, retriever: retriever, Config: &cfg}
	pipeline2.applyPolicy(PolicyFor(IntentTopicCompare, ScopeCollection))
	if _, err := pipeline2.Retrieve(context.Background(), RetrievalPipelineRequest{UserID: 7, TaskIDs: []int64{1, 2}, Question: "q", EmbeddingModel: "embed", Embedding: &fakeEmbeddingClient{dim: 3}}); err != nil {
		t.Fatalf("collection scope should disable BM25 and allow multi-task vector retrieval, err = %v", err)
	}
	if pipeline2.Config.EnableBM25 {
		t.Fatalf("collection scope should set EnableBM25=false, got cfg=%+v", pipeline2.Config)
	}
}

func TestExecutionPolicyRerankSwitchMapsToRerankerMode(t *testing.T) {
	// policy.Rerank=false → RerankerMode=none / reranker 置 nil（关 rerank）。
	// policy.Rerank=true → 保留已配置的 reranker（不退回 none）。
	pipeline := &RetrievalPipeline{reranker: DeterministicReranker{}, Config: ptrRAGConfig(DefaultRAGRetrievalConfig())}

	pipeline.applyPolicy(PolicyFor(IntentDirectQA, ScopeVideo)) // Rerank=true
	if pipeline.Config.RerankerMode == RerankerModeNone || pipeline.reranker == nil {
		t.Fatalf("direct_qa Rerank=true should keep configured reranker, got mode=%q reranker=%T", pipeline.Config.RerankerMode, pipeline.reranker)
	}
	if pipeline.reranker == nil {
		t.Fatal("direct_qa should keep reranker non-nil")
	}

	// overview 的 Rerank=false（虽然 overview 关检索不走到 applyPolicy，但 policy 字段
	// 映射行为应可观察）。
	pipeline.applyPolicy(PolicyFor(IntentVideoOverview, ScopeVideo)) // Rerank=false
	if pipeline.Config.RerankerMode != RerankerModeNone || pipeline.reranker != nil {
		t.Fatalf("overview Rerank=false should set RerankerMode=none and nil reranker, got mode=%q reranker=%T", pipeline.Config.RerankerMode, pipeline.reranker)
	}
}

func TestExecutionPolicyPlaceholderClassifierIntent(t *testing.T) {
	// 占位分类器（docs/architecture/retrieval.md 真分类器落地前的接口位）的 intent 判定。
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	kbSession := &model.ChatSession{ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: 9}

	cases := []struct {
		question string
		session  *model.ChatSession
		mode     ChatMode
		want     Intent
	}{
		{"简要讲这个视频说了什么", videoSession, ChatModeVideoAssistant, IntentVideoOverview},
		{"谁要校验 owner", videoSession, ChatModeVideoAssistant, IntentDirectQA},
		{"谁要校验 owner", videoSession, ChatModeStrictRAG, IntentDirectQA},
		{"两个视频怎么处理失败恢复", kbSession, ChatModeVideoAssistant, IntentTopicCompare},
		{"总结一下这些视频", kbSession, ChatModeVideoAssistant, IntentSeriesLocate}, // KB 概览 → 跨视频召回，不关检索
	}
	for _, c := range cases {
		got := classifyIntentPlaceholder(c.question, c.session, c.mode)
		if got != c.want {
			t.Fatalf("classifyIntentPlaceholder(%q, scope=%v, mode=%v) = %q, want %q", c.question, c.session.ScopeType, c.mode, got, c.want)
		}
	}
}

func TestExecutionPolicySmallTalkLeavesRetrievalOff(t *testing.T) {
	// small_talk policy 留接口位（真分类器在 docs/architecture/retrieval.md）：关检索关 LLM。
	// 占位分类器不产出 small_talk，故只断言 PolicyFor 的可观察字段，不接端到端。
	policy := PolicyFor(IntentSmallTalk, ScopeVideo)
	if policy.Retrieve || policy.UseLLM || policy.UseSummary {
		t.Fatalf("small_talk policy should leave retrieval+LLM off, got %+v", policy)
	}
}

func TestExecutionPolicyTimelineLocateLeavesSignalInterface(t *testing.T) {
	// timeline_locate 开检索 + Signal 时间戳过滤接口位（Signal 提取留 docs/architecture/retrieval.md，
	// 本 spec 只确认 Retrieve 开、参数同 direct_qa）。
	policy := PolicyFor(IntentTimelineLocate, ScopeVideo)
	if !policy.Retrieve || !policy.Rerank || !policy.UseLLM {
		t.Fatalf("timeline_locate policy should retrieve + rerank + LLM, got %+v", policy)
	}
}

func TestExecutionPolicyUnknownIntentFallsBackToDirectQA(t *testing.T) {
	policy := PolicyFor(Intent("unknown_intent"), ScopeVideo)
	direct := PolicyFor(IntentDirectQA, ScopeVideo)
	if !reflect.DeepEqual(policy, direct) {
		t.Fatalf("unknown intent should fall back to direct_qa, got %+v want %+v", policy, direct)
	}
}

// ClampTopK 把原 prepareRAGChat 的散落 if（topK 默认值 + topK>10→10）统一表达。
func TestExecutionPolicyClampTopKReplacesScatteredIf(t *testing.T) {
	policy := PolicyFor(IntentDirectQA, ScopeVideo) // TopK=5
	cases := []struct{ in, want int }{
		{0, 5},      // <=0 → policy 默认
		{3, 3},      // caller 传值在 [1,10] 内透传
		{10, 10},    // 上限边界
		{50, 10},    // 超上限截到 MaxRetrievalTopK
		{-1, 5},     // 负值归默认
	}
	for _, c := range cases {
		got := policy.ClampTopK(c.in)
		if got != c.want {
			t.Fatalf("ClampTopK(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// 保证 video_overview 在 strict_rag 模式下不被占位分类器产出（strict 必须检索）。
func TestExecutionPolicyStrictRAGNeverClassifiesOverview(t *testing.T) {
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	got := classifyIntentPlaceholder("简要讲这个视频说了什么", videoSession, ChatModeStrictRAG)
	if got == IntentVideoOverview {
		t.Fatalf("strict_rag must not classify overview (关检索), got %q", got)
	}
	if got != IntentDirectQA {
		t.Fatalf("strict_rag overview question should fall to direct_qa, got %q", got)
	}
}

// errNoRetrievedContext 兜底断言（避免静默吞错）。

func ptrRAGConfig(c RAGRetrievalConfig) *RAGRetrievalConfig { return &c }
