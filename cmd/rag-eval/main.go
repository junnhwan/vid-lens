package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	rageval "vid-lens/internal/eval"
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
	configPath   string
	manifestPath string
	casesPath    string
	outputPath   string
	environment  string
	commit       string
	topK         int
	candidateK   int
	timeout      time.Duration
	progress     bool

	strict                      bool
	snapshotOnly                bool
	validateOnly                bool
	datasetVersion              string
	split                       rageval.Split
	sealedTestToken             string
	outputDir                   string
	sealedAccessRegistry        string
	experimentRegistry          string
	experimentID                string
	variantID                   string
	corpusSHA256                string
	chunkManifestSHA256         string
	vectorArtifactSHA256        string
	configSHA256                string
	retrievalConfigPath         string
	baselineRetrievalConfigPath string
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

type answerModeResult struct {
	mode   string
	report service.VideoAgentAnswerEvalReport
}

func main() {
	opts, err := parseEvalFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rag eval flags: %v\n", err)
		os.Exit(2)
	}
	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "rag eval failed: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() evalOptions {
	opts, err := parseEvalFlags(os.Args[1:])
	if err != nil {
		panic(err)
	}
	return opts
}

func parseEvalFlags(args []string) (evalOptions, error) {
	flags := flag.NewFlagSet("rag-eval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	opts := evalOptions{}
	flags.StringVar(&opts.configPath, "config", "config.yaml", "config file path")
	flags.StringVar(&opts.manifestPath, "manifest", "", "strict dataset manifest YAML path; --cases points to the selected physical split")
	flags.StringVar(&opts.casesPath, "cases", "docs/eval/rag-quant-cases.yaml", "RAG eval cases YAML path")
	flags.StringVar(&opts.outputPath, "output", "docs/eval/resume-quant-results.md", "legacy Markdown output path")
	flags.StringVar(&opts.environment, "environment", "local", "evaluation environment label")
	flags.StringVar(&opts.commit, "commit", "unknown", "code commit label")
	flags.IntVar(&opts.topK, "top-k", 0, "retrieval topK; default uses config or 5")
	flags.IntVar(&opts.candidateK, "candidate-k", 0, "hybrid candidateK; default uses config or topK")
	flags.DurationVar(&opts.timeout, "timeout", 5*time.Minute, "overall evaluation timeout")
	flags.BoolVar(&opts.progress, "progress", false, "write evaluation progress to stderr")

	var split string
	flags.BoolVar(&opts.strict, "strict", false, "enable source-group strict evaluation mode")
	flags.BoolVar(&opts.snapshotOnly, "snapshot-only", false, "freeze live corpus/chunk/vector inputs without running an experiment")
	flags.BoolVar(&opts.validateOnly, "validate-only", false, "validate strict dataset and registry without executing retrieval")
	flags.StringVar(&opts.datasetVersion, "dataset-version", "", "required strict dataset version")
	flags.StringVar(&split, "split", string(rageval.SplitDev), "strict split: train, dev, or test")
	flags.StringVar(&opts.sealedTestToken, "sealed-test-token", "", "token required to authorize sealed test execution")
	flags.StringVar(&opts.outputDir, "output-dir", "artifacts/rag-eval", "strict artifact output root")
	flags.StringVar(&opts.sealedAccessRegistry, "sealed-access-registry", "docs/eval/sealed-access-registry.jsonl", "append-only sealed test access registry")
	flags.StringVar(&opts.experimentRegistry, "experiment-registry", "docs/eval/experiment-registry.yaml", "preregistered experiment registry")
	flags.StringVar(&opts.experimentID, "experiment-id", "", "preregistered experiment ID")
	flags.StringVar(&opts.variantID, "variant-id", "", "preregistered candidate variant ID")
	flags.StringVar(&opts.corpusSHA256, "corpus-hash", "", "corpus SHA-256")
	flags.StringVar(&opts.chunkManifestSHA256, "chunk-manifest-hash", "", "chunk manifest SHA-256")
	flags.StringVar(&opts.vectorArtifactSHA256, "vector-artifact-hash", "", "vector artifact SHA-256")
	flags.StringVar(&opts.configSHA256, "config-hash", "", "retrieval config SHA-256")
	flags.StringVar(&opts.retrievalConfigPath, "retrieval-config", "", "candidate retrieval config YAML")
	flags.StringVar(&opts.baselineRetrievalConfigPath, "baseline-retrieval-config", "", "baseline retrieval config YAML")

	if err := flags.Parse(args); err != nil {
		return evalOptions{}, err
	}
	opts.split = rageval.Split(strings.ToLower(strings.TrimSpace(split)))
	if opts.split != rageval.SplitTrain && opts.split != rageval.SplitDev && opts.split != rageval.SplitTest {
		return evalOptions{}, fmt.Errorf("invalid split %q: want train, dev, or test", split)
	}
	if opts.snapshotOnly && !opts.strict {
		return evalOptions{}, fmt.Errorf("--snapshot-only requires --strict")
	}
	if opts.snapshotOnly && strings.TrimSpace(opts.retrievalConfigPath) == "" {
		return evalOptions{}, fmt.Errorf("--retrieval-config is required with --snapshot-only")
	}
	if opts.strict && strings.TrimSpace(opts.datasetVersion) == "" {
		return evalOptions{}, fmt.Errorf("--dataset-version is required in strict mode")
	}
	if opts.strict && opts.split == rageval.SplitTest && !opts.validateOnly && strings.TrimSpace(opts.sealedTestToken) == "" {
		return evalOptions{}, fmt.Errorf("--sealed-test-token is required for strict test execution")
	}
	return opts, nil
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

	progress.stage("connecting mysql and milvus")
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

	caseContexts, embeddingModel, taskIDs, err := prepareCases(ctx, cases, repos, profiles, factory, cfg.RAG.RerankEndpoint, cfg.RAG.RerankModel, progress)
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
	progress.stage("evaluating retrieval mode: Rewrite + MultiQuery + RRF + Window + Model Rerank")
	modelRerankReport, err := evaluateModelRerankPipeline(ctx, caseContexts, store, repos, factory, topK, candidateK, progress)
	if err != nil {
		return err
	}

	retrievalResults := []modeResult{
		{mode: "Vector only", report: vectorReport},
		{mode: "Vector + BM25 + RRF", report: hybridReport},
		{mode: "Rewrite + MultiQuery + RRF", report: rewriteReport},
		{mode: "Rewrite + MultiQuery + RRF + Window + Rerank", report: fullReport},
		{mode: "Rewrite + MultiQuery + RRF + Window + Model Rerank", report: modelRerankReport},
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

type evalProgress struct {
	enabled bool
	out     io.Writer
}

func runStrict(parent context.Context, opts evalOptions) error {
	dataset, err := loadStrictDataset(opts)
	if err != nil {
		return err
	}
	if opts.split == rageval.SplitTest {
		if !opts.validateOnly {
			if err := rageval.AuthorizeSealedTest(dataset, opts.sealedTestToken); err != nil {
				return err
			}
		}
	} else if err := rageval.GuardTuningAllowed(opts.sealedAccessRegistry, dataset.DatasetVersion); err != nil {
		return err
	}

	if opts.snapshotOnly {
		snapshotCfg, err := loadSnapshotRetrievalConfig(opts.retrievalConfigPath)
		if err != nil {
			return err
		}
		return executeStrictSnapshot(parent, opts, dataset, snapshotCfg)
	}

	registryRaw, err := os.ReadFile(opts.experimentRegistry)
	if err != nil {
		return fmt.Errorf("read experiment registry: %w", err)
	}
	registry, err := rageval.LoadExperimentRegistry(registryRaw)
	if err != nil {
		return err
	}
	experiment, candidate, err := registry.BindRun(opts.experimentID, opts.variantID, dataset.DatasetVersion, opts.commit, opts.configSHA256)
	if err != nil {
		return err
	}
	if len(experiment.BaselineConfigSHA256) != 64 {
		return fmt.Errorf("experiment %q must freeze baseline_config_sha256", opts.experimentID)
	}
	if err := experiment.FrozenEvidence.Validate(); err != nil {
		return fmt.Errorf("experiment %q: %w", opts.experimentID, err)
	}
	for name, values := range map[string][2]string{
		"corpus hash":          {opts.corpusSHA256, experiment.FrozenEvidence.CorpusSHA256},
		"chunk manifest hash":  {opts.chunkManifestSHA256, experiment.FrozenEvidence.ChunkManifestSHA256},
		"vector artifact hash": {opts.vectorArtifactSHA256, experiment.FrozenEvidence.VectorArtifactSHA256},
	} {
		if len(values[0]) != 64 {
			return fmt.Errorf("%s must be a 64-character SHA-256 hex digest", name)
		}
		if !strings.EqualFold(values[0], values[1]) {
			return fmt.Errorf("%s does not match preregistered value", name)
		}
	}
	if opts.validateOnly {
		return nil
	}
	if strings.TrimSpace(opts.retrievalConfigPath) == "" || strings.TrimSpace(opts.baselineRetrievalConfigPath) == "" {
		return fmt.Errorf("--retrieval-config and --baseline-retrieval-config are required for strict execution")
	}
	if opts.split == rageval.SplitTest {
		return fmt.Errorf("sealed test execution is intentionally disabled in this dev command; use a separately audited final-run workflow")
	}
	baselineCfg, retrievalCfg, factor, err := loadRetrievalConfigs(
		opts.baselineRetrievalConfigPath, opts.retrievalConfigPath,
		experiment.BaselineConfigSHA256, candidate.ConfigSHA256,
	)
	if err != nil {
		return err
	}
	return executeStrictRetrieval(parent, opts, dataset, registry, baselineCfg, retrievalCfg, factor)
}

func loadStrictDataset(opts evalOptions) (rageval.Dataset, error) {
	splitRaw, err := os.ReadFile(opts.casesPath)
	if err != nil {
		return rageval.Dataset{}, fmt.Errorf("read strict dataset cases: %w", err)
	}
	if strings.TrimSpace(opts.manifestPath) == "" {
		if opts.split == rageval.SplitTest {
			return rageval.Dataset{}, fmt.Errorf("--manifest is required for sealed test; combined strict datasets cannot execute test")
		}
		return rageval.LoadDataset(splitRaw, rageval.LoadOptions{
			Mode: rageval.LoadModeStrict, DatasetVersion: opts.datasetVersion,
		})
	}

	manifestRaw, err := os.ReadFile(opts.manifestPath)
	if err != nil {
		return rageval.Dataset{}, fmt.Errorf("read strict dataset manifest: %w", err)
	}
	loadOpts := rageval.SplitLoadOptions{
		ExpectedVersion: opts.datasetVersion,
		Split:           opts.split,
	}
	if opts.split == rageval.SplitTest {
		loadOpts.SealedToken = opts.sealedTestToken
		loadOpts.AccessRegistryPath = opts.sealedAccessRegistry
		loadOpts.AccessEvent = rageval.SealedAccessEvent{
			OccurredAt:   time.Now().UTC(),
			ExperimentID: opts.experimentID,
			RunID:        fmt.Sprintf("%s-%s-%d", opts.datasetVersion, opts.split, time.Now().UnixNano()),
			Commit:       opts.commit,
		}
	}
	return rageval.LoadSplitDataset(manifestRaw, splitRaw, loadOpts)
}

func newEvalProgress(enabled bool, out io.Writer) evalProgress {
	if out == nil {
		out = io.Discard
	}
	return evalProgress{enabled: enabled, out: out}
}

func (p evalProgress) stage(format string, args ...any) {
	if !p.enabled {
		return
	}
	fmt.Fprintf(p.out, "[rag-eval] %s\n", fmt.Sprintf(format, args...))
}

func (p evalProgress) caseStep(stage string, idx, total int, c evalCase) {
	if !p.enabled {
		return
	}
	fmt.Fprintf(p.out, "[rag-eval] %s case %d/%d task=%d question=%q\n", stage, idx, total, c.TaskID, truncateProgressQuestion(c.Question))
}

func truncateProgressQuestion(question string) string {
	question = strings.TrimSpace(question)
	runes := []rune(question)
	if len(runes) <= 80 {
		return question
	}
	return string(runes[:80]) + "..."
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

func evaluateVectorOnly(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, topK int, progress evalProgress) (service.RAGEvalReport, error) {
	results := make([]service.RAGEvalCaseResult, 0, len(cases))
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

func evaluateHybrid(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, topK, candidateK int, progress evalProgress) (service.RAGEvalReport, error) {
	results := make([]service.RAGEvalCaseResult, 0, len(cases))
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

func evaluateRewritePipeline(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int, full bool, progress evalProgress) (service.RAGEvalReport, error) {
	results := make([]service.RAGEvalCaseResult, 0, len(cases))
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

func evaluateModelRerankPipeline(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int, progress evalProgress) (service.RAGEvalReport, error) {
	results := make([]service.RAGEvalCaseResult, 0, len(cases))
	for i, c := range cases {
		progress.caseStep("model rerank retrieval", i+1, len(cases), c.evalCase)
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

func evaluateAnswerModes(ctx context.Context, cases []caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int, progress evalProgress) []answerModeResult {
	ordinaryResults := make([]service.VideoAgentAnswerEvalCaseResult, 0, len(cases))
	agentResults := make([]service.VideoAgentAnswerEvalCaseResult, 0, len(cases))
	for i, c := range cases {
		progress.caseStep("ordinary answer", i+1, len(cases), c.evalCase)
		ordinaryResults = append(ordinaryResults, evaluateOrdinaryAnswer(ctx, c, store, repos, factory, topK, candidateK))
		progress.caseStep("agentic answer", i+1, len(cases), c.evalCase)
		agentResults = append(agentResults, evaluateAgenticAnswer(ctx, c, store, repos, factory, topK, candidateK))
	}
	return []answerModeResult{
		{mode: "Ordinary RAG answer", report: service.EvaluateVideoAgentAnswers(ordinaryResults)},
		{mode: "Agentic answer", report: service.EvaluateVideoAgentAnswers(agentResults)},
	}
}

func evaluateOrdinaryAnswer(ctx context.Context, c caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int) (result service.VideoAgentAnswerEvalCaseResult) {
	result = service.VideoAgentAnswerEvalCaseResult{Case: c.evalCase.serviceCase()}
	startedAt := time.Now()
	defer func() {
		result.Duration = time.Since(startedAt)
	}()
	chat, err := newAnswerEvalChatClient(factory, *c.profile)
	if err != nil {
		result = answerEvalErrorResult(result, err)
		return
	}
	pipeline := newAnswerEvalPipeline(c, store, repos, candidateK)
	retrieval, err := pipeline.Retrieve(ctx, service.RetrievalPipelineRequest{
		UserID:         c.userID,
		TaskID:         c.evalCase.TaskID,
		Question:       c.evalCase.Question,
		TopK:           topK,
		EmbeddingModel: c.profile.EmbeddingModel,
		Embedding:      c.embedding,
	})
	if err != nil {
		result = answerEvalErrorResult(result, err)
		return
	}
	result.Citations = retrieval.Citations
	if len(retrieval.Citations) == 0 {
		result = answerEvalErrorResult(result, fmt.Errorf("no retrieved citations"))
		return
	}
	answer, err := chat.Chat(ctx, service.BuildRAGAnswerMessages(retrieval.Citations, c.evalCase.Question))
	if err != nil {
		result = answerEvalErrorResult(result, err)
		return
	}
	result.Answer = answer
	return
}

func evaluateAgenticAnswer(ctx context.Context, c caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, factory *ai.Factory, topK, candidateK int) (result service.VideoAgentAnswerEvalCaseResult) {
	result = service.VideoAgentAnswerEvalCaseResult{Case: c.evalCase.serviceCase()}
	startedAt := time.Now()
	defer func() {
		result.Duration = time.Since(startedAt)
	}()
	chat, err := newAnswerEvalChatClient(factory, *c.profile)
	if err != nil {
		result = answerEvalErrorResult(result, err)
		return
	}
	pipeline := newAnswerEvalPipeline(c, store, repos, candidateK)
	tools := service.NewVideoAgentTools(repos, pipeline, chat)
	search, step, err := tools.SearchTranscript(ctx, service.SearchTranscriptInput{
		UserID:         c.userID,
		TaskID:         c.evalCase.TaskID,
		Question:       c.evalCase.Question,
		TopK:           topK,
		EmbeddingModel: c.profile.EmbeddingModel,
		Embedding:      c.embedding,
	})
	result.Trace = append(result.Trace, step)
	if err != nil {
		result = answerEvalErrorResult(result, err)
		return
	}
	if len(search.Citations) == 0 {
		result = answerEvalErrorResult(result, fmt.Errorf("no retrieved citations"))
		return
	}
	template := service.ClassifyVideoAgentTemplate(c.evalCase.Question)
	answer, citations, trace, err := service.ExecuteVideoAgentTemplate(ctx, tools, template, service.VideoAgentTemplateRequest{
		UserID:         c.userID,
		TaskID:         c.evalCase.TaskID,
		Question:       c.evalCase.Question,
		EmbeddingModel: c.profile.EmbeddingModel,
	}, search.Citations, result.Trace)
	result.Trace = trace
	result.Citations = citations
	if err != nil {
		result = answerEvalErrorResult(result, err)
		return
	}
	result.Answer = answer
	return
}

func newAnswerEvalPipeline(c caseEvalContext, store service.RAGRetriever, repos *repository.Repositories, candidateK int) *service.RetrievalPipeline {
	return service.NewRetrievalPipeline(
		repos,
		store,
		cachedEvalRewriter{result: c.rewrite, err: c.rewriteErr},
		service.NewContextExpander(repos, 1, 4000),
		service.DeterministicReranker{},
		candidateK,
		0,
	)
}

func newAnswerEvalChatClient(factory *ai.Factory, profile ai.Profile) (ai.ChatClient, error) {
	if strings.TrimSpace(profile.LLMProvider) == "" ||
		strings.TrimSpace(profile.LLMBaseURL) == "" ||
		strings.TrimSpace(profile.LLMAPIKey) == "" ||
		strings.TrimSpace(profile.LLMModel) == "" {
		return nil, fmt.Errorf("LLM answer profile is incomplete")
	}
	return factory.NewChatClient(profile)
}

func answerEvalErrorResult(result service.VideoAgentAnswerEvalCaseResult, err error) service.VideoAgentAnswerEvalCaseResult {
	result.FallbackOrError = true
	if err != nil {
		result.Error = err.Error()
	}
	return result
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
	return renderMarkdownWithAgentAnswerEval(opts, taskIDs, caseCount, embeddingModel, topK, candidateK, results, nil)
}

func renderMarkdownWithAgentAnswerEval(opts evalOptions, taskIDs []int64, caseCount int, embeddingModel string, topK, candidateK int, results []modeResult, answerResults []answerModeResult) string {
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
	renderAgentAnswerEvaluation(&b, answerResults)
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

func renderAgentAnswerEvaluation(b *strings.Builder, answerResults []answerModeResult) {
	if len(answerResults) == 0 {
		return
	}
	fmt.Fprintf(b, "## Agent Answer Evaluation\n\n")
	fmt.Fprintf(b, "| Mode | Answer Point Coverage | Citation Hit Rate | No Answer Rate | Avg Tool Steps | Fallback/Error Rate | Avg Latency |\n")
	fmt.Fprintf(b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range answerResults {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %.1f | %s | %.2f ms |\n",
			result.mode,
			formatPercent(result.report.AnswerPointCoverage),
			formatPercent(result.report.CitationHitRate),
			formatPercent(result.report.NoAnswerRate),
			result.report.AvgToolSteps,
			formatPercent(result.report.FallbackErrorRate),
			result.report.AvgLatencyMs,
		)
	}
	fmt.Fprintf(b, "\n")
	ordinary, hasOrdinary := findAnswerModeResult(answerResults, "Ordinary RAG answer")
	agentic, hasAgentic := findAnswerModeResult(answerResults, "Agentic answer")
	if hasOrdinary && hasAgentic {
		if agentic.report.AnswerPointCoverage > ordinary.report.AnswerPointCoverage &&
			agentic.report.FallbackErrorRate <= ordinary.report.FallbackErrorRate {
			fmt.Fprintf(b, "Agentic answer improved deterministic answer-point coverage from %s to %s. Treat this as local eval evidence, not a production benchmark or broad answer-accuracy claim.\n\n",
				formatPercent(ordinary.report.AnswerPointCoverage),
				formatPercent(agentic.report.AnswerPointCoverage))
		} else {
			fmt.Fprintf(b, "Agentic answer eval did not prove a safer answer-point coverage improvement over ordinary RAG in this run. Do not claim answer accuracy improvement from Agentic QA without stronger eval evidence.\n\n")
		}
	}
}

func findAnswerModeResult(results []answerModeResult, mode string) (answerModeResult, bool) {
	for _, result := range results {
		if result.mode == mode {
			return result, true
		}
	}
	return answerModeResult{}, false
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

func loadSnapshotRetrievalConfig(path string) (service.RAGRetrievalConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return service.RAGRetrievalConfig{}, fmt.Errorf("read snapshot retrieval config: %w", err)
	}
	var cfg service.RAGRetrievalConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return service.RAGRetrievalConfig{}, fmt.Errorf("parse snapshot retrieval config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return service.RAGRetrievalConfig{}, err
	}
	if err := cfg.ValidateStrictExperiment(); err != nil {
		return service.RAGRetrievalConfig{}, err
	}
	return cfg, nil
}

func loadRetrievalConfigs(baselinePath, candidatePath, expectedBaselineHash, expectedCandidateHash string) (service.RAGRetrievalConfig, service.RAGRetrievalConfig, string, error) {
	load := func(path string) (service.RAGRetrievalConfig, []byte, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return service.RAGRetrievalConfig{}, nil, err
		}
		var cfg service.RAGRetrievalConfig
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, nil, err
		}
		if err := cfg.Validate(); err != nil {
			return cfg, nil, err
		}
		if err := cfg.ValidateStrictExperiment(); err != nil {
			return cfg, nil, err
		}
		return cfg, raw, nil
	}
	base, baseRaw, err := load(baselinePath)
	if err != nil {
		return service.RAGRetrievalConfig{}, service.RAGRetrievalConfig{}, "", fmt.Errorf("load baseline retrieval config: %w", err)
	}
	candidate, candidateRaw, err := load(candidatePath)
	if err != nil {
		return service.RAGRetrievalConfig{}, service.RAGRetrievalConfig{}, "", fmt.Errorf("load candidate retrieval config: %w", err)
	}
	verifyHash := func(label string, raw []byte, expected string) error {
		sum := sha256.Sum256(raw)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(strings.TrimSpace(expected), actual) {
			return fmt.Errorf("%s retrieval config hash mismatch: got %s", label, actual)
		}
		return nil
	}
	if err := verifyHash("baseline", baseRaw, expectedBaselineHash); err != nil {
		return service.RAGRetrievalConfig{}, service.RAGRetrievalConfig{}, "", err
	}
	if err := verifyHash("candidate", candidateRaw, expectedCandidateHash); err != nil {
		return service.RAGRetrievalConfig{}, service.RAGRetrievalConfig{}, "", err
	}
	factor, err := service.ValidateSingleVariableAblation(base, candidate)
	if err != nil {
		return service.RAGRetrievalConfig{}, service.RAGRetrievalConfig{}, "", err
	}
	return base, candidate, factor, nil
}

type productionEvidenceRetriever struct {
	repos    *repository.Repositories
	profiles *service.AIProfileService
	factory  *ai.Factory
	store    service.RAGRetriever
	config   service.RAGRetrievalConfig
}

func (r *productionEvidenceRetriever) Retrieve(ctx context.Context, c rageval.Case) ([]rageval.ChunkEvidence, error) {
	if c.TaskID <= 0 {
		return nil, fmt.Errorf("case %s has no task_id", c.CaseID)
	}
	task, err := r.repos.Task.FindByID(c.TaskID)
	if err != nil {
		return nil, err
	}
	profile, err := r.profiles.GetDefaultAIProfile(task.UserID)
	if err != nil {
		return nil, err
	}
	var embedding ai.EmbeddingClient
	if r.config.EnableVector {
		embedding, err = r.factory.NewEmbeddingClient(*profile)
		if err != nil {
			return nil, err
		}
	}
	var chat ai.ChatClient
	if r.config.QueryMode == service.QueryModeRewrite {
		chat, err = r.factory.NewChatClient(*profile)
		if err != nil {
			return nil, err
		}
	}
	reranker, err := configuredReranker(r.config.RerankerMode)
	if err != nil {
		return nil, err
	}
	pipeline, err := service.NewConfiguredRetrievalPipeline(r.repos, r.store, chat, reranker, r.config)
	if err != nil {
		return nil, err
	}
	result, err := pipeline.Retrieve(ctx, service.RetrievalPipelineRequest{UserID: task.UserID, TaskID: c.TaskID, Question: c.Question, TopK: r.config.TopK, EmbeddingModel: profile.EmbeddingModel, Embedding: embedding})
	if err != nil {
		return nil, err
	}
	out := make([]rageval.ChunkEvidence, 0, len(result.Citations))
	for _, chunk := range result.Citations {
		out = append(out, chunkEvidenceFromCitation(chunk, c.VideoID))
	}
	return out, nil
}

func configuredReranker(mode string) (service.Reranker, error) {
	switch mode {
	case "", service.RerankerModeNone:
		return nil, nil
	case service.RerankerModeDeterministic:
		return service.DeterministicReranker{}, nil
	default:
		return nil, fmt.Errorf("unsupported strict reranker mode %q", mode)
	}
}

func chunkEvidenceFromCitation(chunk service.RetrievedChunk, videoID string) rageval.ChunkEvidence {
	// Strict evaluation scores the exact expanded context sent to answer
	// generation. EvidenceID remains anchored to the original retrieval unit.
	text := chunk.Content
	if strings.TrimSpace(text) == "" {
		text = chunk.AnchorContent
	}
	return rageval.ChunkEvidence{
		ContextID: chunk.EvidenceID, VideoID: videoID, Text: text,
		Source: rageval.EvidenceSourceASR, TokenCount: len([]rune(text)) / 4,
	}
}

func executeStrictSnapshot(parent context.Context, opts evalOptions, dataset rageval.Dataset, retrievalCfg service.RAGRetrievalConfig) error {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
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
	store, err := vector.NewMilvusStore(ctx, vector.MilvusConfig{Address: cfg.Milvus.Address, Username: cfg.Milvus.Username, Password: cfg.Milvus.Password, Token: cfg.Milvus.Token, Database: cfg.Milvus.Database, Collection: cfg.RAG.Collection, Dim: cfg.RAG.EmbeddingDim})
	if err != nil {
		return fmt.Errorf("connect milvus: %w", err)
	}
	defer store.Close()
	splitDataset, snapshot, err := buildLiveEvidenceSnapshot(ctx, dataset, opts.split, retrievalCfg, liveEvidenceSources{
		tasks: repos.Task, transcriptions: repos.Transcription, chunks: repos.VideoChunk, indexes: repos.RAGIndex, vectors: store,
	})
	if err != nil {
		return err
	}
	frozen, err := rageval.FreezeEvidenceArtifacts(splitDataset, snapshot)
	if err != nil {
		return err
	}
	if err := writeFrozenEvidenceArtifacts(opts.outputDir, dataset.DatasetVersion, opts.split, frozen); err != nil {
		return err
	}
	fmt.Printf("strict snapshot dataset=%s split=%s corpus=%s chunks=%s vectors=%s\n", dataset.DatasetVersion, opts.split, frozen.Corpus.SHA256, frozen.Chunks.SHA256, frozen.Vectors.SHA256)
	return nil
}

func strictRetrievalMetricConfig(topK int) rageval.MetricConfig {
	return rageval.MetricConfig{
		K:                   topK,
		BoundaryToleranceMS: 500,
		MaxChunkDurationMS:  30_000,
		MinEvidenceCoverage: 1,
	}
}
func executeStrictRetrieval(parent context.Context, opts evalOptions, dataset rageval.Dataset, registry rageval.ExperimentRegistry, baselineCfg, candidateCfg service.RAGRetrievalConfig, factor string) error {
	if strings.HasPrefix(factor, "chunk") {
		return fmt.Errorf("strict chunking ablation %q requires separately rebuilt and frozen indexes; the live shared index cannot represent both variants", factor)
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, opts.timeout)
	defer cancel()
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect mysql: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	repos := repository.NewRepositories(db)
	codecSecret := cfg.Security.APIKeySecret
	if codecSecret == "" {
		codecSecret = cfg.JWT.Secret
	}
	codec, err := secret.NewCodecFromPassphrase(codecSecret)
	if err != nil {
		return err
	}
	profiles := service.NewAIProfileService(repos.AIProfile, codec, nil)
	store, err := vector.NewMilvusStore(ctx, vector.MilvusConfig{Address: cfg.Milvus.Address, Username: cfg.Milvus.Username, Password: cfg.Milvus.Password, Token: cfg.Milvus.Token, Database: cfg.Milvus.Database, Collection: cfg.RAG.Collection, Dim: cfg.RAG.EmbeddingDim})
	if err != nil {
		return err
	}
	defer store.Close()

	splitDataset, snapshot, err := buildLiveEvidenceSnapshot(ctx, dataset, opts.split, candidateCfg, liveEvidenceSources{
		tasks: repos.Task, transcriptions: repos.Transcription, chunks: repos.VideoChunk, indexes: repos.RAGIndex, vectors: store,
	})
	if err != nil {
		return err
	}
	frozen, err := rageval.FreezeEvidenceArtifacts(splitDataset, snapshot)
	if err != nil {
		return err
	}
	liveEvidence := rageval.FrozenEvidenceReference{
		CorpusSHA256: frozen.Corpus.SHA256, ChunkManifestSHA256: frozen.Chunks.SHA256,
		VectorArtifactSHA256: frozen.Vectors.SHA256,
	}
	if err := writeFrozenEvidenceArtifacts(opts.outputDir, dataset.DatasetVersion, opts.split, frozen); err != nil {
		return err
	}
	registeredExperiment, err := registry.Experiment(opts.experimentID)
	if err != nil {
		return err
	}
	experiment, candidate, err := registry.BindStrictRun(
		opts.experimentID, opts.variantID, dataset.DatasetVersion, opts.commit,
		registeredExperiment.BaselineConfigSHA256, opts.configSHA256, liveEvidence,
	)
	if err != nil {
		return err
	}
	for label, values := range map[string][2]string{
		"corpus":          {opts.corpusSHA256, frozen.Corpus.SHA256},
		"chunk manifest":  {opts.chunkManifestSHA256, frozen.Chunks.SHA256},
		"vector artifact": {opts.vectorArtifactSHA256, frozen.Vectors.SHA256},
	} {
		if values[0] != "" && !strings.EqualFold(values[0], values[1]) {
			return fmt.Errorf("claimed %s hash does not match live artifact: claimed=%s live=%s", label, values[0], values[1])
		}
	}

	factory := ai.NewFactory()
	promptSum := sha256.Sum256([]byte("retrieval-only-no-answer-generation"))
	newMetadata := func(variantID, configHash string) rageval.RunMetadata {
		return rageval.RunMetadata{
			Commit: opts.commit, CorpusSHA256: frozen.Corpus.SHA256,
			ChunkManifestSHA256: frozen.Chunks.SHA256, VectorArtifactSHA256: frozen.Vectors.SHA256,
			ConfigSHA256: configHash, Environment: opts.environment,
			ExperimentID: opts.experimentID, VariantID: variantID,
			Models: rageval.ModelMetadata{Embedding: rageval.ModelRef{Provider: "user-profile", Name: "resolved-per-task"}},
			Milvus: rageval.MilvusMetadata{Collection: cfg.RAG.Collection, IndexType: "collection-configured", MetricType: "COSINE"},
			Prompt: rageval.PromptMetadata{Name: "retrieval-only", Version: "1", SHA256: hex.EncodeToString(promptSum[:])},
		}
	}
	runVariant := func(retrievalCfg service.RAGRetrievalConfig, metadata rageval.RunMetadata) (rageval.RunArtifact, error) {
		retriever := &productionEvidenceRetriever{repos: repos, profiles: profiles, factory: factory, store: store, config: retrievalCfg}
		return (rageval.Runner{Executor: rageval.ChunkEvidenceExecutor{Retriever: retriever}}).Run(
			ctx, splitDataset, opts.split, metadata,
			strictRetrievalMetricConfig(retrievalCfg.TopK),
		)
	}
	baselineArtifact, err := runVariant(baselineCfg, newMetadata(experiment.BaselineVariant, experiment.BaselineConfigSHA256))
	if err != nil {
		return fmt.Errorf("run frozen baseline: %w", err)
	}
	candidateArtifact, err := runVariant(candidateCfg, newMetadata(candidate.VariantID, candidate.ConfigSHA256))
	if err != nil {
		return fmt.Errorf("run candidate: %w", err)
	}
	analysis, err := rageval.AnalyzePairedRunArtifacts(registry, opts.experimentID, opts.variantID, baselineArtifact, candidateArtifact)
	if err != nil {
		return err
	}
	candidateArtifact.Analysis = &analysis
	baselinePaths, err := rageval.WriteArtifacts(opts.outputDir, baselineArtifact)
	if err != nil {
		return err
	}
	candidatePaths, err := rageval.WriteArtifacts(opts.outputDir, candidateArtifact)
	if err != nil {
		return err
	}
	fmt.Printf("strict dev factor=%s baseline=%s candidate=%s status=%s\n", factor, baselinePaths.Directory, candidatePaths.Directory, analysis.Status)
	return nil
}

func writeFrozenEvidenceArtifacts(outputDir, datasetVersion string, split rageval.Split, frozen rageval.EvidenceArtifactBundle) error {
	dir := filepath.Join(outputDir, "frozen-inputs", datasetVersion, string(split))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create frozen evidence directory: %w", err)
	}
	for name, artifact := range map[string]rageval.FrozenArtifact{
		"corpus": frozen.Corpus, "chunks": frozen.Chunks, "vectors": frozen.Vectors,
	} {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s.json", name, artifact.SHA256))
		if err := os.WriteFile(path, append(append([]byte(nil), artifact.CanonicalJSON...), '\n'), 0o600); err != nil {
			return fmt.Errorf("write frozen %s artifact: %w", name, err)
		}
	}
	return nil
}
