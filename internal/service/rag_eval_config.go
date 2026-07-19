package service

import (
	"fmt"
	"sort"
	"strings"
)

type QueryMode string

const (
	QueryModeOriginal   QueryMode = "original"
	QueryModePreprocess QueryMode = "preprocess"
	QueryModeRewrite    QueryMode = "rewrite"

	ChunkerStrategySemanticBoundary = "semantic_boundary"
	RerankerModeNone                = "none"
	RerankerModeDeterministic       = "deterministic"
	RerankerModeModel               = "model"
)

// RAGRetrievalConfig is a fully explicit, hashable experiment configuration.
// Dev runs should persist one file per variant instead of relying on hidden
// defaults. Production callers may use DefaultRAGRetrievalConfig.
type RAGRetrievalConfig struct {
	Name            string    `json:"name" yaml:"name"`
	EnableVector    bool      `json:"enable_vector" yaml:"enable_vector"`
	EnableBM25      bool      `json:"enable_bm25" yaml:"enable_bm25"`
	QueryMode       QueryMode `json:"query_mode" yaml:"query_mode"`
	RewriteQueries  int       `json:"rewrite_queries" yaml:"rewrite_queries"`
	TopK            int       `json:"top_k" yaml:"top_k"`
	CandidateK      int       `json:"candidate_k" yaml:"candidate_k"`
	RRFK            float64   `json:"rrf_k" yaml:"rrf_k"`
	NeighborRadius  int       `json:"neighbor_radius" yaml:"neighbor_radius"`
	MaxContextChars int       `json:"max_context_chars" yaml:"max_context_chars"`
	MinVectorScore  float32   `json:"min_vector_score" yaml:"min_vector_score"`

	// Index-time and post-retrieval variables are part of the frozen experiment
	// config even though chunking is not executed in the query process.
	ChunkerStrategy string `json:"chunker_strategy" yaml:"chunker_strategy"`
	ChunkerVersion  string `json:"chunker_version" yaml:"chunker_version"`
	ChunkSize       int    `json:"chunk_size" yaml:"chunk_size"`
	ChunkOverlap    int    `json:"chunk_overlap" yaml:"chunk_overlap"`
	RerankerMode    string `json:"reranker_mode" yaml:"reranker_mode"`
	RerankerVersion string `json:"reranker_version" yaml:"reranker_version"`
}

func DefaultRAGRetrievalConfig() RAGRetrievalConfig {
	return RAGRetrievalConfig{
		Name: "production-hybrid", EnableVector: true, EnableBM25: true,
		QueryMode: QueryModeRewrite, RewriteQueries: 3,
		TopK: 5, CandidateK: 20, RRFK: defaultRRFK,
		NeighborRadius: 1, MaxContextChars: 4000,
		ChunkerStrategy: ChunkerStrategyRecursiveSentence, ChunkerVersion: RecursiveSentenceChunkerVersion,
		ChunkSize: 800, ChunkOverlap: 100,
		RerankerMode: RerankerModeDeterministic, RerankerVersion: "deterministic-v1",
	}
}

func (c RAGRetrievalConfig) Validate() error {
	var problems []string
	if strings.TrimSpace(c.Name) == "" {
		problems = append(problems, "name is required")
	}
	if !c.EnableVector && !c.EnableBM25 {
		problems = append(problems, "at least one retriever must be enabled")
	}
	switch c.QueryMode {
	case QueryModeOriginal, QueryModePreprocess:
		if c.RewriteQueries > 1 {
			problems = append(problems, "rewrite_queries must be 0 or 1 unless query_mode is rewrite")
		}
	case QueryModeRewrite:
		if c.RewriteQueries < 2 || c.RewriteQueries > 5 {
			problems = append(problems, "rewrite_queries must be between 2 and 5 in rewrite mode")
		}
	default:
		problems = append(problems, fmt.Sprintf("unsupported query_mode %q", c.QueryMode))
	}
	if c.TopK <= 0 || c.TopK > 50 {
		problems = append(problems, "top_k must be in [1,50]")
	}
	if c.CandidateK < c.TopK || c.CandidateK > 100 {
		problems = append(problems, "candidate_k must be between top_k and 100")
	}
	if c.EnableVector && c.MinVectorScore < 0 {
		problems = append(problems, "min_vector_score must not be negative")
	}
	if c.EnableVector && c.EnableBM25 && c.RRFK <= 0 {
		problems = append(problems, "rrf_k must be positive for hybrid retrieval")
	}
	if c.NeighborRadius < 0 || c.NeighborRadius > 5 {
		problems = append(problems, "neighbor_radius must be in [0,5]")
	}
	if c.NeighborRadius > 0 && c.MaxContextChars <= 0 {
		problems = append(problems, "max_context_chars must be positive when neighbor expansion is enabled")
	}
	if c.ChunkSize < 0 || (c.ChunkSize > 0 && c.ChunkOverlap >= c.ChunkSize) || c.ChunkOverlap < 0 {
		problems = append(problems, "chunk size/overlap must satisfy size > overlap >= 0")
	}
	if c.RerankerMode != "" && c.RerankerMode != RerankerModeNone && c.RerankerMode != RerankerModeDeterministic && c.RerankerMode != RerankerModeModel {
		problems = append(problems, fmt.Sprintf("unsupported reranker_mode %q", c.RerankerMode))
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid retrieval config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (c RAGRetrievalConfig) ValidateStrictExperiment() error {
	if err := c.Validate(); err != nil {
		return err
	}
	var missing []string
	for field, value := range map[string]string{
		"chunker_strategy": c.ChunkerStrategy,
		"chunker_version":  c.ChunkerVersion,
		"reranker_mode":    c.RerankerMode,
		"reranker_version": c.RerankerVersion,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, field)
		}
	}
	if c.ChunkSize <= 0 || c.ChunkOverlap < 0 || c.ChunkOverlap >= c.ChunkSize {
		missing = append(missing, "valid chunk_size/chunk_overlap")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("strict experiment config must freeze %s", strings.Join(missing, ", "))
	}
	return nil
}

// ValidateSingleVariableAblation enforces the dev discipline that a candidate
// changes one retrieval factor only. Name is metadata and is ignored.
func ValidateSingleVariableAblation(base, candidate RAGRetrievalConfig) (string, error) {
	if err := base.ValidateStrictExperiment(); err != nil {
		return "", fmt.Errorf("baseline: %w", err)
	}
	if err := candidate.ValidateStrictExperiment(); err != nil {
		return "", fmt.Errorf("candidate: %w", err)
	}
	type factor struct {
		name    string
		changed bool
	}
	factors := []factor{
		{"enable_vector", base.EnableVector != candidate.EnableVector},
		{"enable_bm25", base.EnableBM25 != candidate.EnableBM25},
		{"query", base.QueryMode != candidate.QueryMode || base.RewriteQueries != candidate.RewriteQueries},
		{"top_k", base.TopK != candidate.TopK},
		{"candidate_k", base.CandidateK != candidate.CandidateK},
		{"rrf_k", base.RRFK != candidate.RRFK},
		{"neighbor_radius", base.NeighborRadius != candidate.NeighborRadius},
		{"max_context_chars", base.MaxContextChars != candidate.MaxContextChars},
		{"min_vector_score", base.MinVectorScore != candidate.MinVectorScore},
		{"chunker_strategy", base.ChunkerStrategy != candidate.ChunkerStrategy},
		{"chunker_version", base.ChunkerVersion != candidate.ChunkerVersion},
		{"chunk_size", base.ChunkSize != candidate.ChunkSize},
		{"chunk_overlap", base.ChunkOverlap != candidate.ChunkOverlap},
		{"reranker_mode", base.RerankerMode != candidate.RerankerMode},
		{"reranker_version", base.RerankerVersion != candidate.RerankerVersion},
	}
	var changed []string
	for _, f := range factors {
		if f.changed {
			changed = append(changed, f.name)
		}
	}
	if len(changed) != 1 {
		return "", fmt.Errorf("candidate must change exactly one factor; changed=%v", changed)
	}
	return changed[0], nil
}
