# VidLens Ragas 离线评测适配器

该工具只用于离线评测，不进入在线服务。它读取 Go 严格评测器输出的逐 case JSONL，映射为 Ragas 的 `user_input`、`retrieved_contexts`、`response`、`reference`，并保存每条样例的指标、错误、Judge 原始回调输出以及可获得的 token 用量。

## 安装

```bash
python -m pip install -e "tools/rag_eval[ragas,test]"
```

依赖固定为 `ragas==0.4.3`。`prompt-manifest.json` 的 SHA-256 会写入运行结果；更换 Ragas 或指标 Prompt 后必须更新 manifest，并视为新的评测配置。

## 运行

```bash
python tools/rag_eval/ragas_runner.py \
  --input artifacts/cases.jsonl \
  --output artifacts/ragas-cases.jsonl \
  --judge-model judge-model \
  --embedding-model embedding-model
```

API Key 只从 `OPENAI_API_KEY`（或 `--api-key-env` 指定的环境变量）读取，不写入 artifact。每条 case 单独执行，单条 Judge 异常不会让样例从分母中消失。

## 结果解释

- `*.jsonl`：逐 case 六项 Ragas 指标、错误和原始回调输出。
- `*.summary.json`：成功评分样例的均值和失败样例数。
- Provider 不返回 token 时，token 字段为 `null`，不会伪装成 `0`。
- 当前不根据模型价格表推算费用；没有 Provider 明确费用时 `cost_usd` 为 `null`。
- Ragas/LLM Judge 只是辅助指标，最终简历数字仍需冻结数据集、成对统计和人工盲评校准。
