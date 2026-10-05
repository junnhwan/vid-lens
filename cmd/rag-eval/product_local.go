package main

import (
	"context"
	"strings"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/eval"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

// Local product evaluation uses the same conversation/persistence path with the
// saved owner profile. It creates no credentials and never resets sessions.
func localProductExecutor(ctx context.Context, path string) (eval.ProductExecutor, func(), error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	if err := validateEvalConfig(cfg); err != nil {
		return nil, nil, err
	}
	db, err := openEvalDatabase(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	store, err := newConfiguredVectorStore(ctx, cfg)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	closeAll := func() { store.Close(); db.Close() }
	repos := repository.NewRepositories(db.GORM)
	passphrase := cfg.Security.APIKeySecret
	if passphrase == "" {
		passphrase = cfg.JWT.Secret
	}
	codec, err := secret.NewCodecFromPassphrase(passphrase)
	if err != nil {
		closeAll()
		return nil, nil, err
	}
	profiles := service.NewAIProfileService(repos.AIProfile, codec, nil).WithAgentBudgetConfig(cfg.AgentBudget)
	factory := ai.NewFactory()
	retrieval := service.DefaultRAGRetrievalConfig()
	retrieval.TopK, retrieval.CandidateK, retrieval.MinVectorScore = cfg.RAG.TopK, cfg.RAG.CandidateK, cfg.RAG.MinScore
	retrieval.QueryMode, retrieval.RewriteQueries = service.QueryModeOriginal, 1
	if cfg.RAG.RewriteQueries > 1 {
		retrieval.QueryMode = service.QueryModeRewrite
		retrieval.RewriteQueries = cfg.RAG.RewriteQueries
	}
	retrieval.EnableBM25 = false
	if strings.TrimSpace(cfg.RAG.RerankModel) != "" {
		retrieval.RerankerMode = service.RerankerModeModel
		retrieval.RerankerVersion = cfg.RAG.RerankModel
	}
	svc := service.NewChatService(repos, store, service.ChatConfig{TopK: cfg.RAG.TopK, CandidateK: cfg.RAG.CandidateK, MinScore: cfg.RAG.MinScore, RecentTurns: cfg.RAG.RecentTurns, Retrieval: &retrieval, ReviewCitationSupport: true, ModelRerankerFactory: func(profile ai.Profile) service.Reranker {
		client, err := factory.NewRerankClient(profile)
		if err != nil {
			return service.NewModelReranker(nil)
		}
		return service.NewModelReranker(client)
	}})
	agent := service.NewVideoAgentService(svc)
	execution := service.NewConversationExecution(svc, agent, profiles, factory)
	return conversationProductExecutor(execution, repos.AgentExecution.GetRun), closeAll, nil
}
