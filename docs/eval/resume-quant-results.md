# VidLens Resume Quantification Results

## RAG Retrieval A/B Evaluation

- Date: 2026-06-28
- Environment: local
- Code commit: 0f427ef
- Task IDs: 5, 6
- Case count: 19
- Embedding model: text-embedding-3-small
- TopK: 5
- CandidateK: 30
- Latency note: retrieval latency excludes the shared query embedding API call.

| Mode | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Rerank Changed Rank Count | Citation Context Hit Rate | Expanded Context Hit Rate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vector only | 100.0% | 0.939 | 0.0% | 2.56 ms | 0.0% | 0.0 chars | 0 | 100.0% | 0.0% |
| Vector + BM25 + RRF | 100.0% | 0.974 | 0.0% | 3.87 ms | 0.0% | 0.0 chars | 0 | 100.0% | 0.0% |
| Rewrite + MultiQuery + RRF | 100.0% | 0.974 | 0.0% | 14119.04 ms | 0.0% | 3432.3 chars | 0 | 100.0% | 0.0% |
| Rewrite + MultiQuery + RRF + Window + Rerank | 100.0% | 0.921 | 0.0% | 13598.95 ms | 0.0% | 9467.2 chars | 14 | 100.0% | 15.8% |

Conclusion:
On this small self-built video QA evaluation set, the RAG 2.0 modes did not produce a safer aggregate improvement over vector-only retrieval. Do not write a resume claim about retrieval improvement from this run.

Resume sentence:
设计并实现 VidLens 视频 RAG 检索评测框架，支持 vector-only、BM25+RRF、query rewrite、多查询召回、相邻片段回填和 rerank 多模式对比；通过自建 19 条视频 QA case 记录 Recall@5、MRR、无结果率和检索延迟，为后续优化提供可量化依据。

### Vector only Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 15.93 ms |
| 2 | true | 1 | 5 | 2.09 ms |
| 3 | true | 1 | 5 | 2.09 ms |
| 4 | true | 3 | 5 | 1.57 ms |
| 5 | true | 1 | 5 | 2.22 ms |
| 6 | true | 1 | 5 | 1.47 ms |
| 7 | true | 1 | 5 | 2.19 ms |
| 8 | true | 1 | 5 | 1.55 ms |
| 9 | true | 1 | 5 | 1.57 ms |
| 10 | true | 1 | 5 | 1.57 ms |
| 11 | true | 1 | 5 | 1.56 ms |
| 12 | true | 1 | 5 | 1.09 ms |
| 13 | true | 1 | 5 | 1.54 ms |
| 14 | true | 2 | 5 | 2.15 ms |
| 15 | true | 1 | 3 | 1.59 ms |
| 16 | true | 1 | 3 | 2.54 ms |
| 17 | true | 1 | 3 | 2.09 ms |
| 18 | true | 1 | 3 | 1.70 ms |
| 19 | true | 1 | 3 | 2.04 ms |

Source counts: vector=85

### Vector + BM25 + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 5.21 ms |
| 2 | true | 1 | 5 | 4.41 ms |
| 3 | true | 1 | 5 | 4.81 ms |
| 4 | true | 2 | 5 | 3.09 ms |
| 5 | true | 1 | 5 | 5.45 ms |
| 6 | true | 1 | 5 | 3.99 ms |
| 7 | true | 1 | 5 | 3.67 ms |
| 8 | true | 1 | 5 | 3.12 ms |
| 9 | true | 1 | 5 | 3.17 ms |
| 10 | true | 1 | 5 | 3.78 ms |
| 11 | true | 1 | 5 | 3.53 ms |
| 12 | true | 1 | 5 | 3.71 ms |
| 13 | true | 1 | 5 | 3.39 ms |
| 14 | true | 1 | 5 | 3.43 ms |
| 15 | true | 1 | 3 | 5.52 ms |
| 16 | true | 1 | 3 | 4.21 ms |
| 17 | true | 1 | 3 | 3.24 ms |
| 18 | true | 1 | 3 | 3.17 ms |
| 19 | true | 1 | 3 | 2.69 ms |

Source counts: hybrid=83, vector=2

### Rewrite + MultiQuery + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 11865.97 ms |
| 2 | true | 1 | 5 | 9955.11 ms |
| 3 | true | 1 | 5 | 13443.89 ms |
| 4 | true | 1 | 5 | 13571.59 ms |
| 5 | true | 1 | 5 | 12797.18 ms |
| 6 | true | 1 | 5 | 23061.14 ms |
| 7 | true | 1 | 5 | 15080.27 ms |
| 8 | true | 1 | 5 | 13991.55 ms |
| 9 | true | 1 | 5 | 11770.85 ms |
| 10 | true | 1 | 5 | 14750.20 ms |
| 11 | true | 1 | 5 | 12221.32 ms |
| 12 | true | 1 | 5 | 13425.83 ms |
| 13 | true | 1 | 5 | 12214.34 ms |
| 14 | true | 2 | 5 | 17003.11 ms |
| 15 | true | 1 | 3 | 17563.21 ms |
| 16 | true | 1 | 3 | 11840.10 ms |
| 17 | true | 1 | 3 | 15475.54 ms |
| 18 | true | 1 | 3 | 10846.88 ms |
| 19 | true | 1 | 3 | 17383.70 ms |

Source counts: hybrid=68, vector=17

### Rewrite + MultiQuery + RRF + Window + Rerank Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 9764.15 ms |
| 2 | true | 1 | 5 | 13318.17 ms |
| 3 | true | 1 | 5 | 14717.91 ms |
| 4 | true | 2 | 5 | 12281.13 ms |
| 5 | true | 1 | 5 | 18237.73 ms |
| 6 | true | 2 | 5 | 15122.45 ms |
| 7 | true | 1 | 5 | 13228.82 ms |
| 8 | true | 1 | 5 | 14119.72 ms |
| 9 | true | 1 | 5 | 12382.40 ms |
| 10 | true | 1 | 5 | 9685.11 ms |
| 11 | true | 2 | 5 | 9771.38 ms |
| 12 | true | 1 | 5 | 12235.36 ms |
| 13 | true | 1 | 5 | 16685.16 ms |
| 14 | true | 1 | 5 | 14546.68 ms |
| 15 | true | 1 | 3 | 15255.29 ms |
| 16 | true | 1 | 3 | 16287.91 ms |
| 17 | true | 1 | 3 | 12137.22 ms |
| 18 | true | 1 | 3 | 13185.15 ms |
| 19 | true | 1 | 3 | 15418.28 ms |

Source counts: hybrid=80, vector=5

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
