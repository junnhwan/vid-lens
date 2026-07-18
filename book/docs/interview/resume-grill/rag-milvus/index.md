# RAG 与 pgvector 拷打

> 路径名 `rag-milvus` 为兼容旧链接暂时保留；本页描述当前 PostgreSQL + pgvector 实现。更集中的速背题见 [pgvector + BM25 + RRF](/interview/resume-grill/resume-core/hybrid-rag/)。

## 1. 为什么用 ASR 转写，不用摘要做知识源？

摘要适合浏览，但已经被 LLM 压缩和改写，可能丢失术语、原句和局部事实。VidLens 读取完整 transcription，切成 chunks；citation 也引用这些转写片段，因此检索证据更接近视频原始内容。

当前边界是 ASR 自身也可能识别错误，单视频 RAG 也不是多源知识库。

**证据：** `internal/service/rag_index_build.go`、`internal/model/transcription.go`。

## 2. chunk size 和 overlap 怎么取舍？

chunk 太大时 embedding 会压缩过多内容，命中后 prompt 成本也高；太小时上下文不完整、向量数量增加。overlap 可以减轻句子被边界切断，但会增加重复内容。

当前使用可配置、可复现的固定长度切分，并把 chunker strategy/version/size/overlap 写入 index 状态。它不是语义切分最优解。

**证据：** `internal/service/rag_index_build.go`、`internal/model/rag_index.go`。

## 3. pgvector 如何隔离用户和视频？

应用层先校验 task 归属；向量 SQL 再按 `user_id + task_id + embedding_model` 过滤。task 限定单视频范围，user 防止数据串租户，embedding model 防止不同维度或语义空间混用。

不为每个用户建表或 collection，是因为共享 schema 加组合过滤更适合当前规模，迁移和运维也更简单。

**证据：** `internal/vector/pgvector.go`、`internal/service/rag_index_build.go`。

## 4. 为什么还要 BM25？

语义向量适合近义表达，但可能漏掉专有名词、缩写、数字和精确原词。Go 侧 BM25 使用词频、逆文档频率和长度归一化补充关键词信号；中文以 2 到 4 字 n-gram，Latin 文本按字母数字 token。

当前实现会读取单任务 chunks 后在 Go 中计算，适合项目规模，不等于专业倒排引擎。数据量扩大后是否迁移 PostgreSQL FTS、Bleve 或 OpenSearch，需要基准和业务范围证明。

**证据：** `internal/repository/video_chunk.go`、`internal/service/retrieval_fusion.go`。

## 5. 为什么使用 RRF，而不是直接加分？

向量相似度与 BM25 的分值定义和尺度不同，直接相加需要归一化与权重调参，容易被某一路支配。RRF 只根据排名累加 `1 / (k + rank)`，让两路结果以相同排名语义参与融合。

RRF 不是免调参。`k`、candidate count 和 topK 仍需固定评测集验证。

**证据：** `internal/service/retrieval_fusion.go`、`internal/service/retrieval_fusion_test.go`。

## 6. 为什么 `video_chunks` 和 pgvector 表都保存内容？

`video_chunks` 是关系型 source of truth：支持 BM25、重建、审计、chunk 邻居扩展和状态排查。pgvector 行是相似度检索 projection，同时保留 stable evidence ID 与必要内容，让检索结果能直接组装 citation。

它们不是两套主数据库，而是同一个 PostgreSQL 中的事实源和派生投影。当前仍分两个 transaction 发布，所以必须保留 failed 状态、audit 和 reindex，不能宣称整体强一致。

**证据：** `internal/repository/video_chunk.go`、`internal/vector/pgvector.go`、`internal/service/rag_index_build.go`。

## 7. citation 和 retrieval snapshot 有什么价值？

检索结果保存 evidence ID、chunk index、content、召回来源和排名信息。流式问答先发送 citations，再发送 answer；完成后将 retrieval snapshot 随 assistant message 持久化。

它让用户和开发者能够复盘“当时检索到了什么”，但不能自动证明模型忠实引用或答案正确。

**证据：** `internal/service/chat_prepare.go`、`internal/service/chat_stream.go`、`internal/service/chat_messages.go`、`internal/model/chat.go`。

## 8. SSE 都是真 token streaming 吗？

不是。实现会优先使用 provider 的 `StreamChat`；不支持 streaming 的 client 会退化为完整回答后分块发送。接口体验仍是逐块输出，但面试时不能把 fallback 说成底层 token streaming。

**证据：** `internal/service/chat_stream.go`、`internal/ai/` 中的 streaming client 实现。

## 9. source/projection 不一致怎么恢复？

建索引先写 `video_chunks` source，再发布向量 projection。projection 失败时 index 状态为 failed，转写和 source 不应被抹掉。`rag-audit` 比较 stable identity、content hash 和 metadata，`rag-reindex` 从 source 分页重建 projection。

这是可审计、可恢复的最终一致性，不是分布式事务。

**证据：** `internal/service/rag_projection_audit.go`、`internal/service/rag_reindex.go`、`cmd/rag-audit/`、`cmd/rag-reindex/`。

## 10. 为什么从 Milvus 迁到 pgvector？

第一版 Milvus 是真实实现，也留下过部署排障经历；但当前规模下，独立向量服务增加配置、容器、备份和对账成本，没有证明需要它的大规模 ANN 优势。pgvector 将业务数据、chunk source 与向量 projection 收敛到 PostgreSQL，降低维护和交接成本。

Milvus adapter 和数据暂时保留作为观察期回滚资产。只有向量规模、并发或延迟经基准证明 PostgreSQL 不够时，才重新评估专用向量系统。

**证据：** `internal/vector/factory.go`、`internal/vector/milvus.go`、`docs/pgvector-migration.md`。

## 11. RAG 质量怎么评估？

准备固定问题、相关 chunk 和期望证据，对比 vector-only、BM25-only、hybrid 的 Recall@K、MRR、citation 命中与延迟；答案质量另外评估 groundedness。迁移 case 和 `rag-eval` 提供了基础，但未稳定的数据不能写成生产收益。

**证据：** `cmd/rag-eval/`、`internal/service/rag_eval*.go`、`docs/eval/pgvector-migration-cases.yaml`。
