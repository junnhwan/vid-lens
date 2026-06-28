package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/pkg/secret"
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

type evalOptions struct {
	configPath  string
	casesPath   string
	outputPath  string
	environment string
	commit      string
	topK        int
	candidateK  int
	timeout     time.Duration
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
	report service.RAGEvalReport
}

func main() {
	opts := parseFlags()
	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "rag eval failed: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() evalOptions {
	timeout := flag.Duration("timeout", 5*time.Minute, "overall evaluation timeout")
	opts := evalOptions{}
	flag.StringVar(&opts.configPath, "config", "config.yaml", "config file path")
	flag.StringVar(&opts.casesPath, "cases", "docs/eval/rag-quant-cases.yaml", "RAG eval cases YAML path")
	flag.StringVar(&opts.outputPath, "output", "docs/eval/resume-quant-results.md", "Markdown output path")
	flag.StringVar(&opts.environment, "environment", "local", "evaluation environment label")
	flag.StringVar(&opts.commit, "commit", "unknown", "code commit label")
	flag.IntVar(&opts.topK, "top-k", 0, "retrieval topK; default uses config or 5")
	flag.IntVar(&opts.candidateK, "candidate-k", 0, "hybrid candidateK; default uses config or topK")
	flag.Parse()
	opts.timeout = *timeout
	return opts
}

func run(parent context.Context, opts evalOptions) error {
	cases, err := loadCases(opts.casesPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	if !cfg.RAG.Enabled {
		return fmt.Errorf("RAG is disabled in config")
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

	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect mysql: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("unwrap mysql db: %w", err)
	}
	defer sqlDB.Close()

	repos := repository.NewRepositories(db)
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

	store, err := vector.NewMilvusStore(ctx, vector.MilvusConfig{
		Address:    cfg.Milvus.Address,
		Username:   cfg.Milvus.Username,
		Password:   cfg.Milvus.Password,
		Token:      cfg.Milvus.Token,
		Database:   cfg.Milvus.Database,
		Collection: cfg.RAG.Collection,
		Dim:        cfg.RAG.EmbeddingDim,
	})
	if err != nil {
		return fmt.Errorf("connect milvus: %w", err)
	}
	defer store.Close()

	caseContexts, embeddingModel, taskIDs, err := prepareCases(ctx, cases, repos, profiles, factory, cfg.RAG.RerankEndpoint, cfg.RAG.RerankModel)
	if err != nil {
		return err
	}

	vectorReport, err := evaluateVectorOnly(ctx, caseContexts, store, topK)
	if err != nil {
		return err
	}
	hybridReport, err := evaluateHybrid(ctx, caseContexts, store, repos, topK, candidateK)
	if err != nil {
		return err
	}
	rewriteReport, err := evaluateRewritePipeline(ctx, caseContexts, store, repos, factory, topK, candidateK, false)
	if err != nil {
		return err
	}
	fullReport, err := evaluateRewritePipeline(ctx, caseContexts, store, repos, factory, topK, candidateK, true)
	if err != nil {
		return err
	}
	modelRerankReport, err := evaluateModelRerankPipeline(ctx, caseContexts, store, repos, factory, topK, candidateK)
	if err != nil {
		return err
	}

	markdown := renderMarkdown(opts, taskIDs, len(cases), embeddingModel, topK, candidateK, []modeResult{
		{mode: "Vector only", report: vectorReport},
		{mode: "Vector + BM25 + RRF", report: hybridReport},
		{mode: "Rewrite + MultiQuery + RRF", report: rewriteReport},
		{mode: "Rewrite + MultiQuery + RRF + Window + Rerank", report: fullReport},
		{mode: "Rewrite + MultiQuery + RRF + Window + Model Rerank", report: modelRerankReport},
	})
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

func prepareCases(ctx context.Context, cases []evalCase, repos *repository.Repositories, profiles *service.AIProfileService, factory *ai.Factory, rerankEndpoint, rerankModel string) ([]caseEvalContext, string, []int64, error) {
	profileCache := make(map[int64]profileBundle)
	taskIDSet := make(map[int64]bool)
	taskIDs := make([]int64, 0)
	var embeddingModel string
	prepared := make([]caseEvalContext, 0, len(cases))

	for _, c := range cases {
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
		vector, err := bundle.embedding.Embed(ctx, c.Question)
		if err != nil {
			return nil, "", nil, fmt.Errorf("embed question for task %d: %w", c.TaskID, err)
		}
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

func evaluateVectorOnly(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, topK int) (service.RAGEvalReport, error) {
	results := make([]service.RAGEvalCaseResult, 0, len(cases))
	for _, c := range cases {
		startedAt := time.Now()
		chunks, err := store.Search(ctx, c.vector, service.RetrievalRequest{
			UserID:         c.userID,
			TaskID:         c.evalCase.TaskID,
			EmbeddingModel: c.profile.EmbeddingModel,
			TopK:           topK,
		})
		duration := time.Since(startedAt)
		if err != nil {
			return service.RAGEvalReport{}, fmt.Errorf("vector search task %d: %w", c.evalCase.TaskID, err)
		}
		for i := range chunks {
			chunks[i].Source = service.RetrievalSourceVector
			chunks[i].VectorRank = i + 1
		}
		results = append(results, service.RAGEvalCaseResult{
			Case:      c.evalCase.serviceCase(),
			Citations: chunks,
			Duration:  duration,
		})
	}
	return service.EvaluateRAGRetrieval(results, topK), nil
}

func evaluateHybrid(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, topK, candidateK int) (service.RAGEvalReport, error) {
	results := make([]service.RAGEvalCaseResult, 0, len(cases))
	for _, c := range cases {
		startedAt := time.Now()
		vectorChunks, err := store.Search(ctx, c.vector, service.RetrievalRequest{
			UserID:         c.userID,
			TaskID:         c.evalCase.TaskID,
			EmbeddingModel: c.profile.EmbeddingModel,
			TopK:           candidateK,
		})
		if err != nil {
			return service.RAGEvalReport{}, fmt.Errorf("hybrid vector search task %d: %w", c.evalCase.TaskID, err)
		}
		terms := service.ExtractQueryTerms(c.evalCase.Question)
		keywordResults, err := repos.VideoChunk.SearchByBM25(c.userID, c.evalCase.TaskID, c.profile.EmbeddingModel, terms, candidateK)
		if err != nil {
			return service.RAGEvalReport{}, fmt.Errorf("BM25 search task %d: %w", c.evalCase.TaskID, err)
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
		results = append(results, service.RAGEvalCaseResult{
			Case:      c.evalCase.serviceCase(),
			Citations: chunks,
			Duration:  duration,
		})
	}
	return service.EvaluateRAGRetrieval(results, topK), nil
}

func evaluateRewritePipeline(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int, full bool) (service.RAGEvalReport, error) {
	results := make([]service.RAGEvalCaseResult, 0, len(cases))
	for _, c := range cases {
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
			return service.RAGEvalReport{}, fmt.Errorf("pipeline eval task %d: %w", c.evalCase.TaskID, err)
		}
		results = append(results, service.RAGEvalCaseResult{
			Case:                 c.evalCase.serviceCase(),
			Citations:            result.Citations,
			Duration:             duration,
			RewriteFallback:      result.Rewrite.Fallback,
			ExpandedContextChars: citationContentChars(result.Citations),
			RerankChangedRank:    rerankChangedRank(result.Citations),
		})
	}
	return service.EvaluateRAGRetrieval(results, topK), nil
}

func evaluateModelRerankPipeline(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int) (service.RAGEvalReport, error) {
	results := make([]service.RAGEvalCaseResult, 0, len(cases))
	for _, c := range cases {
		expander := service.NewContextExpander(repos, 1, 4000)
		rerankClient, err := factory.NewRerankClient(*c.profile)
		var reranker service.Reranker
		if err == nil {
			reranker = service.NewModelReranker(rerankClient)
		} else {
			reranker = service.NewModelReranker(nil)
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
			return service.RAGEvalReport{}, fmt.Errorf("model rerank pipeline eval task %d: %w", c.evalCase.TaskID, err)
		}
		results = append(results, service.RAGEvalCaseResult{
			Case:                 c.evalCase.serviceCase(),
			Citations:            result.Citations,
			Duration:             duration,
			RewriteFallback:      result.Rewrite.Fallback,
			ExpandedContextChars: citationContentChars(result.Citations),
			RerankChangedRank:    rerankChangedRank(result.Citations),
		})
	}
	return service.EvaluateRAGRetrieval(results, topK), nil
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

func citationContentChars(citations []service.RetrievedChunk) int {
	var total int
	for _, citation := range citations {
		total += len([]rune(citation.Content))
	}
	return total
}

func rerankChangedRank(citations []service.RetrievedChunk) bool {
	for _, citation := range citations {
		if citation.FinalRank > 0 && citation.CrossQueryRank > 0 && citation.FinalRank != citation.CrossQueryRank {
			return true
		}
	}
	return false
}

func (c evalCase) serviceCase() service.RAGEvalCase {
	return service.RAGEvalCase{
		Category:              c.Category,
		TaskHint:              c.TaskHint,
		Question:              c.Question,
		ExpectedChunkKeywords: c.ExpectedChunkKeywords,
		ExpectedAnswerPoints:  c.ExpectedAnswerPoints,
	}
}

func renderMarkdown(opts evalOptions, taskIDs []int64, caseCount int, embeddingModel string, topK, candidateK int, results []modeResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# VidLens Resume Quantification Results\n\n")
	fmt.Fprintf(&b, "## RAG Retrieval A/B Evaluation\n\n")
	fmt.Fprintf(&b, "- Date: %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "- Environment: %s\n", opts.environment)
	fmt.Fprintf(&b, "- Code commit: %s\n", opts.commit)
	fmt.Fprintf(&b, "- Task IDs: %s\n", formatInt64s(taskIDs))
	fmt.Fprintf(&b, "- Case count: %d\n", caseCount)
	fmt.Fprintf(&b, "- Embedding model: %s\n", embeddingModel)
	fmt.Fprintf(&b, "- TopK: %d\n", topK)
	fmt.Fprintf(&b, "- CandidateK: %d\n", candidateK)
	fmt.Fprintf(&b, "- Latency note: retrieval latency excludes the shared query embedding API call.\n\n")
	fmt.Fprintf(&b, "| Mode | Recall@%d | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Rerank Changed Rank Count | Citation Context Hit Rate | Expanded Context Hit Rate |\n", topK)
	fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range results {
		fmt.Fprintf(&b, "| %s | %s | %.3f | %s | %.2f ms | %s | %.1f chars | %d | %s | %s |\n",
			result.mode,
			formatPercent(result.report.RecallAtK),
			result.report.MRR,
			formatPercent(result.report.NoResultRate),
			result.report.AvgLatencyMs,
			formatPercent(result.report.RewriteFallbackRate),
			result.report.AvgExpandedContextChars,
			result.report.RerankChangedRankCount,
			formatPercent(result.report.CitationContextHitRate),
			formatPercent(result.report.ExpandedContextHitRate),
		)
	}
	fmt.Fprintf(&b, "\n")
	renderCategoryMetrics(&b, topK, results)
	fmt.Fprintf(&b, "\n")
	if len(results) >= 2 {
		base := results[0].report
		hybrid, hasHybrid := findModeResult(results, "Vector + BM25 + RRF")
		modelRerank, hasModelRerank := findModeResult(results, "Rewrite + MultiQuery + RRF + Window + Model Rerank")
		fmt.Fprintf(&b, "Conclusion:\n")
		if hasHybrid && (hybrid.report.RecallAtK > base.RecallAtK || hybrid.report.MRR > base.MRR) && hybrid.report.NoResultRate <= base.NoResultRate {
			fmt.Fprintf(&b, "BM25+RRF %s and %s. This supports a cautious claim about hybrid retrieval improving retrieval ranking on this self-built case set, not a broad claim about answer accuracy or production RAG quality.\n",
				recallComparisonText(topK, base.RecallAtK, hybrid.report.RecallAtK),
				mrrComparisonText(base.MRR, hybrid.report.MRR))
			if hasModelRerank {
				if modelRerank.report.RecallAtK > base.RecallAtK || modelRerank.report.MRR > base.MRR {
					fmt.Fprintf(&b, "Model Rerank changed ranking in this run; only claim it if the category metrics justify the specific scenario.\n\n")
				} else {
					fmt.Fprintf(&b, "Model Rerank did not improve ranking in this run; 不要写 model rerank 提升检索排名的简历 claim，建议默认关闭或仅作为后续可选优化继续评估。\n\n")
				}
			} else {
				fmt.Fprintf(&b, "\n")
			}
			fmt.Fprintf(&b, "Resume sentence:\n")
			fmt.Fprintf(&b, "设计并实现 VidLens 视频 RAG 检索评测框架，支持 vector-only、BM25+RRF、query rewrite、多查询召回、相邻片段回填和 rerank 多模式对比；在自建 %d 条视频 QA case 上，BM25+RRF %s，%s，但 model rerank 本轮未证明排序收益，因此不作为简历提升 claim。\n\n",
				caseCount,
				recallResumeText(topK, base.RecallAtK, hybrid.report.RecallAtK),
				mrrResumeText(base.MRR, hybrid.report.MRR))
		} else {
			fmt.Fprintf(&b, "On this small self-built video QA evaluation set, the RAG 2.0 modes did not produce a safer aggregate improvement over vector-only retrieval. Do not write a resume claim about retrieval improvement from this run.\n\n")
			fmt.Fprintf(&b, "Resume sentence:\n")
			fmt.Fprintf(&b, "设计并实现 VidLens 视频 RAG 检索评测框架，支持 vector-only、BM25+RRF、query rewrite、多查询召回、相邻片段回填和 rerank 多模式对比；通过自建 %d 条视频 QA case 记录 Recall@%d、MRR、无结果率和检索延迟，为后续优化提供可量化依据。\n\n", caseCount, topK)
		}
	}
	for _, result := range results {
		fmt.Fprintf(&b, "### %s Case Details\n\n", result.mode)
		fmt.Fprintf(&b, "| # | Hit | First Hit Rank | Result Count | Latency |\n")
		fmt.Fprintf(&b, "| ---: | --- | ---: | ---: | ---: |\n")
		for i, c := range result.report.Cases {
			rank := "-"
			if c.FirstHitRank > 0 {
				rank = fmt.Sprintf("%d", c.FirstHitRank)
			}
			fmt.Fprintf(&b, "| %d | %t | %s | %d | %.2f ms |\n", i+1, c.Hit, rank, c.ResultCount, c.LatencyMs)
		}
		fmt.Fprintf(&b, "\nSource counts: %s\n\n", formatSourceCounts(result.report.SourceCounts))
	}
	return b.String()
}

func findModeResult(results []modeResult, mode string) (modeResult, bool) {
	for _, result := range results {
		if result.mode == mode {
			return result, true
		}
	}
	return modeResult{}, false
}

func renderCategoryMetrics(b *strings.Builder, topK int, results []modeResult) {
	categories := make([]string, 0)
	seen := make(map[string]bool)
	for _, result := range results {
		for category := range result.report.Categories {
			if !seen[category] {
				seen[category] = true
				categories = append(categories, category)
			}
		}
	}
	if len(categories) == 0 {
		return
	}
	sort.Strings(categories)
	fmt.Fprintf(b, "### Per-Category Metrics\n\n")
	fmt.Fprintf(b, "| Mode | Category | Cases | Recall@%d | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Citation Context Hit Rate | Expanded Context Hit Rate | Rerank Changed Rank Count |\n", topK)
	fmt.Fprintf(b, "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range results {
		for _, category := range categories {
			categoryReport, ok := result.report.Categories[category]
			if !ok {
				continue
			}
			fmt.Fprintf(b, "| %s | %s | %d | %s | %.3f | %s | %.2f ms | %s | %.1f chars | %s | %s | %d |\n",
				result.mode,
				category,
				categoryReport.TotalCases,
				formatPercent(categoryReport.RecallAtK),
				categoryReport.MRR,
				formatPercent(categoryReport.NoResultRate),
				categoryReport.AvgLatencyMs,
				formatPercent(categoryReport.RewriteFallbackRate),
				categoryReport.AvgExpandedContextChars,
				formatPercent(categoryReport.CitationContextHitRate),
				formatPercent(categoryReport.ExpandedContextHitRate),
				categoryReport.RerankChangedRankCount,
			)
		}
	}
}

func recallComparisonText(topK int, base, opt float64) string {
	if opt > base {
		return fmt.Sprintf("improved Recall@%d from %s to %s", topK, formatPercent(base), formatPercent(opt))
	}
	if opt == base {
		return fmt.Sprintf("kept Recall@%d at %s", topK, formatPercent(opt))
	}
	return fmt.Sprintf("changed Recall@%d from %s to %s", topK, formatPercent(base), formatPercent(opt))
}

func mrrComparisonText(base, opt float64) string {
	if opt > base {
		return fmt.Sprintf("improved MRR from %.3f to %.3f", base, opt)
	}
	if opt == base {
		return fmt.Sprintf("kept MRR at %.3f", opt)
	}
	return fmt.Sprintf("changed MRR from %.3f to %.3f", base, opt)
}

func recallResumeText(topK int, base, opt float64) string {
	if opt > base {
		return fmt.Sprintf("将 Recall@%d 从 %s 提升至 %s", topK, formatPercent(base), formatPercent(opt))
	}
	if opt == base {
		return fmt.Sprintf("Recall@%d 均为 %s", topK, formatPercent(opt))
	}
	return fmt.Sprintf("Recall@%d 从 %s 变为 %s", topK, formatPercent(base), formatPercent(opt))
}

func mrrResumeText(base, opt float64) string {
	if opt > base {
		return fmt.Sprintf("MRR 从 %.3f 提升至 %.3f", base, opt)
	}
	if opt == base {
		return fmt.Sprintf("MRR 均为 %.3f", opt)
	}
	return fmt.Sprintf("MRR 从 %.3f 变为 %.3f", base, opt)
}

func formatPercent(v float64) string {
	return fmt.Sprintf("%.1f%%", v*100)
}

func formatInt64s(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ", ")
}

func formatSourceCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func parentDir(path string) string {
	idx := strings.LastIndexAny(path, `/\`)
	if idx < 0 {
		return "."
	}
	return path[:idx]
}
