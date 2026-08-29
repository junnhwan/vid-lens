package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type RewriteInput struct {
	Question   string
	Recent     []model.ChatMessage
	NumQueries int
}

type RewriteResult struct {
	Original string
	Queries  []string
	UsedLLM  bool
	Fallback bool
}

type QueryRewriter interface {
	Rewrite(ctx context.Context, input RewriteInput) (RewriteResult, error)
}

type ParsedRewrite struct {
	Queries []string `json:"queries"`
}

type NoopQueryRewriter struct{}

func (NoopQueryRewriter) Rewrite(_ context.Context, input RewriteInput) (RewriteResult, error) {
	original := strings.TrimSpace(input.Question)
	if original == "" {
		return RewriteResult{}, fmt.Errorf("问题不能为空")
	}
	return RewriteResult{
		Original: original,
		Queries:  []string{original},
	}, nil
}

type LLMQueryRewriter struct {
	chat ai.ChatClient
}

func NewLLMQueryRewriter(chat ai.ChatClient) *LLMQueryRewriter {
	return &LLMQueryRewriter{chat: chat}
}

func (r *LLMQueryRewriter) Rewrite(ctx context.Context, input RewriteInput) (RewriteResult, error) {
	original := strings.TrimSpace(input.Question)
	if original == "" {
		return RewriteResult{}, fmt.Errorf("问题不能为空")
	}
	if input.NumQueries <= 1 || r == nil || r.chat == nil {
		return NoopQueryRewriter{}.Rewrite(ctx, input)
	}

	response, err := r.chat.Chat(ctx, buildRewriteMessages(input, original))
	if err != nil {
		return fallbackRewriteResult(original), err
	}
	parsed, err := ParseRewriteJSON(response)
	if err != nil {
		return fallbackRewriteResult(original), err
	}

	queries := stableRewriteQueries(parsed.Queries, original, input.NumQueries)
	if len(queries) <= 1 {
		return fallbackRewriteResult(original), fmt.Errorf("LLM rewrite returned no usable generated queries")
	}
	return RewriteResult{
		Original: original,
		Queries:  queries,
		UsedLLM:  true,
	}, nil
}

func ParseRewriteJSON(text string) (ParsedRewrite, error) {
	text = stripRewriteCodeFence(strings.TrimSpace(text))
	var parsed ParsedRewrite
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return ParsedRewrite{}, err
	}
	parsed.Queries = normalizeRewriteQueries(parsed.Queries)
	return parsed, nil
}

func buildRewriteMessages(input RewriteInput, original string) []ai.ChatMessage {
	var recent strings.Builder
	for _, msg := range input.Recent {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		fmt.Fprintf(&recent, "%s: %s\n", msg.Role, content)
	}
	userPrompt := fmt.Sprintf(`请把用户的视频问答问题改写成适合检索 ASR 转写文本的查询。

要求：
- 返回严格 JSON，格式为 {"queries":["..."]}。
- 只生成 %d 条以内的查询。
- 可以补全上下文、省略指代、同义表达和中英文别名。
- 不要编造最近对话或当前问题里不存在的实体、数字、日期或结论。
- 不要回答问题，只输出查询。

最近对话：
%s
当前问题：%s`, input.NumQueries-1, recent.String(), original)

	return []ai.ChatMessage{
		{Role: "system", Content: "你是 VidLens 视频转写检索查询改写器。"},
		{Role: "user", Content: userPrompt},
	}
}

func fallbackRewriteResult(original string) RewriteResult {
	return RewriteResult{
		Original: original,
		Queries:  []string{original},
		Fallback: true,
	}
}

func stripRewriteCodeFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```")
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[idx+1:]
	}
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

func normalizeRewriteQueries(queries []string) []string {
	seen := make(map[string]bool, len(queries))
	normalized := make([]string, 0, len(queries))
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" || seen[query] {
			continue
		}
		seen[query] = true
		normalized = append(normalized, query)
	}
	return normalized
}

func stableRewriteQueries(generated []string, original string, maxQueries int) []string {
	if maxQueries <= 0 {
		maxQueries = 1
	}
	seen := make(map[string]bool, maxQueries)
	queries := make([]string, 0, maxQueries)
	add := func(query string) {
		query = strings.TrimSpace(query)
		if query == "" || seen[query] || len(queries) >= maxQueries {
			return
		}
		seen[query] = true
		queries = append(queries, query)
	}
	for _, query := range generated {
		if len(queries) >= maxQueries-1 {
			break
		}
		add(query)
	}
	add(original)
	if !seen[original] && len(queries) == maxQueries {
		queries[len(queries)-1] = original
	}
	return queries
}
