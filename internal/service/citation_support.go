package service

import (
	"context"
	"encoding/json"
	"strings"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// Model review is a semantic assessment, never a replacement for source mapping
// or human evaluation. Invalid IDs or non-verbatim quotes fail closed.
func reviewCitationSupport(ctx context.Context, chat ai.ChatClient, answer string, citations []Citation) ([]Citation, VideoAgentLoopPlannerCallUsage, error) {
	if len(citations) == 0 || chat == nil {
		return citations, VideoAgentLoopPlannerCallUsage{}, nil
	}
	type evidence struct {
		ID     string   `json:"id"`
		Claims []string `json:"claims"`
		Source string   `json:"source"`
		Quote  string   `json:"quote"`
	}
	var entries []evidence
	for _, c := range citations {
		entries = append(entries, evidence{c.CitationID, c.ClaimTexts, c.DisplayContext, c.AnchorQuote})
	}
	raw, _ := json.Marshal(entries)
	messages := []ai.ChatMessage{{Role: "system", Content: "核对结论与原文的语义支持。只核对给出的来源，不使用一般知识补证；来源合法、可回放不等于支持。保留否定、条件、数量、单位、指代、独立来源和矛盾归属。逐个引用判断原文是否足以支持全部关联结论；核对原有 quote 本身是否支持结论，source 中其他句子只能帮助理解，不能借用它们冒充该编号的引用依据。若依赖未引用的先行条件或不同事实，supported=false。返回原有 quote，不重新选择或改写。只输出 JSON 数组 [{\"id\":\"C1\",\"supported\":true,\"quote\":\"原文\"}]。不足支持则 supported=false，quote 保留原文，不推测。"}, {Role: "user", Content: string(raw)}}
	var actual *ai.ChatUsage
	reviewCtx := ai.WithChatBudget(ctx, 3500, func(u ai.ChatUsage) { actual = &u })
	response, err := chat.Chat(reviewCtx, messages)
	usage := estimatedPlannerCallUsage(messages, response)
	if actual != nil {
		usage.PromptTokens, usage.CompletionTokens = actual.PromptTokens, actual.CompletionTokens
		usage.UsageSource = model.AgentCallUsageActual
		usage.TokenEstimated = false
	}
	result := append([]Citation(nil), citations...)
	if err != nil {
		for i := range result {
			result[i].SupportStatus = "review_unavailable"
		}
		return result, usage, err
	}
	var rows []struct {
		ID        string `json:"id"`
		Supported bool   `json:"supported"`
		Quote     string `json:"quote"`
	}
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	if err := json.Unmarshal([]byte(response), &rows); err != nil {
		for i := range result {
			result[i].SupportStatus = "review_invalid"
		}
		return result, usage, nil
	}
	seen := map[string]bool{}
	for i := range result {
		c := &result[i]
		c.SupportStatus = "not_checked"
		for _, row := range rows {
			if row.ID != c.CitationID || seen[row.ID] {
				continue
			}
			seen[row.ID] = true
			quote := strings.TrimSpace(row.Quote)
			if quote == "" || !strings.Contains(c.DisplayContext, quote) || len([]rune(quote)) > 4000 {
				c.SupportStatus = "review_invalid"
				break
			}
			c.SupportStatus = "unsupported"
			if row.Supported {
				c.SupportStatus = "supported"
			}
			// A citation ID identifies the generation-time sentence and source interval.
			// Review may flag insufficiency, but cannot silently move that ID to
			// another sentence or replace its precise playback mapping.
			if !strings.Contains(quote, c.AnchorQuote) && !strings.Contains(c.AnchorQuote, quote) {
				c.SupportStatus = "unsupported"
			}
			break
		}
	}
	return result, usage, nil
}

// Navigation/verification calls inside Agent retain journal budgets and recovery.
type journaledResearchChat struct {
	chat    ai.ChatClient
	journal *AgentExecutionJournal
	userID  int64
	runID   string
	purpose string
}

func (c *journaledResearchChat) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	encoded, _ := json.Marshal(messages)
	digest := digestAgentValue(string(encoded))
	usage := estimatedPlannerCallUsage(messages, "")
	out, err := c.journal.Execute(ctx, AgentJournalStep{UserID: c.userID, RunID: c.runID, StepID: c.purpose + "-" + digest[:16], Sequence: 1, Kind: "support_context", Action: c.purpose, ToolName: c.purpose, CallKind: model.AgentCallKindPlannerLLM, InternalCall: true, LLMCall: true, ReplaySafe: true, ArgumentsDigest: digest, InputSummary: "受预算约束的概要导航或引用语义复核", ContextChars: usage.ContextChars, EstimatedPromptTokens: usage.PromptTokens}, func() (AgentJournalResult, error) {
		var actual *ai.ChatUsage
		maxOutput := int64(1800)
		if c.purpose == "citation_support_review" {
			maxOutput = 3500
		}
		budgetCtx := ai.WithChatBudget(ctx, maxOutput, func(u ai.ChatUsage) { actual = &u })
		response, err := c.chat.Chat(budgetCtx, messages)
		u := estimatedPlannerCallUsage(messages, response)
		if actual != nil {
			u.PromptTokens, u.CompletionTokens = actual.PromptTokens, actual.CompletionTokens
			u.UsageSource = model.AgentCallUsageActual
			u.TokenEstimated = false
		}
		return AgentJournalResult{Checkpoint: response, OutputRef: c.purpose, Usage: u, MetricsJSON: usageMetrics(u)}, err
	})
	if err != nil {
		return "", err
	}
	if out.BudgetExhausted {
		return "", errAgentExecutionBudgetExhausted
	}
	var response string
	err = json.Unmarshal(out.Checkpoint, &response)
	return response, err
}
