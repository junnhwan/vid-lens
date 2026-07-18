# RAG Pipeline 源码走读

> 本章描述当前 PostgreSQL + pgvector 主路径，只保留职责、调用链和事实边界。不要复制大段源码或固定行号；具体实现以链接文件为准。Milvus 是观察期回滚 adapter，不是默认讲解主线。

## 1. 先建立数据模型

```text
video_transcriptions.content
  └─ 原始知识源：ASR 完整转写

video_chunks
  └─ 关系型事实源：stable evidence ID、文本、hash、chunk index、model

vidlens_rag_vectors
  └─ pgvector 派生投影：embedding 与检索所需 metadata

video_rag_indexes
  └─ 构建状态：indexing / indexed / failed、manifest、chunker 参数

chat_messages.retrieval_snapshot
  └─ 某次回答实际使用的 citations 快照
```

核心不变量：

- RAG 使用转写，不使用摘要作为知识源；
- task 必须属于当前 user；
- source 与 projection 使用同一个 stable evidence ID；
- 检索同时限定 user、task 和 embedding model；
- source replace 与 projection publish 当前是两个 transaction；
- projection 失败必须可见、可审计、可重建。

## 2. 文件职责

### 索引构建

- `internal/service/rag_index.go`：公共类型、service 依赖与基础入口。
- `internal/service/rag_index_build.go`：构建编排和状态转换。
- `internal/service/rag_artifact.go`：chunk/vector artifact 与 manifest。
- `internal/repository/video_chunk.go`：source chunks 的事务替换、分页、范围读取和 BM25。
- `internal/vector/pgvector.go`：schema、scope replace、search、manifest。

### 检索与问答

- `internal/service/rag_pipeline.go`：query rewrite、两路召回、融合、扩展和裁剪。
- `internal/service/retrieval_fusion.go`：token 提取与 RRF。
- `internal/service/rag_rewrite.go`：查询改写接口和实现。
- `internal/service/rag_expand.go`：邻居 chunk 扩展。
- `internal/service/rag_rerank.go`：配置化可选 reranker；不是固定主线。
- `internal/service/chat_prepare.go`：权限、索引状态、聊天上下文和检索准备。
- `internal/service/chat_stream.go`：citations 与 answer 事件输出。
- `internal/service/chat_messages.go`：问答和 retrieval snapshot 持久化。

### 恢复与评估

- `internal/service/rag_reindex.go`：从 source 分页重建 projection。
- `internal/service/rag_projection_audit.go`：source/projection manifest 对比。
- `cmd/rag-reindex/`、`cmd/rag-audit/`：运维命令。
- `internal/service/rag_eval*.go`、`cmd/rag-eval/`：检索和回答评估。

### Backend 边界

- `internal/vector/backend.go`：backend 名称兼容边界。
- `internal/vector/factory.go`：按配置创建 pgvector 或 Milvus adapter。
- `internal/vector/milvus.go`：观察期回滚实现，不能当成当前默认架构。

## 3. 索引构建调用链

```text
BuildTaskIndex(userID, taskID, embedding, profile)
  -> 校验 context
  -> 查询 task 并校验 user ownership
  -> 读取 transcription
  -> SplitTextIntoChunks
  -> RAG index = indexing
  -> 生成 embeddings 和 stable artifact metadata
  -> PostgreSQL transaction：ReplaceTaskChunks
  -> pgvector transaction：ReplaceTaskChunks projection
  -> RAG index = indexed + chunk count + manifest
```

任一步失败时，构建将 RAG index 标为 failed 并保存截断后的错误。转写仍然保留；若 source 已提交而 projection 失败，后续可以重试或从 source 重建。

### 为什么不直接把两个步骤塞进一个 transaction？

当前 pgvector store 通过独立 `database/sql` pool 和 backend interface 管理，兼容 Milvus 回滚 adapter，因此 source 与 projection 没有共享 GORM transaction。这是明确的当前限制，不应因为它们位于同一个 PostgreSQL 实例就声称原子提交。

未来若结束 backend 观察期并确认只有 pgvector，可以评估共享连接/事务是否值得，但必须先衡量接口收敛和迁移复杂度，不能只为了“看起来强一致”重写。

## 4. 查询调用链

```text
prepare chat
  -> 校验 session/task/user
  -> 读取索引状态与 embedding model
  -> 读取最近聊天上下文
  -> RetrievalPipeline.Retrieve
       -> query rewrite（失败可 fallback）
       -> 对每个 query：
            -> embedding + pgvector search
            -> VideoChunkRepository.SearchByBM25
            -> RRF fuse
       -> cross-query fuse
       -> optional neighbor expansion
       -> optional configured reranker / topK cap
  -> 组装 citations 与 prompt
  -> 先发送 citations event
  -> provider StreamChat 或完整回答分块 fallback
  -> 保存 messages + retrieval snapshot
```

### 过滤范围

pgvector SQL 和 BM25 source 查询都使用：

```text
user_id + task_id + embedding_model
```

应用层授权仍应在进入检索前完成，数据库过滤是第二层隔离，不是授权替代品。

## 5. BM25 与 RRF

### BM25

`VideoChunkRepository.SearchByBM25` 读取单任务、单模型 chunks，在 Go 内计算词频、逆文档频率和长度归一化。Latin 使用字母数字 token；中文生成 2 到 4 字 n-gram。

优点是实现可复现、测试简单、不引入额外搜索系统。限制是查询需要扫描任务范围 chunks，未来跨视频或大语料必须重新评估倒排方案。

### RRF

向量相似度与 BM25 分数不可直接比较。`FuseRetrievedChunks` 按两路 rank 使用：

```text
score += 1 / (k + rank)
```

同一 evidence 同时被两路命中时会累加排名贡献。RRF 降低了归一化难度，但 `k`、candidate count 和 topK 仍需评测集调参。

## 6. Citations 与 streaming

`RetrievedChunk` 保留 evidence ID、chunk ID/index、content、source、vector/keyword rank 和 RRF score。问答接口先输出 citations，再输出 answer，完成后将 retrieval snapshot 保存到 assistant message。

Provider 实现 `StreamChat` 时可以传递真实流式增量；不支持时使用完整回答分块 fallback。两者用户界面相似，但面试描述必须区分。

## 7. 一致性恢复

### Audit

`rag-audit` 比较 source 与 projection 的：

- evidence ID；
- user/task/model scope；
- chunk ID/index；
- content hash；
- source-only、target-only 和 metadata mismatch。

### Reindex

`rag-reindex` 从 `video_chunks` 按稳定 ID 分页读取，重建 pgvector projection。它不重新调用 ASR，也不依赖旧 Milvus 数据。

### 状态

RAG index 的 `failed` 状态使“视频处理已完成但 RAG 不可用”成为可见状态。不要把索引失败覆盖成 transcription 失败，也不要抹掉已有转写价值。

## 8. 测试入口

- `internal/service/rag_index*_test.go`：构建、guard 和发布失败。
- `internal/service/rag_pipeline_test.go`：检索编排。
- `internal/service/retrieval_fusion_test.go`：token 与 RRF。
- `internal/service/rag_projection_audit_test.go`：manifest 差异。
- `internal/service/rag_reindex_test.go`：分页重建。
- `internal/vector/pgvector_test.go`：SQL、校验和 search。
- `internal/vector/pgvector_integration_test.go`：实际 PostgreSQL + pgvector。

## 9. 后续修改检查表

修改索引或检索时，至少确认：

1. 是否仍以 transcription 为 source；
2. ownership 和 `user/task/model` scope 是否完整；
3. stable evidence ID 与 content hash 是否保持；
4. source 成功、projection 失败是否留下 failed 状态；
5. audit/reindex 是否仍能处理新 metadata；
6. citation snapshot 是否保持可读兼容；
7. 是否用固定评测 case 比较召回，而不是凭单个示例；
8. 文档是否区分当前实现、Milvus 历史和未来方向。
