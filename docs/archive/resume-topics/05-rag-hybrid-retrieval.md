# 专题 5：PostgreSQL + pgvector 混合检索与引用式 RAG

## 1. 推荐简历表述

> 以 ASR 转写原文作为 RAG 数据源，将 PostgreSQL `video_chunks` 作为文本事实表、pgvector 作为可重建向量投影，结合 Go 侧 BM25-style 关键词召回与 RRF 融合，按用户、任务和 embedding model 隔离并返回引用片段。

正式 backend 是 pgvector。Milvus 只在迁移观察期作为回滚 adapter，不写成当前正式技术栈。

## 2. 为什么使用 ASR 原文

摘要已经是模型压缩后的派生结果，可能省略数字、专有名词和局部论据。RAG 的检索源应尽量接近原内容，所以索引从已持久化的 ASR transcription 生成 chunks，而不是从摘要生成。

## 3. 事实源与投影

```text
PostgreSQL video_chunks（事实源）
├── user_id / task_id / chunk_index
├── content / content_hash
├── embedding_model / embedding_dim
└── stable vector_id / evidence identity

PostgreSQL pgvector table（可重建投影）
├── scope 与 chunk identity
├── content metadata
└── embedding vector
```

`video_chunks` 用于审计、关键词检索和重建；pgvector 表用于向量相似度检索。Milvus/pgvector 投影失败不能反向删除已提交的 chunk 事实。

## 4. 索引流程

1. RAG Kafka job 获取 task/job processing lease。
2. 从完整 ASR transcription 按 `chunk_size + overlap` 构建候选 chunks。
3. 生成稳定 chunk/vector identity、content hash 和 embedding model scope。
4. 在 PostgreSQL 中替换 chunk 事实。
5. 批量生成 embeddings，并按 `user_id + task_id + embedding_model` 原子替换 pgvector 投影。
6. 更新 `video_rag_indexes` 为 indexed；失败时记录 failed 和 last error，保留事实源供 audit/reindex。

关系表和 pgvector 位于同一个 PostgreSQL，不代表步骤 4-6 已处于一个事务。准确说法是“单库部署、分阶段提交、投影可重建”。

## 5. 查询流程

1. 校验用户、task 和索引状态。
2. 对问题生成 embedding。
3. pgvector 按 `user_id + task_id + embedding_model` 过滤并召回候选。
4. 从当前 task 的 `video_chunks` 执行 Go 侧 BM25-style 关键词评分。
5. 使用 RRF 按两路排名融合，避免直接相加不可比的原始分数。
6. 按现有流水线进行邻居扩展/确定性排序和 token budget 控制。
7. 把 top chunks 放入 prompt，同时把 evidence identity、chunk index 和文本作为 citations 返回。

当前是单视频 scope，不要包装成跨视频企业知识库。

## 6. 为什么选择 pgvector

当前项目的数据量没有足够业务价值支撑独立 Milvus 集群的 etcd、部署、备份和升级成本。PostgreSQL 已经保存业务事实，pgvector 可以复用同一数据库完成当前规模的向量过滤与检索，同时保留 `service.RAGVectorStore/RAGRetriever` 接口，后续确有规模证据时仍可替换 backend。

这不是说 pgvector 永远优于 Milvus。大规模向量、复杂索引治理、独立扩缩容或专门运维团队出现时，独立向量数据库可能更合适。

## 7. BM25-style 与 RRF 的边界

当前关键词检索是 Go 侧针对单 task chunks 的 BM25-style 实现，不是 Elasticsearch/OpenSearch/Bleve，也不应声称具有专业搜索引擎的中文分词、倒排索引和跨库检索能力。

RRF 的核心形式是 `1 / (k + rank)`。它融合排名而不是直接混合余弦分数和关键词分数，适合当前两路分数尺度不同的情况，但参数和质量仍需要冻结评测集验证。

## 8. citations 能证明什么

citations 让用户看到回答使用了哪些转写片段，便于核对和调试，也能在检索失败时观察上下文来源。但它不能证明 LLM 每句话都受证据支持，更不能保证零幻觉。

## 9. 失败恢复与维护工具

- `video_rag_indexes` 保存 indexing/indexed/failed、chunk count、model 和 last error。
- `cmd/rag-audit` 比对事实 manifest 与当前向量投影。
- `cmd/rag-reindex` 从 PostgreSQL chunk 事实重建 pgvector；它故意固定目标为 pgvector，避免回滚配置下误写 Milvus。
- `cmd/rag-eval` 使用冻结案例评估 Recall@K、MRR 等，不把少量本地 latency smoke 外推为性能结论。

## 10. 高频追问

### 为什么不直接把全文塞给 LLM？

长转写会增加上下文成本、延迟和噪声，并可能超过模型窗口。检索先缩小到相关片段，再用引用保留证据位置。

### 为什么需要关键词召回？

向量召回擅长语义相近，但数字、代码、缩写和专有名词可能不稳定；关键词路径补足精确匹配。是否真的提升必须由评测集证明。

### pgvector 和业务表同库，为什么还有两套连接池？

当前 GORM 业务访问和 pgx 向量访问各自持有连接池，但连接同一个 PostgreSQL。这样保留向量 backend 接口并隔离 SQL 实现，代价是需要分别配置和观测连接池；它不是两个数据库，也没有跨数据库双写。

### Milvus 是否已经删除？

没有。正式配置使用 pgvector，Milvus adapter、profile 和迁移期数据暂留作向量回滚。只有观察期结束并明确确认后才删除。

### 是否上线模型 rerank？

正式在线聊天没有模型 Cross-Encoder rerank。当前线上是确定性检索/排序基线；模型 rerank 只允许作为显式离线实验，不能写入简历已完成功能。

## 11. 代码证据

- `internal/service/rag_index*.go`
- `internal/service/rag_pipeline.go`
- `internal/service/retrieval_fusion.go`
- `internal/repository/video_chunk.go`
- `internal/vector/factory.go`
- `internal/vector/pgvector.go`
- `cmd/rag-audit/`、`cmd/rag-reindex/`、`cmd/rag-eval/`

## 12. 30 秒话术

> 我用 ASR 原文而不是摘要做 RAG。`video_chunks` 是 PostgreSQL 里的文本事实源，pgvector 表是可重建投影。查询时先做按 user、task 和 embedding model 隔离的向量召回，再对当前视频 chunks 做 Go 侧 BM25-style 关键词召回，用 RRF 融合排名，最后把片段同时放进 prompt 和 citations。迁移到 pgvector 是为了减少当前规模下维护 MySQL、PostgreSQL和独立向量库的成本，但索引仍是分阶段提交，不能说全链路强一致。

## 13. 不要这么说

- 不要把 Milvus 写成当前正式 backend。
- 不要说 pgvector 是独立数据库或项目仍双写 MySQL/PostgreSQL。
- 不要说同一 PostgreSQL 自动让 RAG 全链路处于一个事务。
- 不要说当前是专业搜索引擎 BM25、跨视频知识库或模型 rerank。
- 不要说 citations 消除了幻觉。
