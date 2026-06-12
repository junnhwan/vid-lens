# VidLens Resume Quantification Results

## RAG Retrieval A/B Evaluation

- Date: 2026-06-12
- Environment: baiduyun
- Code commit: be68283
- Task IDs: 5, 6
- Case count: 19
- Embedding model: text-embedding-3-small
- TopK: 5
- CandidateK: 30
- Latency note: retrieval latency excludes the shared query embedding API call.

| Mode | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency |
| --- | ---: | ---: | ---: | ---: |
| Vector only | 100.0% | 0.939 | 0.0% | 4.43 ms |
| Vector + BM25 + RRF | 100.0% | 0.974 | 0.0% | 7.58 ms |

Conclusion:
On this small self-built video QA evaluation set, hybrid retrieval kept Recall@5 at 100.0% and improved MRR from 0.939 to 0.974. This supports a cautious resume claim about retrieval ranking for exact keywords and project-specific terms, not a broad claim about answer accuracy or production RAG quality.

Resume sentence:
基于 RAG 实现视频智能问答，针对专有名词和精确关键词问题引入 Go 侧 BM25 风格召回，并通过 RRF 融合向量检索结果；在自建 19 条视频问答评估集上 Recall@5 均为 100.0%，MRR 从 0.939 提升至 0.974，返回引用片段提升答案可解释性。

### Vector only Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 38.91 ms |
| 2 | true | 1 | 5 | 7.38 ms |
| 3 | true | 1 | 5 | 2.24 ms |
| 4 | true | 3 | 5 | 2.40 ms |
| 5 | true | 1 | 5 | 4.19 ms |
| 6 | true | 1 | 5 | 3.81 ms |
| 7 | true | 1 | 5 | 1.78 ms |
| 8 | true | 1 | 5 | 1.27 ms |
| 9 | true | 1 | 5 | 1.07 ms |
| 10 | true | 1 | 5 | 1.11 ms |
| 11 | true | 1 | 5 | 1.78 ms |
| 12 | true | 1 | 5 | 1.23 ms |
| 13 | true | 1 | 5 | 2.43 ms |
| 14 | true | 2 | 5 | 1.21 ms |
| 15 | true | 1 | 3 | 1.89 ms |
| 16 | true | 1 | 3 | 1.85 ms |
| 17 | true | 1 | 3 | 1.86 ms |
| 18 | true | 1 | 3 | 4.85 ms |
| 19 | true | 1 | 3 | 2.84 ms |

Source counts: vector=85

### Vector + BM25 + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 18.29 ms |
| 2 | true | 1 | 5 | 5.38 ms |
| 3 | true | 1 | 5 | 6.19 ms |
| 4 | true | 2 | 5 | 2.01 ms |
| 5 | true | 1 | 5 | 4.40 ms |
| 6 | true | 1 | 5 | 3.92 ms |
| 7 | true | 1 | 5 | 1.79 ms |
| 8 | true | 1 | 5 | 2.90 ms |
| 9 | true | 1 | 5 | 1.92 ms |
| 10 | true | 1 | 5 | 72.52 ms |
| 11 | true | 1 | 5 | 3.70 ms |
| 12 | true | 1 | 5 | 2.55 ms |
| 13 | true | 1 | 5 | 2.14 ms |
| 14 | true | 1 | 5 | 1.80 ms |
| 15 | true | 1 | 3 | 2.28 ms |
| 16 | true | 1 | 3 | 1.68 ms |
| 17 | true | 1 | 3 | 4.07 ms |
| 18 | true | 1 | 3 | 3.40 ms |
| 19 | true | 1 | 3 | 2.99 ms |

Source counts: hybrid=83, vector=2

## Async Task Submission Latency

- Date: 2026-06-12
- Environment: baiduyun server
- Endpoint: `POST /api/v1/media/transcribe/:id`, `POST /api/v1/media/analyze/:id`
- Sample count: 8 existing Gin log samples
- Test account: existing authenticated requests in server logs; user/token details not exported
- Task IDs: 2, 5, 6, 7
- Note: collected from existing logs only; no new ASR/LLM/Embedding jobs were triggered for this measurement.
- Exclusion: manual `POST /api/v1/media/task/:id/rag-index` is excluded because current handler runs index building synchronously.
- P95 note: sample count is below 20, so P95 is not used as a resume metric.

| Metric | Value |
| --- | ---: |
| Avg submit latency | 1023.91 ms |
| P50 submit latency | 1018.74 ms |
| P95 submit latency | Not reported (n=8) |
| Max submit latency | 1074.66 ms |
| Background job sample count | 8 |
| Avg background job duration | 21.25 s |
| P50 background job duration | 18 s |
| Max background job duration | 45 s |

Background job sample:

| Job Type | Completed Samples | Duration Range |
| --- | ---: | ---: |
| download | 2 | 23-45 s |
| transcribe | 2 | 16-20 s |
| analyze | 2 | 16-25 s |
| rag_index | 2 | 11-14 s |

Conclusion:
The measured `analyze` and `transcribe` HTTP endpoints perform authorization, task state checks, DB state changes, `task_jobs` updates, and Kafka enqueue, then return without waiting for ASR or summary execution. Existing server samples show HTTP submission around 1.02 s, while completed background jobs in the same deployment took 11-45 s. This supports a resume/interview claim about avoiding HTTP request blocking on the full video-processing job, but not a claim that total video processing became faster.

Resume sentence:
引入 Kafka 将视频下载、ASR 转写、摘要生成和自动 RAG 建库等后台 job 与 HTTP 请求解耦；服务器现有日志 8 次 `analyze/transcribe` 提交样本平均耗时 1023.91 ms、最大 1074.66 ms，而已完成后台 job 耗时 11-45 s（平均 21.25 s），避免请求阻塞在完整视频处理流程中。
