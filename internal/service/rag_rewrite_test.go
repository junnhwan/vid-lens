package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vid-lens/internal/ai"
)

type rewriteTestChatClient struct {
	response string
	err      error
	messages []ai.ChatMessage
}

func (c *rewriteTestChatClient) Chat(_ context.Context, messages []ai.ChatMessage) (string, error) {
	c.messages = append([]ai.ChatMessage(nil), messages...)
	if c.err != nil {
		return "", c.err
	}
	return c.response, nil
}

func TestParseRewriteJSONHandlesCodeFence(t *testing.T) {
	result, err := ParseRewriteJSON("```json\n{\"queries\":[\" Redis 锁风险 \",\"watchdog\",\"Redis 锁风险\"]}\n```")
	if err != nil {
		t.Fatalf("parse rewrite JSON: %v", err)
	}
	if got, want := result.Queries, []string{"Redis 锁风险", "watchdog"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("queries = %#v, want %#v", got, want)
	}
}

func TestNoopQueryRewriterReturnsOriginal(t *testing.T) {
	rewriter := NoopQueryRewriter{}
	result, err := rewriter.Rewrite(context.Background(), RewriteInput{
		Question:   " 那这个风险点呢 ",
		NumQueries: 3,
	})
	if err != nil {
		t.Fatalf("noop rewrite: %v", err)
	}
	if result.Original != "那这个风险点呢" {
		t.Fatalf("original = %q", result.Original)
	}
	if got, want := result.Queries, []string{"那这个风险点呢"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("queries = %#v, want %#v", got, want)
	}
	if result.UsedLLM || result.Fallback {
		t.Fatalf("noop flags = used_llm:%v fallback:%v, want false/false", result.UsedLLM, result.Fallback)
	}
}

func TestLLMQueryRewriterFallsBackToOriginalOnBadJSON(t *testing.T) {
	chat := &rewriteTestChatClient{response: "not json"}
	rewriter := NewLLMQueryRewriter(chat)

	result, err := rewriter.Rewrite(context.Background(), RewriteInput{
		Question:   "这个问题怎么解决",
		NumQueries: 3,
	})
	if err == nil {
		t.Fatalf("expected parse error for observability")
	}
	if got, want := result.Queries, []string{"这个问题怎么解决"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("queries = %#v, want fallback %#v", got, want)
	}
	if !result.Fallback || result.UsedLLM {
		t.Fatalf("flags = used_llm:%v fallback:%v, want false/true", result.UsedLLM, result.Fallback)
	}
}

func TestLLMQueryRewriterKeepsOriginalAfterGeneratedQueries(t *testing.T) {
	chat := &rewriteTestChatClient{response: `{"queries":["Redis 分布式锁 WatchDog 风险","锁过期并发问题","Redis 分布式锁 WatchDog 风险"]}`}
	rewriter := NewLLMQueryRewriter(chat)

	result, err := rewriter.Rewrite(context.Background(), RewriteInput{
		Question:   "那这个风险点呢",
		NumQueries: 3,
	})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	want := []string{"Redis 分布式锁 WatchDog 风险", "锁过期并发问题", "那这个风险点呢"}
	if strings.Join(result.Queries, "|") != strings.Join(want, "|") {
		t.Fatalf("queries = %#v, want %#v", result.Queries, want)
	}
	if !result.UsedLLM || result.Fallback {
		t.Fatalf("flags = used_llm:%v fallback:%v, want true/false", result.UsedLLM, result.Fallback)
	}
	if len(chat.messages) == 0 || !strings.Contains(chat.messages[len(chat.messages)-1].Content, "不要编造") {
		t.Fatalf("rewrite prompt should constrain hallucinated entities, messages=%#v", chat.messages)
	}
}

func TestLLMQueryRewriterFallsBackOnChatError(t *testing.T) {
	chat := &rewriteTestChatClient{err: errors.New("timeout")}
	rewriter := NewLLMQueryRewriter(chat)

	result, err := rewriter.Rewrite(context.Background(), RewriteInput{
		Question:   "RAG 为什么要切片",
		NumQueries: 3,
	})
	if err == nil {
		t.Fatalf("expected chat error for observability")
	}
	if got, want := result.Queries, []string{"RAG 为什么要切片"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("queries = %#v, want %#v", got, want)
	}
	if !result.Fallback {
		t.Fatalf("fallback = false, want true")
	}
}
