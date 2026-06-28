package service

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestContextExpanderExpandsHitWithNeighborWindow(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	seedVideoChunks(t, repos, 7, 1, "text-embedding-3-small", []string{
		"chunk-0", "chunk-1", "chunk-2", "chunk-3", "chunk-4", "chunk-5", "chunk-6",
	})
	expander := &ContextExpander{repos: repos, Radius: 1, MaxCharsPerCitation: 200}

	expanded, err := expander.Expand(context.Background(), 7, 1, "text-embedding-3-small", []RetrievedChunk{
		{ChunkID: 500, ChunkIndex: 5, Content: "chunk-5", VectorRank: 2, KeywordRank: 1, RRFScore: 0.3, Source: RetrievalSourceHybrid},
	})
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if len(expanded) != 1 {
		t.Fatalf("len(expanded) = %d, want 1", len(expanded))
	}
	got := expanded[0]
	for _, want := range []string{"chunk-4", "chunk-5", "chunk-6"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expanded content %q missing %q", got.Content, want)
		}
	}
	if got.ChunkID != 500 || got.ChunkIndex != 5 || got.VectorRank != 2 || got.KeywordRank != 1 || got.RRFScore != 0.3 {
		t.Fatalf("anchor metadata changed: %+v", got)
	}
	if got.ExpandedFromChunkIndex != 5 || got.ExpandedWindowStart != 4 || got.ExpandedWindowEnd != 6 {
		t.Fatalf("window trace = from:%d start:%d end:%d, want 5/4/6", got.ExpandedFromChunkIndex, got.ExpandedWindowStart, got.ExpandedWindowEnd)
	}
}

func TestContextExpanderDedupesDuplicateHits(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	seedVideoChunks(t, repos, 7, 1, "text-embedding-3-small", []string{"chunk-0", "chunk-1", "chunk-2"})
	expander := &ContextExpander{repos: repos, Radius: 1, MaxCharsPerCitation: 200}

	expanded, err := expander.Expand(context.Background(), 7, 1, "text-embedding-3-small", []RetrievedChunk{
		{ChunkID: 10, ChunkIndex: 1, Content: "chunk-1", RRFScore: 0.3},
		{ChunkID: 10, ChunkIndex: 1, Content: "chunk-1 duplicate", RRFScore: 0.2},
	})
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if len(expanded) != 1 {
		t.Fatalf("len(expanded) = %d, want duplicate hit deduped to 1: %+v", len(expanded), expanded)
	}
}

func TestContextExpanderRespectsMaxCharsPerCitation(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	seedVideoChunks(t, repos, 7, 1, "text-embedding-3-small", []string{
		"aaaaaaaaaa", "bbbbbbbbbb", "cccccccccc",
	})
	expander := &ContextExpander{repos: repos, Radius: 1, MaxCharsPerCitation: 18}

	expanded, err := expander.Expand(context.Background(), 7, 1, "text-embedding-3-small", []RetrievedChunk{
		{ChunkID: 2, ChunkIndex: 1, Content: "bbbbbbbbbb"},
	})
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if len([]rune(expanded[0].Content)) > 18 {
		t.Fatalf("expanded content length = %d, want <= 18: %q", len([]rune(expanded[0].Content)), expanded[0].Content)
	}
	if !expanded[0].WindowTruncated {
		t.Fatalf("WindowTruncated = false, want true")
	}
}

func TestContextExpanderReturnsOriginalCitationOnRepositoryFailure(t *testing.T) {
	expander := &ContextExpander{Radius: 1, MaxCharsPerCitation: 200}
	original := []RetrievedChunk{{ChunkID: 2, ChunkIndex: 1, Content: "original"}}

	expanded, err := expander.Expand(context.Background(), 7, 1, "text-embedding-3-small", original)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if len(expanded) != 1 || expanded[0].Content != "original" {
		t.Fatalf("expanded = %+v, want original citation", expanded)
	}
	if !containsFallback(expanded[0].Fallbacks, "window_expansion_failed") {
		t.Fatalf("fallbacks = %+v, want window_expansion_failed", expanded[0].Fallbacks)
	}
}

func seedVideoChunks(t *testing.T, repos *repository.Repositories, userID, taskID int64, embeddingModel string, contents []string) {
	t.Helper()
	chunks := make([]model.VideoChunk, 0, len(contents))
	for i, content := range contents {
		chunks = append(chunks, model.VideoChunk{
			UserID:         userID,
			TaskID:         taskID,
			ChunkIndex:     i,
			Content:        content,
			ContentHash:    content,
			EmbeddingModel: embeddingModel,
			EmbeddingDim:   1536,
			VectorID:       content,
		})
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(taskID, embeddingModel, chunks); err != nil {
		t.Fatalf("ReplaceTaskChunks() error = %v", err)
	}
}

func containsFallback(fallbacks []string, want string) bool {
	for _, fallback := range fallbacks {
		if fallback == want {
			return true
		}
	}
	return false
}
