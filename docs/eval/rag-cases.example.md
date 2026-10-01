# VidLens RAG Eval Cases Example

> 示例数据只用于说明格式，不包含真实用户视频内容。

这是 live/legacy 案例列表格式，也就是 `go run ./cmd/rag-eval` 在非 `--strict` 模式下 `--cases` 读取的格式（loader 是 `cmd/rag-eval/legacy_eval.go` 里的 `loadCases`）。

它**不是** [dataset-schema.yaml](dataset-schema.yaml) 描述的严格数据集。严格格式必须是带 `schema_version`、`dataset_version`、`manifest` 和 `cases` 的文档，每条 case 必填 `case_id`、`video_id`、`source_group`、`split`、`question`、`category`、`difficulty`、`answerable`，并且该 schema 声明 `additionalProperties: false`，不接受 `expected_chunk_keywords`、`expected_answer_points` 这两个 live 字段。严格数据集的标注与隔离口径见 [annotation-guide.md](annotation-guide.md)。

`loadCases` 只强制校验三件事：`task_id` 必须是正整数，`question` 非空，`expected_chunk_keywords` 非空。`task_id` 还必须在本地库中真实存在（未软删除），否则 preflight 阶段报 `task not found or has been soft-deleted`。`category` 用作报告里的分组标签，`expected_answer_points` 用于答案要点覆盖率。把下面的内容另存为 `.yaml` 文件后，用 `--cases <该文件>` 运行；示例中的 `task_id` 是占位值，必须换成本地真实任务 ID。

```yaml
- task_id: 1
  task_hint: "操作系统课程视频 01"
  category: topic_compare
  question: "视频里怎么解释进程和线程的区别？"
  expected_chunk_keywords:
    - "进程"
    - "线程"
  expected_answer_points:
    - "资源分配"
    - "调度"

- task_id: 2
  task_hint: "后端项目复盘视频 02"
  category: direct_fact
  question: "为什么分布式锁释放时要校验 owner？"
  expected_chunk_keywords:
    - "分布式锁"
    - "owner"
  expected_answer_points:
    - "避免误删别人的锁"
    - "锁续期或任务超时后可能发生 owner 变化"

- task_id: 3
  task_hint: "限流设计讲解视频 03"
  category: direct_qa
  question: "令牌桶限流适合解决什么问题？"
  expected_chunk_keywords:
    - "令牌桶"
    - "限流"
  expected_answer_points:
    - "控制请求速率"
    - "允许一定突发流量"
```
