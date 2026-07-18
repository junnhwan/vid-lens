package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

type answerModeResult struct {
	mode   string
	report service.VideoAgentAnswerEvalReport
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
