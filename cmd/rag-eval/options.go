package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	rageval "vid-lens/internal/eval"
)

type evalOptions struct {
	configPath     string
	manifestPath   string
	casesPath      string
	outputPath     string
	environment    string
	commit         string
	topK           int
	candidateK     int
	timeout        time.Duration
	progress       bool
	rerankEndpoint string
	rerankModel    string

	strict                      bool
	snapshotOnly                bool
	validateOnly                bool
	preflightOnly               bool
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

func (o evalOptions) legacyModelRerankEnabled() bool {
	return strings.TrimSpace(o.rerankModel) != ""
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
	flags.StringVar(&opts.rerankEndpoint, "rerank-endpoint", "", "legacy model-rerank endpoint; if empty, derive it from the user embedding endpoint")
	flags.StringVar(&opts.rerankModel, "rerank-model", "", "legacy model-rerank model; disabled when empty")

	var split string
	flags.BoolVar(&opts.strict, "strict", false, "enable source-group strict evaluation mode")
	flags.BoolVar(&opts.snapshotOnly, "snapshot-only", false, "freeze live corpus/chunk/vector inputs without running an experiment")
	flags.BoolVar(&opts.validateOnly, "validate-only", false, "validate strict dataset and registry without executing retrieval")
	flags.BoolVar(&opts.preflightOnly, "preflight-only", false, "validate legacy cases against relational chunks and the vector manifest without embedding or LLM calls")
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
	opts.rerankEndpoint = strings.TrimSpace(opts.rerankEndpoint)
	opts.rerankModel = strings.TrimSpace(opts.rerankModel)
	if opts.strict && (opts.rerankEndpoint != "" || opts.rerankModel != "") {
		return evalOptions{}, fmt.Errorf("--rerank-model and --rerank-endpoint are legacy-only and cannot be combined with --strict")
	}
	if opts.rerankEndpoint != "" && opts.rerankModel == "" {
		return evalOptions{}, fmt.Errorf("--rerank-model is required when --rerank-endpoint is set")
	}
	if opts.preflightOnly && opts.strict {
		return evalOptions{}, fmt.Errorf("--preflight-only cannot be combined with --strict")
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
