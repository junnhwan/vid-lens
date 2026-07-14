from __future__ import annotations

import argparse
import dataclasses
import hashlib
import json
import math
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterable, Mapping, Protocol, Sequence


@dataclass(frozen=True)
class EvaluationCase:
    case_id: str
    video_id: str
    source_group: str
    user_input: str
    retrieved_contexts: list[str]
    response: str
    reference: str


@dataclass(frozen=True)
class JudgeMetadata:
    provider: str
    model: str
    prompt_name: str
    prompt_version: str
    prompt_sha256: str
    temperature: float = 0.0

    def __post_init__(self) -> None:
        if not self.provider.strip() or not self.model.strip():
            raise ValueError("judge provider and model are required")
        digest = self.prompt_sha256.lower()
        if len(digest) != 64 or any(ch not in "0123456789abcdef" for ch in digest):
            raise ValueError("prompt_sha256 must be a 64-character SHA-256 digest")


@dataclass(frozen=True)
class Usage:
    input_tokens: int | None = None
    output_tokens: int | None = None
    cost_usd: float | None = None


@dataclass(frozen=True)
class MetricScore:
    value: float | None
    raw: Any = None


@dataclass(frozen=True)
class EvaluationSummary:
    total_cases: int
    failed_cases: int
    metric_means: dict[str, float | None]


class JudgeBackend(Protocol):
    def evaluate(
        self,
        cases: Sequence[EvaluationCase],
        metric_names: Sequence[str],
        metadata: JudgeMetadata,
    ) -> Mapping[str, Mapping[str, Any]]: ...


def _required_text(value: Any, field_name: str, line_number: int) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"line {line_number}: missing {field_name}")
    return value.strip()


def _reference_from_case(case: Mapping[str, Any], line_number: int) -> str:
    points = case.get("answer_points") or []
    texts = [
        str(point.get("text", "")).strip()
        for point in points
        if isinstance(point, Mapping) and str(point.get("text", "")).strip()
    ]
    if not texts:
        texts = [str(value).strip() for value in case.get("expected_answer_points", []) if str(value).strip()]
    if not texts:
        raise ValueError(f"line {line_number}: missing reference answer points")
    return "\n".join(texts)


def _map_artifact(raw: Mapping[str, Any], line_number: int) -> EvaluationCase:
    result = raw.get("result", raw)
    if not isinstance(result, Mapping):
        raise ValueError(f"line {line_number}: result must be an object")
    case = result.get("case")
    if not isinstance(case, Mapping):
        raise ValueError(f"line {line_number}: missing result.case")

    contexts_raw = result.get("retrieved_contexts", [])
    if not isinstance(contexts_raw, list):
        raise ValueError(f"line {line_number}: retrieved_contexts must be an array")
    contexts: list[str] = []
    for index, context in enumerate(contexts_raw):
        if isinstance(context, str):
            text = context.strip()
        elif isinstance(context, Mapping):
            text = str(context.get("content", "")).strip()
        else:
            text = ""
        if not text:
            raise ValueError(f"line {line_number}: retrieved_contexts[{index}] missing content")
        contexts.append(text)

    return EvaluationCase(
        case_id=_required_text(raw.get("case_id") or case.get("case_id"), "case_id", line_number),
        video_id=str(case.get("video_id", "")).strip(),
        source_group=str(case.get("source_group", "")).strip(),
        user_input=_required_text(case.get("question"), "user_input", line_number),
        retrieved_contexts=contexts,
        response=str(result.get("response", "")).strip(),
        reference=_reference_from_case(case, line_number),
    )


def load_case_artifacts(path: str | Path) -> list[EvaluationCase]:
    cases: list[EvaluationCase] = []
    seen: set[str] = set()
    with Path(path).open("r", encoding="utf-8-sig") as source:
        for line_number, line in enumerate(source, start=1):
            if not line.strip():
                continue
            try:
                raw = json.loads(line)
            except json.JSONDecodeError as exc:
                raise ValueError(f"line {line_number}: invalid JSON: {exc.msg}") from exc
            if not isinstance(raw, Mapping):
                raise ValueError(f"line {line_number}: artifact must be an object")
            case = _map_artifact(raw, line_number)
            if case.case_id in seen:
                raise ValueError(f"line {line_number}: duplicate case_id {case.case_id!r}")
            seen.add(case.case_id)
            cases.append(case)
    if not cases:
        raise ValueError("input artifact contains no cases")
    return cases


def _json_safe(value: Any) -> Any:
    if dataclasses.is_dataclass(value):
        return {key: _json_safe(item) for key, item in dataclasses.asdict(value).items()}
    if isinstance(value, Mapping):
        return {str(key): _json_safe(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [_json_safe(item) for item in value]
    if isinstance(value, float) and (math.isnan(value) or math.isinf(value)):
        return None
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    if hasattr(value, "item"):
        try:
            return _json_safe(value.item())
        except (TypeError, ValueError):
            pass
    return repr(value)


class RagasEvaluationRunner:
    DEFAULT_METRICS = (
        "context_precision",
        "context_recall",
        "faithfulness",
        "response_relevancy",
        "factual_correctness",
        "noise_sensitivity",
    )

    def __init__(self, backend: JudgeBackend, metadata: JudgeMetadata):
        self.backend = backend
        self.metadata = metadata

    def run(self, cases: Sequence[EvaluationCase], output_path: str | Path) -> EvaluationSummary:
        if not cases:
            raise ValueError("at least one evaluation case is required")
        backend_results = self.backend.evaluate(cases, self.DEFAULT_METRICS, self.metadata)
        output = Path(output_path)
        output.parent.mkdir(parents=True, exist_ok=True)
        values: dict[str, list[float]] = {name: [] for name in self.DEFAULT_METRICS}
        failed = 0

        with output.open("w", encoding="utf-8", newline="\n") as sink:
            for case in cases:
                backend_row = backend_results.get(case.case_id)
                error: str | None = None
                if backend_row is None:
                    backend_row = {}
                    error = "judge returned no result for case"
                elif backend_row.get("error"):
                    error = str(backend_row["error"])
                scores_raw = backend_row.get("scores", {})
                scores: dict[str, Any] = {}
                for metric_name in self.DEFAULT_METRICS:
                    score = scores_raw.get(metric_name)
                    if isinstance(score, MetricScore):
                        value = score.value
                        raw = score.raw
                    elif isinstance(score, Mapping):
                        value = score.get("value")
                        raw = score.get("raw")
                    else:
                        value = None
                        raw = None
                    if value is not None:
                        numeric = float(value)
                        if math.isfinite(numeric):
                            values[metric_name].append(numeric)
                            value = numeric
                        else:
                            value = None
                    scores[metric_name] = {"value": value, "raw": _json_safe(raw)}

                usage = backend_row.get("usage")
                if not isinstance(usage, Usage):
                    usage = Usage(**usage) if isinstance(usage, Mapping) else Usage()
                if error is not None:
                    failed += 1
                row = {
                    "case_id": case.case_id,
                    "video_id": case.video_id,
                    "source_group": case.source_group,
                    "judge": _json_safe(self.metadata),
                    "scores": scores,
                    "raw_judge_outputs": _json_safe(backend_row.get("raw_judge_outputs", [])),
                    "usage": _json_safe(usage),
                    "error": error,
                }
                sink.write(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n")

        summary = EvaluationSummary(
            total_cases=len(cases),
            failed_cases=failed,
            metric_means={
                name: (sum(metric_values) / len(metric_values) if metric_values else None)
                for name, metric_values in values.items()
            },
        )
        summary_path = output.with_suffix(".summary.json")
        summary_path.write_text(
            json.dumps(_json_safe(summary), ensure_ascii=False, sort_keys=True, indent=2) + "\n",
            encoding="utf-8",
        )
        return summary


class _TraceCollector:
    """Best-effort LangChain callback; missing provider usage remains null."""

    ignore_llm = False
    ignore_chain = False
    ignore_agent = False
    ignore_retriever = False
    ignore_chat_model = False
    ignore_retry = False
    ignore_custom_event = False
    raise_error = False
    run_inline = False

    def __init__(self) -> None:
        self.outputs: list[dict[str, Any]] = []
        self.input_tokens = 0
        self.output_tokens = 0
        self.has_usage = False

    def on_llm_end(self, response: Any, **_: Any) -> None:
        generations = []
        for group in getattr(response, "generations", []) or []:
            for generation in group or []:
                generations.append({
                    "text": getattr(generation, "text", None),
                    "message": getattr(getattr(generation, "message", None), "content", None),
                })
        llm_output = getattr(response, "llm_output", None) or {}
        usage = llm_output.get("token_usage") or llm_output.get("usage") or {}
        input_tokens = usage.get("prompt_tokens", usage.get("input_tokens"))
        output_tokens = usage.get("completion_tokens", usage.get("output_tokens"))
        if input_tokens is not None or output_tokens is not None:
            self.has_usage = True
            self.input_tokens += int(input_tokens or 0)
            self.output_tokens += int(output_tokens or 0)
        self.outputs.append({"generations": generations, "provider_metadata": _json_safe(llm_output)})

    def on_llm_error(self, error: BaseException, **_: Any) -> None:
        self.outputs.append({"error": str(error)})

    def __getattr__(self, name: str) -> Any:
        # LangChain dispatches many callback event names. Events unrelated to the
        # judge LLM are intentionally ignored while LLM output stays auditable.
        if name.startswith("on_"):
            return lambda *args, **kwargs: None
        raise AttributeError(name)

    def usage(self) -> Usage:
        if not self.has_usage:
            return Usage()
        return Usage(input_tokens=self.input_tokens, output_tokens=self.output_tokens, cost_usd=None)


class RagasJudgeBackend:
    """Ragas 0.4.3 adapter. Imports the optional dependency only when invoked."""

    _METRIC_OUTPUT_NAMES = {
        "context_precision": "llm_context_precision_with_reference",
        "context_recall": "context_recall",
        "faithfulness": "faithfulness",
        "response_relevancy": "answer_relevancy",
        "factual_correctness": "factual_correctness",
        "noise_sensitivity": "noise_sensitivity_relevant",
    }

    def __init__(self, llm: Any, embeddings: Any):
        self.llm = llm
        self.embeddings = embeddings

    @staticmethod
    def _imports() -> tuple[Any, Any, dict[str, Any]]:
        try:
            from ragas import EvaluationDataset, evaluate
            from ragas.metrics import (
                FactualCorrectness,
                Faithfulness,
                LLMContextPrecisionWithReference,
                LLMContextRecall,
                NoiseSensitivity,
                ResponseRelevancy,
            )
        except ImportError as exc:
            raise RuntimeError(
                "Ragas support is not installed; run `pip install -e tools/rag_eval[ragas]`"
            ) from exc
        return EvaluationDataset, evaluate, {
            "context_precision": LLMContextPrecisionWithReference,
            "context_recall": LLMContextRecall,
            "faithfulness": Faithfulness,
            "response_relevancy": ResponseRelevancy,
            "factual_correctness": FactualCorrectness,
            "noise_sensitivity": NoiseSensitivity,
        }

    def evaluate(
        self,
        cases: Sequence[EvaluationCase],
        metric_names: Sequence[str],
        metadata: JudgeMetadata,
    ) -> Mapping[str, Mapping[str, Any]]:
        EvaluationDataset, evaluate, constructors = self._imports()
        metrics = [constructors[name]() for name in metric_names]
        results: dict[str, Mapping[str, Any]] = {}
        for case in cases:
            collector = _TraceCollector()
            sample = {
                "user_input": case.user_input,
                "retrieved_contexts": case.retrieved_contexts,
                "response": case.response,
                "reference": case.reference,
            }
            try:
                dataset = EvaluationDataset.from_list([sample])
                evaluation = evaluate(
                    dataset=dataset,
                    metrics=metrics,
                    llm=self.llm,
                    embeddings=self.embeddings,
                    callbacks=[collector],
                    raise_exceptions=False,
                    batch_size=1,
                    show_progress=False,
                )
                row = evaluation.to_pandas().iloc[0].to_dict()
                scores: dict[str, MetricScore] = {}
                for name in metric_names:
                    output_name = self._METRIC_OUTPUT_NAMES[name]
                    value = row.get(output_name)
                    if value is None:
                        # Ragas occasionally derives a slightly different name; use the metric instance name.
                        metric = metrics[list(metric_names).index(name)]
                        value = row.get(getattr(metric, "name", output_name))
                    if value is not None:
                        try:
                            numeric = float(value)
                            value = numeric if math.isfinite(numeric) else None
                        except (TypeError, ValueError):
                            value = None
                    scores[name] = MetricScore(value=value, raw={"ragas_value": _json_safe(value)})
                results[case.case_id] = {
                    "scores": scores,
                    "raw_judge_outputs": collector.outputs,
                    "usage": collector.usage(),
                }
            except Exception as exc:  # one failed case must not remove other cases from the denominator
                results[case.case_id] = {
                    "scores": {},
                    "raw_judge_outputs": collector.outputs,
                    "usage": collector.usage(),
                    "error": f"{type(exc).__name__}: {exc}",
                }
        return results


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def _build_openai_backend(args: argparse.Namespace) -> RagasJudgeBackend:
    try:
        from langchain_openai import ChatOpenAI, OpenAIEmbeddings
    except ImportError as exc:
        raise RuntimeError(
            "OpenAI-compatible judge dependencies are missing; install `pip install -e tools/rag_eval[ragas]`"
        ) from exc
    api_key = os.environ.get(args.api_key_env, "")
    if not api_key:
        raise ValueError(f"environment variable {args.api_key_env!r} is required")
    common: dict[str, Any] = {"api_key": api_key}
    if args.base_url:
        common["base_url"] = args.base_url
    llm = ChatOpenAI(model=args.judge_model, temperature=0, **common)
    embeddings = OpenAIEmbeddings(model=args.embedding_model, **common)
    return RagasJudgeBackend(llm=llm, embeddings=embeddings)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Run offline Ragas metrics over VidLens case artifacts")
    parser.add_argument("--input", required=True, type=Path, help="Go evaluator case JSONL")
    parser.add_argument("--output", required=True, type=Path, help="per-case Ragas JSONL")
    parser.add_argument("--judge-provider", default="openai-compatible")
    parser.add_argument("--judge-model", required=True)
    parser.add_argument("--embedding-model", required=True)
    parser.add_argument("--base-url")
    parser.add_argument("--api-key-env", default="OPENAI_API_KEY")
    parser.add_argument(
        "--prompt-manifest",
        type=Path,
        default=Path(__file__).with_name("prompt-manifest.json"),
    )
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    metadata = JudgeMetadata(
        provider=args.judge_provider,
        model=args.judge_model,
        prompt_name="ragas-default-metric-prompts",
        prompt_version="ragas-0.4.3",
        prompt_sha256=_sha256_file(args.prompt_manifest),
        temperature=0.0,
    )
    backend = _build_openai_backend(args)
    summary = RagasEvaluationRunner(backend, metadata).run(load_case_artifacts(args.input), args.output)
    print(json.dumps(_json_safe(summary), ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
