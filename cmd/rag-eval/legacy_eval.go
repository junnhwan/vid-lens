package main

import (
	"context"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"sort"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/ragtool"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/vector"
)

type evalCase struct {
	TaskID                int64    `yaml:"task_id"`
	TaskHint              string   `yaml:"task_hint"`
	Category              string   `yaml:"category"`
	Question              string   `yaml:"question"`
	ExpectedChunkKeywords []string `yaml:"expected_chunk_keywords"`
	ExpectedAnswerPoints  []string `yaml:"expected_answer_points"`
}

type profileBundle struct {
	profile   *ai.Profile
	embedding ai.EmbeddingClient
}

type caseEvalContext struct {
	evalCase   evalCase
	userID     int64
	profile    *ai.Profile
	embedding  ai.EmbeddingClient
	vector     []float32
	rewrite    service.RewriteResult
	rewriteErr error
}

type modeResult struct {
	mode   string
	report ragtool.RAGEvalReport
}

func run(parent context.Context, opts evalOptions) error {
	if opts.strict {
		return runStrict(parent, opts)
	}
	progress := newEvalProgress(opts.progress, os.Stderr)
	cases, err := loadCases(opts.casesPath)
	if err != nil {
		return err
	}
	progress.stage("loaded %d cases from %s", len(cases), opts.casesPath)
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	if !cfg.RAG.Enabled {
		return fmt.Errorf("RAG is disabled in config")
	}
	if err := validateEvalConfig(cfg); err != nil {
		return err
	}

	topK := opts.topK
	if topK <= 0 {
		topK = cfg.RAG.TopK
	}
	if topK <= 0 {
		topK = 5
	}
	candidateK := opts.candidateK
	if candidateK <= 0 {
		candidateK = cfg.RAG.CandidateK
	}
	if candidateK < topK {
		candidateK = topK
	}

	ctx, cancel := context.WithTimeout(parent, opts.timeout)
	defer cancel()

	progress.stage("connecting PostgreSQL and vector backend %s", vector.NormalizeBackendName(cfg.RAG.Store))
	connection, err := openEvalDatabase(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer connection.Close()

	repos := repository.NewRepositories(connection.GORM)
	codecSecret := cfg.Security.APIKeySecret
	if codecSecret == "" {
		codecSecret = cfg.JWT.Secret
	}
	codec, err := secret.NewCodecFromPassphrase(codecSecret)
	if err != nil {
		return fmt.Errorf("init secret codec: %w", err)
	}
	profiles := service.NewAIProfileService(repos.AIProfile, codec, nil)
	factory := ai.NewFactory()

	store, err := newConfiguredVectorStore(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect vector backend %q: %w", vector.NormalizeBackendName(cfg.RAG.Store), err)
	}
	defer store.Close()

	preflight, err := preflightCases(ctx, cases, evalPreflightSources{
		tasks: repos.Task, chunks: repos.VideoChunk, profiles: profiles, vectors: store,
	}, cfg.RAG.Store)
	if err != nil {
		return err
	}
	progress.stage("preflight backend=%s cases=%d tasks=%d ready=%d invalid=%d", preflight.Backend, preflight.CaseCount, len(preflight.Tasks), preflight.ReadyCases, preflight.InvalidCases)
	if opts.preflightOnly {
		if !preflight.Valid() {
			return errors.New(preflight.Error())
		}
		fmt.Printf("rag eval preflight passed: backend=%s cases=%d tasks=%d\n", preflight.Backend, preflight.CaseCount, len(preflight.Tasks))
		return nil
	}
	if !preflight.Valid() {
		return errors.New(preflight.Error())
	}

	caseContexts, embeddingModel, taskIDs, err := prepareCases(ctx, cases, repos, profiles, factory, opts.rerankEndpoint, opts.rerankModel, progress)
	if err != nil {
		return err
	}

	progress.stage("evaluating retrieval mode: Vector only")
	vectorReport, err := evaluateVectorOnly(ctx, caseContexts, store, topK, progress)
	if err != nil {
		return err
	}
	progress.stage("evaluating retrieval mode: Vector + BM25 + RRF")
	hybridReport, err := evaluateHybrid(ctx, caseContexts, store, repos, topK, candidateK, progress)
	if err != nil {
		return err
	}
	progress.stage("evaluating retrieval mode: Rewrite + MultiQuery + RRF")
	rewriteReport, err := evaluateRewritePipeline(ctx, caseContexts, store, repos, factory, topK, candidateK, false, progress)
	if err != nil {
		return err
	}
	progress.stage("evaluating retrieval mode: Rewrite + MultiQuery + RRF + Window + Rerank")
	fullReport, err := evaluateRewritePipeline(ctx, caseContexts, store, repos, factory, topK, candidateK, true, progress)
	if err != nil {
		return err
	}
	retrievalResults := []modeResult{
		{mode: "Vector only", report: vectorReport},
		{mode: "Vector + BM25 + RRF", report: hybridReport},
		{mode: "Rewrite + MultiQuery + RRF", report: rewriteReport},
		{mode: "Rewrite + MultiQuery + RRF + Window + Rerank", report: fullReport},
	}
	if opts.legacyModelRerankEnabled() {
		progress.stage("evaluating retrieval mode: Rewrite + MultiQuery + RRF + Window + Model Rerank")
		modelRerankReport, err := evaluateModelRerankPipeline(ctx, caseContexts, store, repos, factory, topK, candidateK, progress)
		if err != nil {
			return err
		}
		retrievalResults = append(retrievalResults, modeResult{
			mode: "Rewrite + MultiQuery + RRF + Window + Model Rerank", report: modelRerankReport,
		})
	}
	progress.stage("evaluating answer modes: ordinary RAG vs agentic")
	answerResults := evaluateAnswerModes(ctx, caseContexts, store, repos, factory, topK, candidateK, progress)
	markdown := renderMarkdownWithAgentAnswerEval(opts, taskIDs, len(cases), embeddingModel, topK, candidateK, retrievalResults, answerResults)
	if err := os.MkdirAll(parentDir(opts.outputPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(opts.outputPath, []byte(markdown), 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", opts.outputPath)
	return nil
}

func loadCases(path string) ([]evalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cases: %w", err)
	}
	var cases []evalCase
	if err := yaml.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("parse cases: %w", err)
	}
	for i, c := range cases {
		if c.TaskID <= 0 {
			return nil, fmt.Errorf("case %d missing task_id", i+1)
		}
		if strings.TrimSpace(c.Question) == "" {
			return nil, fmt.Errorf("case %d missing question", i+1)
		}
		if len(c.ExpectedChunkKeywords) == 0 {
			return nil, fmt.Errorf("case %d missing expected_chunk_keywords", i+1)
		}
	}
	return cases, nil
}

func prepareCases(ctx context.Context, cases []evalCase, repos *repository.Repositories, profiles *service.AIProfileService, factory *ai.Factory, rerankEndpoint, rerankModel string, progress evalProgress) ([]caseEvalContext, string, []int64, error) {
	profileCache := make(map[int64]profileBundle)
	taskIDSet := make(map[int64]bool)
	taskIDs := make([]int64, 0)
	var embeddingModel string
	prepared := make([]caseEvalContext, 0, len(cases))

	for i, c := range cases {
		progress.caseStep("prepare", i+1, len(cases), c)
		task, err := repos.Task.FindByID(c.TaskID)
		if err != nil {
			return nil, "", nil, fmt.Errorf("find task %d: %w", c.TaskID, err)
		}
		bundle, ok := profileCache[task.UserID]
		if !ok {
			profile, err := profiles.GetDefaultAIProfile(task.UserID)
			if err != nil {
				return nil, "", nil, fmt.Errorf("load default AI profile for user %d: %w", task.UserID, err)
			}
			profile.RerankEndpoint = strings.TrimSpace(rerankEndpoint)
			profile.RerankModel = strings.TrimSpace(rerankModel)
			embedding, err := factory.NewEmbeddingClient(*profile)
			if err != nil {
				return nil, "", nil, fmt.Errorf("create embedding client for user %d: %w", task.UserID, err)
			}
			bundle = profileBundle{profile: profile, embedding: embedding}
			profileCache[task.UserID] = bundle
		}
		progress.caseStep("embedding", i+1, len(cases), c)
		vector, err := bundle.embedding.Embed(ctx, c.Question)
		if err != nil {
			return nil, "", nil, fmt.Errorf("embed question for task %d: %w", c.TaskID, err)
		}
		progress.caseStep("rewrite", i+1, len(cases), c)
		rewrite, rewriteErr := newEvalRewriter(factory, *bundle.profile).Rewrite(ctx, service.RewriteInput{
			Question:   c.Question,
			NumQueries: 3,
		})
		rewrite = normalizeEvalRewrite(c.Question, rewrite)
		if embeddingModel == "" {
			embeddingModel = bundle.profile.EmbeddingModel
		} else if embeddingModel != bundle.profile.EmbeddingModel {
			embeddingModel = embeddingModel + ", " + bundle.profile.EmbeddingModel
		}
		if !taskIDSet[c.TaskID] {
			taskIDSet[c.TaskID] = true
			taskIDs = append(taskIDs, c.TaskID)
		}
		prepared = append(prepared, caseEvalContext{
			evalCase:   c,
			userID:     task.UserID,
			profile:    bundle.profile,
			embedding:  bundle.embedding,
			vector:     vector,
			rewrite:    rewrite,
			rewriteErr: rewriteErr,
		})
	}
	sort.Slice(taskIDs, func(i, j int) bool { return taskIDs[i] < taskIDs[j] })
	return prepared, embeddingModel, taskIDs, nil
}

func evaluateVectorOnly(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, topK int, progress evalProgress) (ragtool.RAGEvalReport, error) {
	results := make([]ragtool.RAGEvalCaseResult, 0, len(cases))
	for i, c := range cases {
		progress.caseStep("vector search", i+1, len(cases), c.evalCase)
		startedAt := time.Now()
		chunks, err := store.Search(ctx, c.vector, service.RetrievalRequest{
			UserID:         c.userID,
			TaskID:         c.evalCase.TaskID,
			EmbeddingModel: c.profile.EmbeddingModel,
			TopK:           topK,
		})
		duration := time.Since(startedAt)
		if err != nil {
			return ragtool.RAGEvalReport{}, fmt.Errorf("vector search task %d: %w", c.evalCase.TaskID, err)
		}
		for i := range chunks {
			chunks[i].Source = service.RetrievalSourceVector
			chunks[i].VectorRank = i + 1
		}
		results = append(results, ragtool.RAGEvalCaseResult{
			Case:      c.evalCase.serviceCase(),
			Citations: chunks,
			Duration:  duration,
		})
	}
	return ragtool.EvaluateRAGRetrieval(results, topK), nil
}

func evaluateHybrid(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, topK, candidateK int, progress evalProgress) (ragtool.RAGEvalReport, error) {
	results := make([]ragtool.RAGEvalCaseResult, 0, len(cases))
	for i, c := range cases {
		progress.caseStep("hybrid retrieval", i+1, len(cases), c.evalCase)
		startedAt := time.Now()
		vectorChunks, err := store.Search(ctx, c.vector, service.RetrievalRequest{
			UserID:         c.userID,
			TaskID:         c.evalCase.TaskID,
			EmbeddingModel: c.profile.EmbeddingModel,
			TopK:           candidateK,
		})
		if err != nil {
			return ragtool.RAGEvalReport{}, fmt.Errorf("hybrid vector search task %d: %w", c.evalCase.TaskID, err)
		}
		terms := service.ExtractQueryTerms(c.evalCase.Question)
		keywordResults, err := repos.VideoChunk.SearchByBM25(c.userID, c.evalCase.TaskID, c.profile.EmbeddingModel, terms, candidateK)
		if err != nil {
			return ragtool.RAGEvalReport{}, fmt.Errorf("BM25 search task %d: %w", c.evalCase.TaskID, err)
		}
		keywordChunks := make([]service.RetrievedChunk, 0, len(keywordResults))
		for _, result := range keywordResults {
			keywordChunks = append(keywordChunks, service.RetrievedChunk{
				ChunkID:     result.Chunk.ID,
				ChunkIndex:  result.Chunk.ChunkIndex,
				Score:       float32(result.Score),
				Content:     result.Chunk.Content,
				Source:      service.RetrievalSourceKeyword,
				KeywordRank: result.Rank,
			})
		}
		chunks := service.FuseRetrievedChunks(vectorChunks, keywordChunks, topK, 0)
		duration := time.Since(startedAt)
		results = append(results, ragtool.RAGEvalCaseResult{
			Case:      c.evalCase.serviceCase(),
			Citations: chunks,
			Duration:  duration,
		})
	}
	return ragtool.EvaluateRAGRetrieval(results, topK), nil
}

func evaluateRewritePipeline(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int, full bool, progress evalProgress) (ragtool.RAGEvalReport, error) {
	results := make([]ragtool.RAGEvalCaseResult, 0, len(cases))
	for i, c := range cases {
		progress.caseStep("rewrite pipeline retrieval", i+1, len(cases), c.evalCase)
		var expander *service.ContextExpander
		var reranker service.Reranker
		if full {
			expander = service.NewContextExpander(repos, 1, 4000)
			reranker = service.DeterministicReranker{}
		}
		pipeline := service.NewRetrievalPipeline(
			repos,
			store,
			cachedEvalRewriter{result: c.rewrite, err: c.rewriteErr},
			expander,
			reranker,
			candidateK,
			0,
		)
		startedAt := time.Now()
		result, err := pipeline.Retrieve(ctx, service.RetrievalPipelineRequest{
			UserID:         c.userID,
			TaskID:         c.evalCase.TaskID,
			Question:       c.evalCase.Question,
			TopK:           topK,
			EmbeddingModel: c.profile.EmbeddingModel,
			Embedding:      c.embedding,
		})
		duration := time.Since(startedAt)
		if err != nil {
			return ragtool.RAGEvalReport{}, fmt.Errorf("pipeline eval task %d: %w", c.evalCase.TaskID, err)
		}
		results = append(results, ragtool.RAGEvalCaseResult{
			Case:                 c.evalCase.serviceCase(),
			Citations:            result.Citations,
			Duration:             duration,
			RewriteFallback:      result.Rewrite.Fallback,
			ExpandedContextChars: citationContentChars(result.Citations),
			RerankChangedRank:    rerankChangedRank(result.Citations),
		})
	}
	return ragtool.EvaluateRAGRetrieval(results, topK), nil
}

type rerankClientFactory interface {
	NewRerankClient(profile ai.Profile) (ai.RerankClient, error)
}

func newLegacyModelReranker(factory rerankClientFactory, profile ai.Profile) service.Reranker {
	if strings.TrimSpace(profile.RerankModel) == "" {
		return service.NewModelReranker(nil)
	}
	client, err := factory.NewRerankClient(profile)
	if err != nil {
		return service.NewModelReranker(nil)
	}
	return service.NewModelReranker(client)
}

func evaluateModelRerankPipeline(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int, progress evalProgress) (ragtool.RAGEvalReport, error) {
	results := make([]ragtool.RAGEvalCaseResult, 0, len(cases))
	for i, c := range cases {
		progress.caseStep("model rerank retrieval", i+1, len(cases), c.evalCase)
		expander := service.NewContextExpander(repos, 1, 4000)
		reranker := newLegacyModelReranker(factory, *c.profile)
		pipeline := service.NewRetrievalPipeline(
			repos,
			store,
			cachedEvalRewriter{result: c.rewrite, err: c.rewriteErr},
			expander,
			reranker,
			candidateK,
			0,
		)
		startedAt := time.Now()
		result, err := pipeline.Retrieve(ctx, service.RetrievalPipelineRequest{
			UserID:         c.userID,
			TaskID:         c.evalCase.TaskID,
			Question:       c.evalCase.Question,
			TopK:           topK,
			EmbeddingModel: c.profile.EmbeddingModel,
			Embedding:      c.embedding,
		})
		duration := time.Since(startedAt)
		if err != nil {
			return ragtool.RAGEvalReport{}, fmt.Errorf("model rerank pipeline eval task %d: %w", c.evalCase.TaskID, err)
		}
		results = append(results, ragtool.RAGEvalCaseResult{
			Case:                 c.evalCase.serviceCase(),
			Citations:            result.Citations,
			Duration:             duration,
			RewriteFallback:      result.Rewrite.Fallback,
			ExpandedContextChars: citationContentChars(result.Citations),
			RerankChangedRank:    rerankChangedRank(result.Citations),
		})
	}
	return ragtool.EvaluateRAGRetrieval(results, topK), nil
}

func newEvalRewriter(factory *ai.Factory, profile ai.Profile) service.QueryRewriter {
	if strings.TrimSpace(profile.LLMProvider) == "" ||
		strings.TrimSpace(profile.LLMBaseURL) == "" ||
		strings.TrimSpace(profile.LLMAPIKey) == "" ||
		strings.TrimSpace(profile.LLMModel) == "" {
		return evalFallbackRewriter{reason: "LLM rewrite profile is incomplete"}
	}
	chat, err := factory.NewChatClient(profile)
	if err != nil {
		return evalFallbackRewriter{reason: err.Error()}
	}
	return service.NewLLMQueryRewriter(chat)
}

type evalFallbackRewriter struct {
	reason string
}

func (r evalFallbackRewriter) Rewrite(_ context.Context, input service.RewriteInput) (service.RewriteResult, error) {
	original := strings.TrimSpace(input.Question)
	if original == "" {
		return service.RewriteResult{}, fmt.Errorf("问题不能为空")
	}
	if r.reason == "" {
		r.reason = "rewrite unavailable"
	}
	return service.RewriteResult{
		Original: original,
		Queries:  []string{original},
		Fallback: true,
	}, fmt.Errorf("%s", r.reason)
}

type cachedEvalRewriter struct {
	result service.RewriteResult
	err    error
}

func (r cachedEvalRewriter) Rewrite(_ context.Context, _ service.RewriteInput) (service.RewriteResult, error) {
	return r.result, r.err
}

func normalizeEvalRewrite(question string, rewrite service.RewriteResult) service.RewriteResult {
	original := strings.TrimSpace(rewrite.Original)
	if original == "" {
		original = strings.TrimSpace(question)
	}
	rewrite.Original = original
	if len(rewrite.Queries) == 0 {
		rewrite.Queries = []string{original}
		rewrite.Fallback = true
	}
	return rewrite
}

func (c evalCase) serviceCase() ragtool.RAGEvalCase {
	return ragtool.RAGEvalCase{
		Category:              c.Category,
		TaskHint:              c.TaskHint,
		Question:              c.Question,
		ExpectedChunkKeywords: c.ExpectedChunkKeywords,
		ExpectedAnswerPoints:  c.ExpectedAnswerPoints,
	}
}
