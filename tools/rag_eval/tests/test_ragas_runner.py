import json
from pathlib import Path

import pytest

from tools.rag_eval.ragas_runner import (
    JudgeMetadata,
    MetricScore,
    RagasEvaluationRunner,
    Usage,
    load_case_artifacts,
)


FIXTURE = Path(__file__).parents[1] / "fixtures" / "cases.jsonl"


def test_load_case_artifacts_maps_go_artifact_fields():
    cases = load_case_artifacts(FIXTURE)

    assert len(cases) == 2
    first = cases[0]
    assert first.case_id == "case-001"
    assert first.source_group == "video-group-a"
    assert first.user_input == "视频中的核心结论是什么？"
    assert first.retrieved_contexts == ["第一段证据", "第二段证据"]
    assert first.response == "核心结论是离线评测必须冻结测试集。"
    assert first.reference == "离线评测必须冻结测试集。\n测试集只能最终运行一次。"


def test_load_case_artifacts_rejects_missing_reference(tmp_path: Path):
    source = tmp_path / "bad.jsonl"
    source.write_text(
        json.dumps({
            "case_id": "missing-reference",
            "result": {
                "case": {"question": "问题", "answer_points": []},
                "retrieved_contexts": [{"content": "证据"}],
                "response": "回答",
            },
        }, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ValueError, match="reference"):
        load_case_artifacts(source)


class FakeJudge:
    def evaluate(self, cases, metric_names, metadata):
        assert metric_names == RagasEvaluationRunner.DEFAULT_METRICS
        return {
            cases[0].case_id: {
                "scores": {
                    name: MetricScore(value=0.8, raw={"verdict": "ok"})
                    for name in metric_names
                },
                "usage": Usage(input_tokens=12, output_tokens=3, cost_usd=0.004),
            },
            cases[1].case_id: {
                "scores": {},
                "error": "judge timeout",
                "usage": Usage(),
            },
        }


def test_runner_keeps_per_case_raw_output_errors_and_nullable_usage(tmp_path: Path):
    metadata = JudgeMetadata(
        provider="openai-compatible",
        model="judge-model",
        prompt_name="ragas-defaults",
        prompt_version="ragas-0.4.3",
        prompt_sha256="a" * 64,
        temperature=0.0,
    )
    output = tmp_path / "result.jsonl"
    runner = RagasEvaluationRunner(FakeJudge(), metadata)

    summary = runner.run(load_case_artifacts(FIXTURE), output)

    rows = [json.loads(line) for line in output.read_text(encoding="utf-8").splitlines()]
    assert summary.total_cases == 2
    assert summary.failed_cases == 1
    assert len(rows) == 2
    assert rows[0]["scores"]["faithfulness"]["raw"] == {"verdict": "ok"}
    assert rows[0]["usage"] == {"input_tokens": 12, "output_tokens": 3, "cost_usd": 0.004}
    assert rows[0]["judge"]["model"] == "judge-model"
    assert rows[1]["error"] == "judge timeout"
    assert rows[1]["usage"] == {"input_tokens": None, "output_tokens": None, "cost_usd": None}


def test_trace_collector_is_compatible_with_langchain_callback_protocol():
    from tools.rag_eval.ragas_runner import _TraceCollector

    collector = _TraceCollector()
    assert collector.ignore_llm is False
    assert collector.raise_error is False
    assert callable(collector.on_chain_start)
    collector.on_chain_start({}, {})
