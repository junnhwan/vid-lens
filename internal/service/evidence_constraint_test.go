package service

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// docs/architecture/retrieval.md ⑨ 轻量证据约束行为测试（当前实现约束：internal/service/evidence_constraint_test.go）。
//
// 只测外部行为（spec 测试约定）：
//   - 引用在范围内 → 保留绑定（fake LLM 产出 [C1] [C2] 合法引用，断言 citations 含 C1/C2）。
//   - 超范围引用被拒 + 触发单轮重检索补证据（fake LLM 注入 [C5] 编造编号或没检索到的片段，
//     断言违规计数 +1、重检索计数 +1、补到证据后保留结论）。
//   - 重检索后仍无证据 → 标注"无证据支撑"拒绝输出该结论（重检索返回空，断言 unsupported 计数 +1、
//     答案含"无证据支撑"标注、citations 为空）。
//   - 重检索有上限不无限循环（maxEvidenceReRetrieval=1，断言重检索至多一次）。
//   - 约束链档数 = 2（校验 + 重检索，回填 docs/architecture/retrieval.md 待评测指标）。
//
// 直接测 enforceEvidenceConstraint 的外部行为（docs/architecture/retrieval.md 验收 seam = internal/service 的证据
// 约束行为），fake re-retriever 注入控制证据补全（docs/architecture/retrieval.md 测试约定：fake LLM 注入
// 超范围引用，断言约束链）。另有一个 Ask 端到端测试验证接进现有 chat 链路无回归。

func newEvidencePrepared(citations []Citation, contexts []RetrievedChunk) *preparedRAGChat {
	return &preparedRAGChat{
		Session:   &model.ChatSession{UserID: 7},
		Question:  "问题",
		Contexts:  contexts,
		Citations: citations,
	}
}

// scriptReRetriever 按调用顺序返回重检索结果，控制"补到证据" vs "仍无证据"。
type scriptReRetriever struct {
	results [][]RetrievedChunk
	calls   int
}

func (r *scriptReRetriever) reRetrieve(_ context.Context, _ string) ([]RetrievedChunk, error) {
	idx := r.calls
	r.calls++
	if idx < len(r.results) {
		return r.results[idx], nil
	}
	return r.results[len(r.results)-1], nil
}

// scriptedRetriever 在每次 Search 调用按 responses 顺序返回结果（控制首轮检索 vs 重检索证据）。
// 用于 Ask 端到端不回归测试。
type scriptedRetriever struct {
	responses [][]RetrievedChunk
	calls     int
}

func (r *scriptedRetriever) Search(_ context.Context, _ []float32, req RetrievalRequest) ([]RetrievedChunk, error) {
	_ = req
	idx := r.calls
	r.calls++
	if idx < len(r.responses) {
		return r.responses[idx], nil
	}
	return r.responses[len(r.responses)-1], nil
}

func TestEvidenceConstraintInRangeCitationsKept(t *testing.T) {
	resetEvidenceConstraintCountersForTest()
	contexts := []RetrievedChunk{
		{EvidenceID: "ev-in-1", ChunkID: 1, ChunkIndex: 0, Content: "第一条证据：工具结果反馈路径。"},
		{EvidenceID: "ev-in-2", ChunkID: 2, ChunkIndex: 1, Content: "第二条证据：消息边界处理。"},
	}
	citations := []Citation{
		{CitationID: "C1", EvidenceID: "ev-in-1", ChunkID: 1, Content: "第一条证据：工具结果反馈路径。"},
		{CitationID: "C2", EvidenceID: "ev-in-2", ChunkID: 2, Content: "第二条证据：消息边界处理。"},
	}
	prepared := newEvidencePrepared(citations, contexts)
	re := &scriptReRetriever{results: nil} // 不应被调用（无违规）。
	svc := NewChatService(nil, nil, ChatConfig{})

	outcome := svc.enforceEvidenceConstraint(context.Background(), prepared, "工具结果按消息边界反馈 [C1][C2]", re.reRetrieve)

	if len(outcome.citations) != 2 || outcome.citations[0].CitationID != "C1" || outcome.citations[1].CitationID != "C2" {
		t.Fatalf("in-range citations = %+v, want C1 and C2 kept", outcome.citations)
	}
	if strings.Contains(outcome.answer, "[C") {
		t.Fatalf("answer = %q, want citation tokens stripped", outcome.answer)
	}
	if outcome.violated {
		t.Fatal("violated=true, want false on in-range citations")
	}
	if outcome.reretrieved {
		t.Fatal("reretrieved=true, want false on in-range")
	}
	if re.calls != 0 {
		t.Fatalf("re-retriever calls = %d, want 0 (no violation)", re.calls)
	}
	if EvidenceViolationTriggers() != 0 {
		t.Fatalf("violation triggers = %d, want 0", EvidenceViolationTriggers())
	}
}

func TestEvidenceConstraintOutOfRangeCitationTriggersReretrievalAndPatchesEvidence(t *testing.T) {
	resetEvidenceConstraintCountersForTest()
	// 首轮检索集只有 C1（evidence ev-out-1）；LLM 编造引用 [C3]（超范围）。
	contexts := []RetrievedChunk{
		{EvidenceID: "ev-out-1", ChunkID: 1, ChunkIndex: 0, Content: "唯一真实证据：重试状态持久化路径。"},
	}
	citations := []Citation{
		{CitationID: "C1", EvidenceID: "ev-out-1", ChunkID: 1, Content: "唯一真实证据：重试状态持久化路径。"},
	}
	prepared := newEvidencePrepared(citations, contexts)
	// 重检索补到一条新证据（不同 evidence id），并入候选集后 LLM 的 [C3] 仍超范围（candidates
	// 长度变 2，C3 仍编造），但并入证据让 finalize 能绑回 C1/C2 之一 → 保留结论。
	re := &scriptReRetriever{results: [][]RetrievedChunk{{
		{EvidenceID: "ev-out-patch", ChunkID: 2, ChunkIndex: 1, Content: "重检索补到的新证据：重试状态持久化细节。"},
	}}}
	svc := NewChatService(nil, nil, ChatConfig{})

	outcome := svc.enforceEvidenceConstraint(context.Background(), prepared, "重试状态会持久化 [C3]", re.reRetrieve)

	if !outcome.violated {
		t.Fatal("violated=false, want true on out-of-range [C3]")
	}
	if !outcome.reretrieved {
		t.Fatal("reretrieved=false, want true (violation triggered re-retrieval)")
	}
	if re.calls != 1 {
		t.Fatalf("re-retriever calls = %d, want 1 (single re-retrieval, bounded)", re.calls)
	}
	if EvidenceViolationTriggers() != 1 {
		t.Fatalf("violation triggers = %d, want 1", EvidenceViolationTriggers())
	}
	if EvidenceReretrievalTriggers() != 1 {
		t.Fatalf("reretrieval triggers = %d, want 1", EvidenceReretrievalTriggers())
	}
	// 补到证据后保留结论：答案含 LLM 结论文本、引用集非空（绑回并入证据）。
	if !strings.Contains(outcome.answer, "重试状态会持久化") {
		t.Fatalf("answer = %q, want patched conclusion kept", outcome.answer)
	}
	if len(outcome.citations) == 0 {
		t.Fatalf("citations empty, want patched evidence bound back (got %+v)", outcome.citations)
	}
	if outcome.unsupported {
		t.Fatal("unsupported=true, want false (re-retrieval patched evidence)")
	}
	if EvidenceUnsupportedTriggers() != 0 {
		t.Fatalf("unsupported triggers = %d, want 0", EvidenceUnsupportedTriggers())
	}
}

func TestEvidenceConstraintReretrievalEmptyMarksUnsupported(t *testing.T) {
	resetEvidenceConstraintCountersForTest()
	// 首轮检索集只有 C1（与 LLM 结论无关）；LLM 编造 [C5]；重检索返回空（仍无证据）。
	contexts := []RetrievedChunk{
		{EvidenceID: "ev-unsup-1", ChunkID: 1, ChunkIndex: 0, Content: "唯一真实证据：与 LLM 结论无关的内容。"},
	}
	citations := []Citation{
		{CitationID: "C1", EvidenceID: "ev-unsup-1", ChunkID: 1, Content: "唯一真实证据：与 LLM 结论无关的内容。"},
	}
	prepared := newEvidencePrepared(citations, contexts)
	re := &scriptReRetriever{results: [][]RetrievedChunk{nil}} // 重检索返回空。
	svc := NewChatService(nil, nil, ChatConfig{})

	outcome := svc.enforceEvidenceConstraint(context.Background(), prepared, "编造的结论需要 [C5] 支撑", re.reRetrieve)

	if !outcome.unsupported {
		t.Fatal("unsupported=false, want true (re-retrieval empty)")
	}
	if EvidenceUnsupportedTriggers() != 1 {
		t.Fatalf("unsupported triggers = %d, want 1", EvidenceUnsupportedTriggers())
	}
	if EvidenceViolationTriggers() != 1 {
		t.Fatalf("violation triggers = %d, want 1", EvidenceViolationTriggers())
	}
	if EvidenceReretrievalTriggers() != 1 {
		t.Fatalf("reretrieval triggers = %d, want 1", EvidenceReretrievalTriggers())
	}
	// 答案含"无证据支撑"标注（docs/architecture/retrieval.md 行为约束 3 诚实拒绝而非硬编）。
	if !strings.Contains(outcome.answer, "无证据支撑") {
		t.Fatalf("answer = %q, want unsupported annotation", outcome.answer)
	}
	// 无证据支撑的结论不绑回 Citation。
	if len(outcome.citations) != 0 {
		t.Fatalf("citations = %+v, want empty on unsupported", outcome.citations)
	}
}

// TestEvidenceConstraintChainTierCount 回填 docs/architecture/retrieval.md 待评测指标"证据约束链档数 2"：
// 校验 + 重检索 = 2 档（单轮轻量，非多轮 ReAct）。
func TestEvidenceConstraintChainTierCount(t *testing.T) {
	if evidenceConstraintChainTiers != 2 {
		t.Fatalf("evidence constraint chain tiers = %d, want 2 (validation + re-retrieval, docs/architecture/retrieval.md)", evidenceConstraintChainTiers)
	}
	if maxEvidenceReRetrieval != 1 {
		t.Fatalf("maxEvidenceReRetrieval = %d, want 1 (single re-retrieval, docs/architecture/retrieval.md lightweight)", maxEvidenceReRetrieval)
	}
}

// TestEvidenceConstraintNoReretrievalLoop 验证不无限循环（docs/architecture/retrieval.md 行为约束 13）：
// 重检索有上限 maxEvidenceReRetrieval=1，无论重检索结果如何都不二次重检索。
func TestEvidenceConstraintNoReretrievalLoop(t *testing.T) {
	resetEvidenceConstraintCountersForTest()
	contexts := []RetrievedChunk{
		{EvidenceID: "ev-loop-1", ChunkID: 1, ChunkIndex: 0, Content: "唯一证据：与 LLM 结论无关。"},
	}
	citations := []Citation{
		{CitationID: "C1", EvidenceID: "ev-loop-1", ChunkID: 1, Content: "唯一证据：与 LLM 结论无关。"},
	}
	prepared := newEvidencePrepared(citations, contexts)
	// 重检索返回【原检索集已有】的重复证据（无新 evidence id）：LLM 的 [C5] 仍超范围，
	// 且重检索没补到新证据 → 标"无证据支撑"。但重检索不二次触发（上限 1 次）。
	re := &scriptReRetriever{results: [][]RetrievedChunk{{
		{EvidenceID: "ev-loop-1", ChunkID: 1, ChunkIndex: 0, Content: "唯一证据：与 LLM 结论无关。"},
	}}}
	svc := NewChatService(nil, nil, ChatConfig{})

	outcome := svc.enforceEvidenceConstraint(context.Background(), prepared, "编造结论 [C5]", re.reRetrieve)

	// 重检索至多一次（maxEvidenceReRetrieval=1，不无限循环）。
	if EvidenceReretrievalTriggers() != 1 {
		t.Fatalf("reretrieval triggers = %d, want 1 (bounded, no loop)", EvidenceReretrievalTriggers())
	}
	if re.calls != 1 {
		t.Fatalf("re-retriever calls = %d, want 1 (no infinite loop)", re.calls)
	}
	// 重检索只带回重复（无新证据）→ 标"无证据支撑"（spec review A1 修复后的正确语义：
	// "仍无证据"指没补到支撑该结论的新证据，而非 chunk 数为 0）。
	if !outcome.unsupported {
		t.Fatal("unsupported=false, want true (re-retrieval returned only duplicates, no new evidence)")
	}
	if EvidenceUnsupportedTriggers() != 1 {
		t.Fatalf("unsupported triggers = %d, want 1 (no new evidence)", EvidenceUnsupportedTriggers())
	}
}

// TestEvidenceConstraintReretrievalOnlyDuplicatesMarksUnsupported 显式覆盖 spec review
// A1 指出的真失败模式：重检索返回了 chunk（非空），但全是原检索集已有的重复 evidence id
// → 不能因 len(finalized.Citations)>0 误判"补到证据"（finalize fallback 会撑到 ≥1），
// 必须判"无新 evidence id" → 标"无证据支撑"。
func TestEvidenceConstraintReretrievalOnlyDuplicatesMarksUnsupported(t *testing.T) {
	resetEvidenceConstraintCountersForTest()
	contexts := []RetrievedChunk{
		{EvidenceID: "ev-dup-1", ChunkID: 1, ChunkIndex: 0, Content: "原检索集已有证据。"},
	}
	citations := []Citation{
		{CitationID: "C1", EvidenceID: "ev-dup-1", ChunkID: 1, Content: "原检索集已有证据。"},
	}
	prepared := newEvidencePrepared(citations, contexts)
	// 重检索返回非空但全是重复 evidence id（与原检索集 ev-dup-1 相同）。
	re := &scriptReRetriever{results: [][]RetrievedChunk{{
		{EvidenceID: "ev-dup-1", ChunkID: 1, ChunkIndex: 0, Content: "原检索集已有证据。"},
	}}}
	svc := NewChatService(nil, nil, ChatConfig{})

	outcome := svc.enforceEvidenceConstraint(context.Background(), prepared, "编造结论 [C5]", re.reRetrieve)

	if !outcome.unsupported {
		t.Fatal("unsupported=false, want true — re-retrieval returned only duplicates; finalize fallback must NOT mask the missing-evidence case")
	}
	if len(outcome.citations) != 0 {
		t.Fatalf("citations = %+v, want empty (no new evidence supporting the conclusion)", outcome.citations)
	}
	if !strings.Contains(outcome.answer, "无证据支撑") {
		t.Fatalf("answer = %q, want unsupported annotation", outcome.answer)
	}
	if EvidenceUnsupportedTriggers() != 1 {
		t.Fatalf("unsupported triggers = %d, want 1", EvidenceUnsupportedTriggers())
	}
}

// TestEvidenceConstraintAskPathNoRegression 验证证据约束接进 Ask 链路后，合法引用场景
// 不回归（docs/architecture/retrieval.md"现有 chat_ask_test.go 作为不回归保障"）。fake LLM 产出合法 [C1]，
// 断言答案清洗、引用保留、无违规/重检索计数。
func TestEvidenceConstraintAskPathNoRegression(t *testing.T) {
	resetEvidenceConstraintCountersForTest()
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "evregregregregregregregregregr", Filename: "v.mp4", FileURL: "videos/evreg.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "s"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// QueryModeRewrite + RewriteQueries=3 与 direct_qa policy.Rewrite=3 一致，避免
	// applyPolicy 把 RewriteQueries 改成 3 后与 QueryMode 冲突（同 ai_reliability_test）。
	cfg := DefaultRAGRetrievalConfig()
	cfg.EnableBM25 = false
	chatCfg := ChatConfig{TopK: 5, MinScore: 0.3, RecentTurns: 8, Retrieval: &cfg}
	results := []RetrievedChunk{
		{EvidenceID: "ev-ask-1", ChunkID: 1, ChunkIndex: 0, Content: "合法证据：owner 校验路径。"},
	}
	retriever := &scriptedRetriever{responses: [][]RetrievedChunk{results}}
	// scriptedChatClient：第一条 = rewrite 的 JSON（合法则用改写 query，但 fakeRetriever
	// 固定返回，不影响），第二条 = 答案，合法引用 [C1]。
	chatClient := &scriptedChatClient{responses: []string{
		`{"queries":["owner 校验风险"]}`,
		"需要校验 owner [C1]",
	}}
	svc := NewChatService(repos, retriever, chatCfg)

	result, err := svc.Ask(context.Background(), 7, session.ID, "为什么校验 owner？", 0, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if !strings.Contains(result.Answer, "需要校验 owner") {
		t.Fatalf("answer = %q, want LLM conclusion kept", result.Answer)
	}
	if strings.Contains(result.Answer, "[C") {
		t.Fatalf("answer = %q, want citation tokens stripped", result.Answer)
	}
	if len(result.Citations) != 1 || result.Citations[0].CitationID != "C1" {
		t.Fatalf("citations = %+v, want C1 kept", result.Citations)
	}
	if EvidenceViolationTriggers() != 0 || EvidenceReretrievalTriggers() != 0 {
		t.Fatalf("counters = violation=%d reretrieval=%d, want 0/0 on in-range", EvidenceViolationTriggers(), EvidenceReretrievalTriggers())
	}
}
