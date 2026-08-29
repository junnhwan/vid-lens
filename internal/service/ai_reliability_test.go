package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/quota"
	"vid-lens/internal/repository"
)

// Spec 06 降级链行为测试（spec 第 112 行：internal/service/ai_reliability_test.go）。
//
// 只测外部行为（spec Testing Decisions）：
//   - 档1：rerank 失败 → 回退向量基线（fake reranker 注入失败，断言降级链可用、
//     fallback 标记命中、档1 计数 +1）。
//   - 档2：LLM 失败 → 无 LLM 模式（fake LLM 注入失败，断言返回 degraded 答案而非
//     错误、答案含检索片段 + 已有摘要、degraded:true、档2 计数 +1）。
//   - 档2 复用 spec 03 FindByMD5 跨 task 摘要：另一 task 的成功摘要被拼进降级答案。
//   - ExecutionPolicy UseLLM=false 的 intent 不误触发档2。
//   - admission RetryAfter 超阈值触发档2、阈值内不降级（admission 协同）。
//
// 复用 chat_ask_test.go / chat_test.go 的 fake LLM/reranker 范式（spec 第 117 行）。

// reliabilityFailingChatClient 注入 LLM 失败，用于触发档2降级。
type reliabilityFailingChatClient struct {
	err error
}

func (c *reliabilityFailingChatClient) Chat(context.Context, []ai.ChatMessage) (string, error) {
	return "", c.err
}

func (c *reliabilityFailingChatClient) StreamChat(context.Context, []ai.ChatMessage, func(string) error) error {
	return c.err
}

// newReliabilityTestService 构造一个 strict_rag 路径的 ChatService，retriever 返回
// 固定片段，reranker 通过 ModelRerankerFactory 注入（可让 fake reranker 失败/成功）。
// repos 由 caller 传入（task/session 在该 repos 上创建，避免"无权访问此会话"）。
func newReliabilityTestService(t *testing.T, repos *repository.Repositories, retriever RAGRetriever, rerankerFactory func(ai.Profile) Reranker) *ChatService {
	t.Helper()
	cfg := DefaultRAGRetrievalConfig()
	// QueryModeRewrite + RewriteQueries=3 与 direct_qa policy.Rewrite=3 一致，
	// 避免 applyPolicy 把 RewriteQueries 改成 3 后与 QueryMode 冲突触发 Validate 失败。
	cfg.QueryMode = QueryModeRewrite
	cfg.RewriteQueries = 3
	cfg.NeighborRadius = 0
	cfg.RerankerMode = RerankerModeModel
	cfg.RerankerVersion = "fake-reranker"
	chatCfg := ChatConfig{TopK: 5, MinScore: 0.3, RecentTurns: 8, Retrieval: &cfg}
	if rerankerFactory != nil {
		chatCfg.ModelRerankerFactory = rerankerFactory
	}
	return NewChatService(repos, retriever, chatCfg)
}

func TestDegradationTier1RerankFailureFallsBackToVectorBaseline(t *testing.T) {
	resetDegradationCountersForTest()
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "t1t1t1t1t1t1t1t1t1t1t1t1t1t1t1", Filename: "v.mp4", FileURL: "videos/t1.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "s"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// 注入失败的 model reranker（fake rerank client 返回 error）。
	svc := newReliabilityTestService(t, repos, &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Score: 0.82, Content: "rerank 失败后仍应可用的向量基线片段"},
	}}, func(ai.Profile) Reranker {
		return NewModelReranker(&fakeRerankClient{err: errors.New("rerank service down")})
	})

	result, err := svc.Ask(context.Background(), 7, session.ID, "片段里讲了什么？", 0, &fakeEmbeddingClient{dim: 3}, &recordingChatClient{}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v, want degraded answer not error (档1)", err)
	}
	if result.Answer == "" {
		t.Fatal("Answer empty, want LLM-generated answer on tier1 (rerank fallback still runs LLM)")
	}
	// 档1 不标 degraded（LLM 仍生成完整答案，决策记录第 10 节 degraded 只针对档2）。
	if result.Degraded {
		t.Fatal("Degraded=true on tier1, want false (rerank fallback still produces full LLM answer)")
	}
	if DegradationTier1Triggers() != 1 {
		t.Fatalf("tier1 triggers = %d, want 1", DegradationTier1Triggers())
	}
	if DegradationTier2Triggers() != 0 {
		t.Fatalf("tier2 triggers = %d, want 0 on rerank-only failure", DegradationTier2Triggers())
	}
}

func TestDegradationTier2LLMFailureReturnsDegradedAnswerWithChunks(t *testing.T) {
	resetDegradationCountersForTest()
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "t2t2t2t2t2t2t2t2t2t2t2t2t2t2t2", Filename: "v.mp4", FileURL: "videos/t2.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "s"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// 不配 reranker（DeterministicReranker 默认），LLM 注入失败 → 档2。
	svc := newReliabilityTestService(t, repos, &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Score: 0.82, Content: "档2 降级应拼进答案的检索片段"},
	}}, nil)

	result, err := svc.Ask(context.Background(), 7, session.ID, "LLM 挂了还能答吗？", 0, &fakeEmbeddingClient{dim: 3}, &reliabilityFailingChatClient{err: errors.New("llm gateway 5xx")}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v, want degraded answer not error (档2 稀缺点)", err)
	}
	if !result.Degraded {
		t.Fatal("Degraded=false, want true (档2 LLM 失败→无 LLM 模式)")
	}
	if !strings.Contains(result.Answer, "档2 降级应拼进答案的检索片段") {
		t.Fatalf("Answer = %q, want retrieved chunks direct-joined", result.Answer)
	}
	if DegradationTier2Triggers() != 1 {
		t.Fatalf("tier2 triggers = %d, want 1", DegradationTier2Triggers())
	}
	if DegradationTier1Triggers() != 0 {
		t.Fatalf("tier1 triggers = %d, want 0 when rerank succeeded", DegradationTier1Triggers())
	}
}

func TestDegradationTier2ReusesSummaryByMD5AcrossTasks(t *testing.T) {
	resetDegradationCountersForTest()
	repos := newChatServiceTestRepositories(t)
	// 另一个 task 持有相同 file_md5 的成功摘要（spec 03 FindByMD5 跨 task 复用）。
	sourceTask := &model.VideoTask{UserID: 99, FileMD5: "sharemd5sharemd5sharemd5share", Filename: "src.mp4", FileURL: "videos/src.mp4"}
	if err := repos.Task.Create(sourceTask); err != nil {
		t.Fatalf("create source task: %v", err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: sourceTask.ID, FileMD5: "sharemd5sharemd5sharemd5share", Content: "跨 task 复用的成功摘要：讲分布式锁 owner 校验。", ModelName: "chat-model"}); err != nil {
		t.Fatalf("create summary: %v", err)
	}
	// 当前 task 用相同 file_md5（不同 task、不同 user），无自有摘要。
	task := &model.VideoTask{UserID: 7, FileMD5: "sharemd5sharemd5sharemd5share", Filename: "cur.mp4", FileURL: "videos/cur.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "s"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	svc := newReliabilityTestService(t, repos, &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 1, Score: 0.8, Content: "检索片段内容"},
	}}, nil)

	result, err := svc.Ask(context.Background(), 7, session.ID, "LLM 挂了，摘要还能用吗？", 0, &fakeEmbeddingClient{dim: 3}, &reliabilityFailingChatClient{err: errors.New("llm down")}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v, want degraded answer", err)
	}
	if !result.Degraded {
		t.Fatal("Degraded=false, want true")
	}
	if !strings.Contains(result.Answer, "跨 task 复用的成功摘要") {
		t.Fatalf("Answer = %q, want summary reused via FindByMD5", result.Answer)
	}
}

func TestDegradationUseLLMFalseIntentDoesNotTriggerTier2(t *testing.T) {
	resetDegradationCountersForTest()
	// small_talk intent = UseLLM=false（ExecutionPolicy）。但占位分类器在 strict_rag
	// 下产出 direct_qa（UseLLM=true），故此测验证 shouldTriggerLLMDegradation 的纯逻辑：
	// UseLLM=false 的 policy 即便 LLM 失败也不降级。
	policy := ExecutionPolicy{Retrieve: false, UseLLM: false}
	if shouldTriggerLLMDegradation(policy, errors.New("llm err")) {
		t.Fatal("UseLLM=false triggered tier2, want no degradation (本来就不调 LLM)")
	}
	policyUseLLM := ExecutionPolicy{Retrieve: true, UseLLM: true}
	if !shouldTriggerLLMDegradation(policyUseLLM, errors.New("llm err")) {
		t.Fatal("UseLLM=true with llm err did not trigger tier2")
	}
}

func TestDegradationAdmissionRetryAfterOverCutoffTriggersTier2(t *testing.T) {
	resetDegradationCountersForTest()
	policy := ExecutionPolicy{UseLLM: true}
	// admission 拒配额 RetryAfter=10s（超 5s 阈值）→ 触发档2。
	longRetry := &ai.AdmissionError{Decision: quota.Decision{Allowed: false, Scope: "provider", RetryAfter: 10 * time.Second}}
	if !shouldTriggerLLMDegradation(policy, longRetry) {
		t.Fatal("RetryAfter=10s over cutoff did not trigger tier2")
	}
	// RetryAfter=2s（阈值内）→ 不触发档2（由 caller 重试，admission 协同 spec 06）。
	shortRetry := &ai.AdmissionError{Decision: quota.Decision{Allowed: false, Scope: "provider", RetryAfter: 2 * time.Second}}
	if shouldTriggerLLMDegradation(policy, shortRetry) {
		t.Fatal("RetryAfter=2s under cutoff triggered tier2, want retry path")
	}
}

func TestDegradationTier2StreamReturnsDegradedAnswer(t *testing.T) {
	resetDegradationCountersForTest()
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "t2s2t2s2t2s2t2s2t2s2t2s2t2s2", Filename: "v.mp4", FileURL: "videos/t2s.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "s"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	svc := newReliabilityTestService(t, repos, &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 1, Score: 0.8, Content: "流式档2 降级片段"},
	}}, nil)

	var events []ChatStreamEvent
	result, err := svc.AskStream(context.Background(), 7, session.ID, "流式 LLM 挂了？", 0, &fakeEmbeddingClient{dim: 3}, &reliabilityFailingChatClient{err: errors.New("stream llm down")}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	}, func(event ChatStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("AskStream() error = %v, want degraded answer", err)
	}
	if !result.Degraded {
		t.Fatal("Degraded=false, want true on stream tier2")
	}
	if !strings.Contains(result.Answer, "流式档2 降级片段") {
		t.Fatalf("Answer = %q, want chunks direct-joined", result.Answer)
	}
	hasDegradedDone := false
	for _, e := range events {
		if e.Type == "done" {
			if m, ok := e.Data.(map[string]interface{}); ok && m["degraded"] == true {
				hasDegradedDone = true
			}
		}
	}
	if !hasDegradedDone {
		t.Fatal("done event did not carry degraded:true")
	}
	if DegradationTier2Triggers() != 1 {
		t.Fatalf("tier2 triggers = %d, want 1", DegradationTier2Triggers())
	}
}

// TestDegradationAdmissionProviderErrorRetryAfterHonored 验证 429 ProviderError 的
// RetryAfter 也走 admissionRetryAfterCutoff 语义（spec 06 协同一致性）：短 RetryAfter
// 不降级（走重试路径）、长 RetryAfter 触发档2。修复 review 指出的"429 短 RetryAfter
// 误触发档2"问题。
func TestDegradationAdmissionProviderErrorRetryAfterHonored(t *testing.T) {
	resetDegradationCountersForTest()
	policy := ExecutionPolicy{UseLLM: true}
	// 429 限流 RetryAfter=2s（阈值内）→ 不触发档2（走重试）。
	short429 := &ai.ProviderError{Class: ai.ErrorRateLimited, StatusCode: 429, Retryable: true, RetryAfter: 2 * time.Second}
	if shouldTriggerLLMDegradation(policy, short429) {
		t.Fatal("429 RetryAfter=2s under cutoff triggered tier2, want retry path")
	}
	// 429 限流 RetryAfter=10s（超阈值）→ 触发档2。
	long429 := &ai.ProviderError{Class: ai.ErrorRateLimited, StatusCode: 429, Retryable: true, RetryAfter: 10 * time.Second}
	if !shouldTriggerLLMDegradation(policy, long429) {
		t.Fatal("429 RetryAfter=10s over cutoff did not trigger tier2")
	}
}

// TestDegradationChainTierCount 断言降级链档数 = 3（档0正常/档1 rerank→向量基线/档2
// LLM→无LLM模式），回填 spec 06 数字占位符"降级链档数 3"。
func TestDegradationChainTierCount(t *testing.T) {
	// DegradationLevel 枚举覆盖档0/档1/档2 三档。
	levels := []DegradationLevel{DegradationNone, DegradationRerankFallback, DegradationLLMUnavailable}
	if len(levels) != 3 {
		t.Fatalf("degradation tiers = %d, want 3", len(levels))
	}
	if DegradationNone.Degraded() || DegradationRerankFallback.Degraded() || !DegradationLLMUnavailable.Degraded() {
		t.Fatal("Degraded() flag mismatch: only tier2 (LLM unavailable) is degraded:true")
	}
}

// TestDegradationAvailabilityRate 回填 spec 06 数字占位符"降级可用率"：
// LLM 失败场景下返回 degraded 答案而非错误的占比。造 N 个 LLM 失败场景（UseLLM=true
// 的 direct_qa intent + fake LLM 注入失败），断言 100% 返回 degraded 答案（非错误）。
// 这是 spec 06 决策记录第 6 节稀缺点的直接验收：LLM 失败 → 回退无 LLM 模式而非全废。
func TestDegradationAvailabilityRate(t *testing.T) {
	resetDegradationCountersForTest()
	const scenarios = 5
	returnedDegraded := 0
	for i := 0; i < scenarios; i++ {
		repos := newChatServiceTestRepositories(t)
		task := &model.VideoTask{UserID: 7, FileMD5: "rate" + string(rune('a'+i)) + "0123456789abcdef012345", Filename: "v.mp4", FileURL: "videos/r.mp4"}
		if err := repos.Task.Create(task); err != nil {
			t.Fatalf("create task: %v", err)
		}
		session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "s"}
		if err := repos.Chat.CreateSession(session); err != nil {
			t.Fatalf("create session: %v", err)
		}
		svc := newReliabilityTestService(t, repos, &fakeRetriever{results: []RetrievedChunk{
			{ChunkID: 1, ChunkIndex: 1, Score: 0.8, Content: "可用率场景的检索片段"},
		}}, nil)
		result, err := svc.Ask(context.Background(), 7, session.ID, "问题", 0, &fakeEmbeddingClient{dim: 3}, &reliabilityFailingChatClient{err: errors.New("llm unavailable")}, ai.Profile{
			EmbeddingModel: "text-embedding-3-small",
			LLMModel:       "chat-model",
		})
		if err != nil {
			t.Fatalf("scenario %d: Ask() error = %v, want degraded answer not error", i, err)
		}
		if result.Degraded {
			returnedDegraded++
		}
	}
	rate := returnedDegraded * 100 / scenarios
	if rate != 100 {
		t.Fatalf("degradation availability rate = %d%%, want 100%% (returned=%d/%d)", rate, returnedDegraded, scenarios)
	}
}
