package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	rageval "vid-lens/internal/eval"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/vector"
)

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
	if err := validateEvalConfig(cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, opts.timeout)
	defer cancel()
	connection, err := openEvalDatabase(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer connection.Close()
	repos := repository.NewRepositories(connection.GORM)
	store, err := newConfiguredVectorStore(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect vector backend %q: %w", vector.NormalizeBackendName(cfg.RAG.Store), err)
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
	if err := validateEvalConfig(cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, opts.timeout)
	defer cancel()
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
		return err
	}
	profiles := service.NewAIProfileService(repos.AIProfile, codec, nil)
	store, err := newConfiguredVectorStore(ctx, cfg)
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
			Milvus: rageval.MilvusMetadata{Collection: cfg.Milvus.Collection, IndexType: "collection-configured", MetricType: "COSINE"},
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
