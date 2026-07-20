# RAG / ASR 评测资料

本目录服务 **离线评测与量化实验**，不是产品 HTTP 路径。实现入口在 `internal/ragtool/` 与 `cmd/rag-eval`、`cmd/rag-audit`、`cmd/rag-reindex`。

## AI 默认应读（改评测/数据集时）

| 文件 | 用途 |
|---|---|
| `dataset-schema.yaml` | 数据集字段约定 |
| `annotation-guide.md` | 标注规范 |
| `experiment-registry.yaml` | 实验登记 |
| `rag-cases.example.md` | 用例示例（最小可跑样例） |
| `rag-quant-cases.yaml` | 量化用例集 |
| `pgvector-migration-cases.yaml` | 迁移相关用例 |
| `legacy-baseline-manifest.json` | 旧基线清单 |

## 可选 / 会话记录（默认不必读）

| 文件 | 说明 |
|---|---|
| `2026-07-19-*.md` | 某次评测会话与设计记录，**不是**当前生产配置 |
| `resume-quant-results.md` | 简历向量化结果摘录，可能滞后于代码 |

改生产检索行为请看 `cmd/server/wiring.go` 的 `productionRetrievalConfig`，不要从本目录的 session 记录反推线上默认值。

## 相关命令

```bash
go run ./cmd/rag-eval -config config.yaml -cases docs/eval/rag-cases.example.md
go run ./cmd/rag-audit ...
go run ./cmd/rag-reindex ...
# 可选 Python 辅助（非 server 路径）
# tools/rag_eval/
```
