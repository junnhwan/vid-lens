package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type collectionTestRetriever struct {
	mu      sync.Mutex
	calls   []RetrievalRequest
	fail    error
	foreign bool
}

func (r *collectionTestRetriever) Search(ctx context.Context, v []float32, req RetrievalRequest) ([]RetrievedChunk, error) {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	r.mu.Unlock()
	if r.fail != nil {
		return nil, r.fail
	}
	if r.foreign {
		return []RetrievedChunk{{TaskID: 99, Content: "foreign"}}, nil
	}
	ids := req.TaskIDs
	if len(ids) == 0 {
		ids = []int64{req.TaskID}
	}
	var result []RetrievedChunk
	if len(ids) > 1 {
		ids = ids[:1]
	}
	for _, id := range ids {
		for i := 0; i < 12; i++ {
			result = append(result, RetrievedChunk{TaskID: id, EvidenceID: fmt.Sprintf("%d:%d", id, i), Content: fmt.Sprintf("来源%d 恢复成本事实%d", id, i), Score: .9})
		}
	}
	return result, nil
}

type collectionEmbedding struct{ count atomic.Int32 }

func (e *collectionEmbedding) Embed(context.Context, string) ([]float32, error) {
	e.count.Add(1)
	return []float32{1}, nil
}

func TestCollectionRequestTopKAndTargetRecallReuseEmbedding(t *testing.T) {
	cfg := DefaultRAGRetrievalConfig()
	cfg.TopK = 5
	cfg.EnableBM25 = false
	cfg.QueryMode = QueryModeOriginal
	cfg.RewriteQueries = 1
	retriever := &collectionTestRetriever{}
	embedding := &collectionEmbedding{}
	p := &RetrievalPipeline{Config: &cfg, retriever: retriever, rewriter: NoopQueryRewriter{}}
	out, err := p.Retrieve(context.Background(), RetrievalPipelineRequest{TaskIDs: []int64{1, 2}, RequiredTaskIDs: []int64{1, 2}, Question: "比较恢复成本", TopK: 8, Embedding: embedding})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Citations) != 8 || out.Trace.TopK != 8 || cfg.TopK != 5 || embedding.count.Load() != 1 {
		t.Fatalf("out=%+v embedding=%d cfg=%+v", out, embedding.count.Load(), cfg)
	}
	if out.Citations[0].TaskID != 1 || out.Citations[1].TaskID != 2 {
		t.Fatalf("comparison partner lost: %+v", out.Citations)
	}
	if len(retriever.calls) != 3 {
		t.Fatalf("calls=%+v", retriever.calls)
	}
}

func TestCollectionChannelDegradationAndScopeNeverDegrades(t *testing.T) {
	repos, _, ids := knowledgeAgentFixture(t)
	cfg := DefaultRAGRetrievalConfig()
	cfg.QueryMode = QueryModeOriginal
	cfg.RewriteQueries = 1
	for _, test := range []struct {
		name    string
		err     error
		foreign bool
		wantErr error
	}{
		{"vector unavailable", errors.New("provider unavailable"), false, nil},
		{"cancelled", context.Canceled, false, context.Canceled},
		{"foreign evidence", nil, true, errRetrievalScope},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := &RetrievalPipeline{repos: repos, Config: &cfg, retriever: &collectionTestRetriever{fail: test.err, foreign: test.foreign}}
			out, err := p.Retrieve(context.Background(), RetrievalPipelineRequest{UserID: 7, TaskIDs: ids, Question: "owner", EmbeddingModel: "embed", Embedding: &collectionEmbedding{}, TopK: 2})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil || len(out.Citations) != 2 || !containsFallback(out.Trace.Fallbacks, "vector_channel_failed") {
				t.Fatalf("out=%+v err=%v", out, err)
			}
		})
	}
}

func TestCollectionDiversityPreservesIndependentConsensusAndNegation(t *testing.T) {
	got := selectDiverseEvidence([]RetrievedChunk{{TaskID: 1, Content: "只能在租约有效时恢复。"}, {TaskID: 1, Content: "只能在租约有效时恢复。"}, {TaskID: 1, Content: "不能在租约失效后恢复。"}, {TaskID: 2, Content: "只能在租约有效时恢复。"}}, 5, []int64{1, 2})
	if len(got) != 3 {
		t.Fatalf("evidence=%+v", got)
	}
	if !strings.Contains(got[2].Content, "不能") {
		t.Fatalf("negation lost: %+v", got)
	}
}

func TestCollectionPartialAvailabilityFreezesAllOwnedMembers(t *testing.T) {
	repos, session, ids := knowledgeAgentFixture(t)
	index, err := repos.RAGIndex.FindByTaskAndModel(7, ids[1], "embed")
	if err != nil {
		t.Fatal(err)
	}
	index.Status = model.RAGIndexStatusIndexing
	if err := repos.RAGIndex.Upsert(index); err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(repos, &fakeRetriever{}, ChatConfig{})
	for _, scopeType := range []string{model.ChatScopeKnowledgeBase, model.ChatScopeVideoLibrary} {
		copy := *session
		copy.ScopeType = scopeType
		scope, err := svc.sessionRetrievalScope(7, &copy, "embed")
		if err != nil || !sameTaskIDs(scope.Members, ids) || !sameTaskIDs(scope.Ready, ids[:1]) || !strings.Contains(scope.coveragePrompt(), "部分结果") {
			t.Fatalf("scope=%+v err=%v", scope, err)
		}
	}
}

func TestVideoLibraryAgentPersistsAndResumesWithSafeSourceEdges(t *testing.T) {
	repos, _, ids := knowledgeAgentFixture(t)
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeVideoLibrary}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 5})
	agent := NewVideoAgentService(svc)
	chat := &scriptedChatClient{responses: []string{testSearchDecision, `{"done":true,"stop_reason":"ready"}`, "两方 owner 校验 [C1][C2]"}}
	result, err := agent.RunAgent(context.Background(), VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "比较 owner 校验", RunID: "library-research"}, &fakeEmbeddingClient{dim: 3}, chat, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	edges, err := repos.Chat.ListSourceTaskIDsByMessageID(7, result.MessageID)
	if err != nil || !sameTaskIDs(edges, ids) {
		t.Fatalf("sources=%v err=%v", edges, err)
	}
	restored, err := agent.ResumeAgent(context.Background(), 7, result.RunID, &fakeEmbeddingClient{dim: 3}, chat, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err != nil || restored.MessageID != result.MessageID {
		t.Fatalf("resume=%+v err=%v", restored, err)
	}
	if _, err := agent.findVideoAgentSession(7, session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCitationClaimLinksAndVerbatimSupportReview(t *testing.T) {
	source := "只有租约有效时才能恢复。租约失效时不能重试。"
	finalized := finalizeAnswerCitations("租约失效时不能重试 [C1]。没有依据 [C99]。", []Citation{{CitationID: "C1", AnchorQuote: source, Content: source, DisplayContext: source}})
	if len(finalized.Citations) != 1 || len(finalized.Citations[0].ClaimTexts) != 1 || !strings.Contains(finalized.Answer, "[C1]") {
		t.Fatalf("claim offsets=%+v", finalized)
	}
	for _, tt := range []struct{ response, status string }{
		{`[{"id":"C1","supported":true,"quote":"租约失效时不能重试。"}]`, "supported"},
		{`[{"id":"C1","supported":true,"quote":"租约失效时可以重试。"}]`, "review_invalid"},
		{`[{"id":"C1","supported":false,"quote":"租约失效时不能重试。"}]`, "unsupported"},
	} {
		got, _, err := reviewCitationSupport(context.Background(), &scriptedChatClient{responses: []string{tt.response}}, finalized.Answer, finalized.Citations)
		if err != nil || got[0].SupportStatus != tt.status {
			t.Fatalf("review=%+v err=%v", got, err)
		}
	}
}

func TestTemporalContextRejectsMixedModalityGapsAndUnknownTime(t *testing.T) {
	rows := []model.VideoChunk{
		{TaskID: 1, ChunkIndex: 0, Content: "条件先行句", Modality: "transcript", StartMS: 0, EndMS: 1000, TimeRangeStatus: "exact", SourceMappingStatus: "mapped"},
		{TaskID: 1, ChunkIndex: 1, Content: "画面文字", Modality: "visual_ocr", StartMS: 500, EndMS: 1500, TimeRangeStatus: "exact", SourceMappingStatus: "mapped"},
		{TaskID: 1, ChunkIndex: 2, Content: "这种方式仅限租约有效", Modality: "transcript", StartMS: 1000, EndMS: 2000, TimeRangeStatus: "exact", SourceMappingStatus: "mapped"},
		{TaskID: 1, ChunkIndex: 3, Content: "十秒后其他话题", Modality: "transcript", StartMS: 12000, EndMS: 13000, TimeRangeStatus: "exact", SourceMappingStatus: "mapped"},
		{TaskID: 1, ChunkIndex: 4, Content: "未知时间", Modality: "transcript", TimeRangeStatus: "unknown", SourceMappingStatus: "mapped"},
	}
	got := continuousChunkWindow(rows, 2, 3)
	if len(got) != 2 || got[0].ChunkIndex != 0 || got[1].ChunkIndex != 2 {
		t.Fatalf("window=%+v", got)
	}
	if len(continuousChunkWindow(rows, 4, 3)) != 1 {
		t.Fatal("unknown time widened")
	}
}

func TestCollectionSummaryRouteFindsNinthVideoAndOverviewReadsEveryPage(t *testing.T) {
	repos, _, _ := knowledgeAgentFixture(t)
	var ids []int64
	for i := 0; i < 15; i++ {
		task := &model.VideoTask{UserID: 7, FileMD5: fmt.Sprintf("%032d", i+100), Filename: fmt.Sprintf("lecture%d.mp4", i), Title: fmt.Sprintf("课程%02d", i)}
		if err := repos.Task.Create(task); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
		summary := "普通介绍"
		if i == 9 {
			summary = "租约恢复条件与成本"
		}
		if err := repos.Summary.Create(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: summary}); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewChatService(repos, nil, ChatConfig{})
	route, err := svc.routeCollection(context.Background(), 7, ids, "租约恢复条件", nil)
	if err != nil || len(route.Maps) == 0 || route.Maps[0].TaskID != ids[9] {
		t.Fatalf("route=%+v err=%v", route, err)
	}
	client := &scriptedChatClient{responses: []string{"第一页主题导航", "第二页主题导航"}}
	overview, err := svc.routeCollection(context.Background(), 7, ids, "全库有哪些共同主题", client)
	if err != nil || len(client.messages) != 2 || !strings.Contains(overview.Overview, fmt.Sprint(ids[14])) {
		t.Fatalf("overview=%+v calls=%d err=%v", overview, len(client.messages), err)
	}
}

// A request-aware fixture models the vector store's scope filtering and supports
// concurrent collection/target searches without depending on goroutine order.
type scopedFixtureRetriever struct {
	mu       sync.Mutex
	results  []RetrievedChunk
	requests []RetrievalRequest
}

func (r *scopedFixtureRetriever) Search(_ context.Context, _ []float32, req RetrievalRequest) ([]RetrievedChunk, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	ids := req.TaskIDs
	if len(ids) == 0 {
		ids = []int64{req.TaskID}
	}
	var out []RetrievedChunk
	for _, c := range r.results {
		if containsTaskID(ids, c.TaskID) {
			out = append(out, c)
		}
	}
	return out, nil
}

func TestPlannerCompactsSourceRefsAndCollectionNavigation(t *testing.T) {
	state := VideoAgentLoopState{Goal: "比较成本", CollectionContext: strings.Repeat("完整导航概要。", 6000)}
	for i := 0; i < 8; i++ {
		c := RetrievedChunk{TaskID: int64(i%2 + 1), EvidenceID: fmt.Sprint(i), Content: strings.Repeat("原文。", 800)}
		for j := 0; j < 200; j++ {
			c.SourceRefs = append(c.SourceRefs, ChunkSourceRef{StableID: strings.Repeat("source", 50)})
			c.ContextSourceRefs = append(c.ContextSourceRefs, c.SourceRefs[j])
		}
		state.Evidence = append(state.Evidence, c)
	}
	messages, err := buildPlannerMessages(state, NewVideoAgentTools(nil, nil, nil).Registry().Definitions())
	if err != nil || estimatedPlannerCallUsage(messages, "").PromptTokens > 8192 {
		t.Fatalf("messages exceed bounded context: %v", err)
	}
	if len(state.Evidence[0].SourceRefs) != 200 || len([]rune(state.CollectionContext)) != 42000 {
		t.Fatal("compaction mutated durable state")
	}
}
