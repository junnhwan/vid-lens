# 简历主线四：pgvector + BM25 + RRF 混合检索

> 当前主线是 PostgreSQL `video_chunks` 文本事实源 + pgvector 向量投影。Milvus 只保留观察期回滚适配；回答时不要再使用“当前 MySQL + Milvus”口径。

## Q1：RAG 为什么使用 ASR 全文，而不是摘要？

**直接回答：** 摘要已经被模型压缩和改写，可能丢失原句、术语、时间点和局部例子。VidLens 建索引时读取转写全文，citation 也回到转写 chunk，因此知识源更接近原始视频证据。

**防守边界：** 转写也可能有 ASR 误差；citation 提高可追溯性，不自动保证答案正确。

**项目证据：** `internal/service/rag_index_build.go`、`internal/model/transcription.go`。

## Q2：转写文本怎么切 chunk？

**直接回答：** 使用配置化 chunk size 和 overlap 切分，并把 chunker strategy、version、size、overlap 写入 RAG index 状态。这样重建和评测可以知道索引由哪套切分参数产生。

**防守边界：** 固定长度切分容易复现，但不一定遵循语义边界；目前不声称已经实现语义分段。

**项目证据：** `internal/service/rag_index_build.go`、`internal/service/rag_artifact.go`、`internal/model/rag_index.go`。

## Q3：为什么同一个 PostgreSQL 里还要有 `video_chunks` 和向量表？

**直接回答：** 两者职责不同。`video_chunks` 是可审计、可重建的文本事实源，保存稳定 evidence ID、chunk 索引、内容 hash 和 embedding model，也供 BM25 使用；pgvector 表是面向相似度检索的派生投影。

**防守边界：** 同库不等于同一个 transaction。当前先在关系事务中替换 source chunks，再单独发布向量 projection；投影失败会把 RAG index 标为 failed，并通过重试、reindex 和 audit 恢复。

**项目证据：** `internal/repository/video_chunk.go`、`internal/vector/pgvector.go`、`internal/service/rag_index_build.go`、`internal/service/rag_projection_audit.go`。

## Q4：pgvector 检索如何隔离用户、视频和模型？

**直接回答：** 应用层先检查 task 归属，向量查询再同时约束 `user_id`、`task_id` 和 `embedding_model`。这既防止跨用户、跨视频召回，也防止不同 embedding 模型或语义空间混用。

**防守边界：** 数据库过滤不能替代应用层授权，两层都需要。

**项目证据：** `internal/service/rag_index_build.go`、`internal/vector/pgvector.go`。

## Q5：为什么向量召回之外还需要 BM25？

**直接回答：** 向量召回擅长语义相似，但专有名词、缩写、数字和原词匹配可能不稳定。BM25 用词频、逆文档频率和长度归一化补充关键词信号，两路召回覆盖的错误类型不同。

**防守边界：** 当前 BM25 是在单视频 chunk 集合上由 Go 计算，不是 Elasticsearch、OpenSearch 或 PostgreSQL Full Text Search。

**项目证据：** `internal/repository/video_chunk.go`。

## Q6：中文 BM25 怎么处理 token？

**直接回答：** Latin 文本按连续字母数字 token；连续汉字生成 2 到 4 字 n-gram。这样不依赖特定数据库的中文 parser，结果容易在测试中复现。

**防守边界：** n-gram 会增加 token，也不等同于专业中文分词器。

**项目证据：** `internal/repository/video_chunk.go`、`internal/service/retrieval_fusion.go`。

## Q7：为什么不直接相加向量分和 BM25 分？

**直接回答：** 两路分数的含义和尺度不同，直接相加会被数值范围较大的一路支配。RRF 只使用排名，以 `1 / (k + rank)` 累加，更适合融合异构召回结果。

**防守边界：** RRF 的 `k`、candidate count 和最终 topK 仍应通过固定评测集调参，不能只凭几个例子。

**项目证据：** `internal/service/retrieval_fusion.go`、`internal/service/retrieval_fusion_test.go`。

## Q8：当前检索管线是什么顺序？

**直接回答：** 先根据配置做 query rewrite；对每个 query 分别执行 embedding + pgvector 搜索和 BM25 搜索；单 query 内用 RRF 融合，再做跨 query 排名融合；随后可进行邻居 chunk 扩展和配置化 reranker，最后裁剪 topK 并组装 citations。

**防守边界：** 简历稳定主线仍是 pgvector + BM25 + RRF。可选 reranker 不等于已经部署 cross-encoder，也不能宣称固定收益。

**项目证据：** `internal/service/rag_pipeline.go`、`internal/service/rag_rewrite.go`、`internal/service/rag_expand.go`、`internal/service/rag_rerank.go`。

## Q9：引用片段如何返回？

**直接回答：** `RetrievedChunk` 保留 evidence ID、chunk index、content 和召回来源。流式接口先发送 citations，再输出 answer；回答完成后把引用快照随 assistant message 持久化，便于复盘当时使用了哪些证据。

**防守边界：** 引用表示模型拿到了这些上下文，不证明模型一定忠实使用，也不证明检索没有漏召回。

**项目证据：** `internal/service/chat_prepare.go`、`internal/service/chat_stream.go`、`internal/service/chat_messages.go`、`internal/model/chat.go`。

## Q10：source 和 projection 不一致时怎么发现和恢复？

**直接回答：** RAG index 保存状态、chunk count 和 manifest。`rag-audit` 比较 `video_chunks` 与向量 projection 的 stable identity、content hash 和 metadata；`rag-reindex` 可以从 source chunks 分页重建 projection。建索引失败也会保留 failed 状态和错误。

**防守边界：** 这是可检测、可恢复的最终一致性，不是跨两个 transaction 的原子提交。

**项目证据：** `cmd/rag-audit/`、`cmd/rag-reindex/`、`internal/service/rag_projection_audit.go`、`internal/service/rag_reindex.go`。

## Q11：混合检索效果怎么评估？

**直接回答：** 使用固定问题与相关 chunk 标注，对比 vector-only、BM25-only 和 hybrid 的 Recall@K、MRR、citation 命中与延迟；答案质量再单独看 groundedness。项目有 `rag-eval` 工具和迁移 case，但不能把未稳定的本地结果写成生产收益。

**防守边界：** 离线检索命中不等于最终回答正确，还会受 prompt 和 LLM 影响。

**项目证据：** `cmd/rag-eval/`、`internal/service/rag_eval*.go`、`docs/eval/pgvector-migration-cases.yaml`。

## Q12：为什么从 Milvus 迁到 pgvector？最大限制是什么？

**直接回答：** 第一版 Milvus 在当前规模下增加了一套独立部署、备份和对账成本，但没有体现大规模 ANN 的必要性。pgvector 让业务表、chunk source 和向量 projection 统一到 PostgreSQL，降低了运维和理解成本。

当前限制是 BM25 仍会读取单任务 chunks 在 Go 内计算，问答主要限制在单视频，向量索引策略也需要随数据量做基准测试。只有实际规模和延迟证明 PostgreSQL 不够时，才重新评估专用向量数据库或专业倒排系统。

**项目证据：** `internal/vector/factory.go`、`internal/vector/pgvector.go`、`docs/pgvector-migration.md`、`docs/postgresql-single-database-migration.md`。
