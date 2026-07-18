# `rag-eval` maintenance guide

`rag-eval` is the offline evaluation command for VidLens retrieval and answer-generation experiments. It intentionally stays in one Go package so that file boundaries describe responsibilities without adding interfaces or cross-package indirection.

## Execution paths

```text
main
└─ parseEvalFlags
   └─ run
      ├─ strict=false → live/legacy evaluation
      │  ├─ load cases and config
      │  ├─ connect MySQL and the configured vector backend
      │  ├─ preflight manifests before paid embedding/LLM calls
      │  ├─ execute retrieval and answer modes
      │  └─ render the Markdown report
      └─ strict=true → strict evaluation
         ├─ validate dataset and experiment registry
         ├─ optionally freeze live evidence
         └─ compare preregistered baseline and candidate artifacts
```

## Rerank boundaries

The repository has three distinct rerank paths. Keep them separate in code, reports, and interview claims:

- The online `cmd/server` chat path wires `service.DeterministicReranker`; it does not read a model-rerank endpoint or model from `config.yaml`.
- Strict evaluation accepts only the retrieval artifact's `reranker_mode` values `none` and `deterministic`. Legacy model-rerank CLI flags are rejected in strict mode.
- Live/legacy evaluation can additionally run a model-rerank experiment, but only when `--rerank-model` is explicit. `--rerank-endpoint` is optional; when omitted, the AI factory may derive `/rerank` from the user's embedding endpoint ending in `/embeddings`. The experiment uses that profile's embedding API key.

Without `--rerank-model`, the legacy command neither creates a rerank client nor executes or reports the model-rerank retrieval mode. This prevents an ordinary evaluation from silently making extra network or paid calls.

```powershell
go run ./cmd/rag-eval --config config.yaml --cases docs/eval/rag-quant-cases.yaml `
  --rerank-model Qwen/Qwen3-Reranker-4B `
  --rerank-endpoint https://api.example.com/v1/rerank
```

`ai.Profile.RerankEndpoint` and `ai.Profile.RerankModel` remain runtime inputs for this experiment. They are not persisted user-profile fields and are not server RAG configuration. Therefore, current evidence does not support claiming online cross-encoder/model rerank.

## File responsibilities

| File | Responsibility |
| --- | --- |
| `main.go` | Process entrypoint and shared vector-store construction only. |
| `options.go` | CLI option model, defaults, parsing, and cross-flag validation. |
| `progress.go` | Optional stderr progress reporting. It must not affect evaluation results. |
| `legacy_eval.go` | Live-database evaluation orchestration, case preparation, and retrieval-mode execution. “Legacy” distinguishes the original Markdown workflow from strict artifact evaluation; it is still supported. |
| `answer_eval.go` | Ordinary RAG versus agentic answer evaluation. |
| `report.go` | Markdown rendering and presentation-only formatting helpers. |
| `preflight.go` | Checks task/chunk/profile/vector manifests before embedding or LLM calls. |
| `strict_eval.go` | Strict dataset validation, frozen-evidence retrieval, preregistered A/B execution, and artifact writing. |
| `artifact_snapshot.go` | Deterministic live-evidence snapshot construction shared by strict mode. |

Tests stay in `package main`, so moving an unexported helper between these files does not require widening its visibility.

## Invariants to preserve

1. MySQL `video_chunks` is the source of truth; Milvus or pgvector is a rebuildable retrieval projection.
2. The selected vector backend must come from `internal/vector.NewStore`; do not add another backend switch inside this command.
3. Live evaluation must finish preflight before any paid embedding or LLM call.
4. Strict evaluation must bind runs to the declared dataset, retrieval config, and frozen evidence hashes.
5. Report formatting must not change retrieval results or mutate evaluation reports.
6. Progress output is optional and goes to stderr so stdout remains usable by scripts.

## Common change locations

- Add or validate a CLI flag: `options.go`, then update flag tests in `main_test.go`.
- Add a retrieval mode: execution in `legacy_eval.go`, presentation in `report.go`, and a focused test.
- Change vector backend behavior: update `internal/vector`, not this command.
- Change manifest checks: `preflight.go` and `preflight_test.go`.
- Change strict evidence or artifact semantics: `artifact_snapshot.go` / `strict_eval.go`, with deterministic snapshot tests.

## Verification

Run the focused command tests while editing:

```powershell
go test ./cmd/rag-eval
```

Before finalizing a backend refactor, run the project gates from the repository root:

```powershell
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
go build ./cmd/server ./cmd/rag-eval ./cmd/rag-reindex ./cmd/rag-audit
```
