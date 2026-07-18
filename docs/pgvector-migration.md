# VidLens pgvector migration boundary

## 当前状态

VidLens 的首次本地迁移历史上先把 MySQL 中 161 个 chunk 重建到 pgvector，随后才把 20 张业务表迁入同一个 PostgreSQL `public` schema。当前 server 只使用 PostgreSQL：业务关系数据、RAG 文本事实和 pgvector 投影位于同一数据库；MySQL 仅作为迁移观察期的离线回滚源。历史执行顺序不应复制到新的环境；当前标准流程必须先迁关系事实，再从 PostgreSQL `video_chunks` 重建 pgvector。

```yaml
database:
  host: 127.0.0.1
  port: 5433
  username: <postgres-user>
  password: <postgres-password>
  dbname: vidlens
  sslmode: disable
rag:
  store: pgvector
  enabled: true
  vector_table: vidlens_rag_vectors
```

当前配置生效的前提是 PostgreSQL 已启动并安装 `vector` 扩展。服务启动时会执行幂等的 schema 初始化：

- `CREATE EXTENSION IF NOT EXISTS vector`
- 创建 `vidlens_rag_vectors`（或配置的安全表名）
- 创建 `(user_id, task_id, embedding_model)` 过滤索引

本地 compose 提供了隔离的可选服务：

```bash
docker compose up -d postgres
```

启动后运行真实集成测试（默认单元测试会跳过它）：

```bash
VIDLENS_PGVECTOR_INTEGRATION=1 go test ./internal/vector -run TestPGVectorStoreIntegration -v
```

PowerShell 对应写法：

```powershell
$env:VIDLENS_PGVECTOR_INTEGRATION="1"; go test ./internal/vector -run TestPGVectorStoreIntegration -v
```

PostgreSQL 是 Compose 默认服务，正式配置启动 server 时必须保证该依赖可用。MySQL 仅通过 `legacy-mysql` profile 显式启动；Milvus 数据、代码和 `milvus` profile 暂时保留，作为向量迁移观察窗口内的回滚选项。

## 数据边界

pgvector 使用独立的 `vidlens_rag_vectors` 表，不把它伪装成 PostgreSQL 的 `video_chunks` 事实表：

- PostgreSQL `video_chunks` 保存转写片段、文本 hash、模型和索引状态，是 RAG 文本事实源；
- 同一 PostgreSQL 中的 pgvector 表保存向量检索所需的元数据、原文和 embedding，是可重建投影；
- `user_id`、`task_id`、`embedding_model` 同时参与删除和检索过滤；
- `embedding` 使用 cosine distance，服务层返回 `1 - distance`，与现有 Milvus `Score` 语义一致。

当前实现先使用 exact scan：

```sql
ORDER BY embedding <=> $1::vector
```

当数据量和离线评测证明有必要时，再根据实际查询延迟和召回率选择 HNSW 或 IVFFlat，不能仅凭理论直接宣称已经优化。


## 与关系迁移的职责边界

- `mysql-to-postgres` 只迁移和审计关系表，不打开向量连接；
- `rag-reindex` 只从 PostgreSQL `video_chunks` 读取事实并写 pgvector；
- `rag-audit --all` 读取 source/target scope 并集，能发现 source-only scope 和 target-only scope，是全量向量切换门禁；
- `rag-eval` 在 manifest 一致之后验证固定问题的召回与排序，不能被 count 对账替代。

因此新环境的 pgvector 阶段只能在关系事务提交并通过独立关系审计后开始。`rag-reindex` checkpoint 完成只证明写入流程完成，最终仍必须执行 `rag-audit --all`。

## 可恢复重建命令

当前提供独立命令 `cmd/rag-reindex`，不会把迁移逻辑塞进 server 启动流程：

```bash
go run ./cmd/rag-reindex --config config.yaml --task-id 123
```

默认是 dry-run，只扫描 PostgreSQL `video_chunks`，不会调用 embedding API，也不会写 pgvector 投影。确认范围、模型和维度后，显式加 `--execute` 才会执行：

```bash
go run ./cmd/rag-reindex --config config.yaml --task-id 123 --execute
```

命令的安全边界：

- 数据源固定是 PostgreSQL `video_chunks`，目标固定是同库 pgvector 表；原 Milvus 数据不删除；
- dry-run 和 execute 都要求 `rag.embedding_dim` 为正数，并逐条检查 `video_chunks.embedding_dim` 与目标 pgvector 维度一致；维度漂移会在调用付费 embedding 或写向量前失败；
- 以 `video_chunks.id` 作为稳定游标，成功写入一个向量后再更新 checkpoint；中断后从上一个成功点继续，pgvector upsert 可重复执行；
- checkpoint 默认位于 `.logs/rag-reindex-pgvector.json`，并绑定过滤条件、目标数据库和维度，避免误用到另一批数据；
- 默认 profile 的 embedding model / dimension 与源 chunk 不一致时主动停止，不静默混写；
- `--user-id`、`--task-id`、`--model` 可缩小范围；无过滤条件的全量执行必须显式传 `--all`，`--reset-checkpoint` 只能在确认范围变化后使用；
- embedding 失败会按 `--max-retries` 重试，checkpoint 不会推进到失败的 chunk。

### checkpoint v2 生命周期

execute 模式的 checkpoint 使用显式生命周期，维护者不要只看历史兼容字段 `completed`：

- `status=running`：命令已经写入开始状态但尚未正常收尾。它既可能表示进程仍在执行，也可能表示进程被 kill、主机掉电等异常中断；恢复前先确认没有同 scope 的 `rag-reindex` 进程仍在运行，再使用同一个 checkpoint 继续。
- `status=failed`：命令捕获到失败并完成失败状态写回。查看 `failure_stage` 定位 `initialize_api_key_codec`、`connect_pgvector`、`rebuild_vectors` 或 `complete_checkpoint`，修复原因后使用相同 scope 和 checkpoint 继续。
- `status=completed`：只证明该 checkpoint 对应的向量重建流程正常收尾，不证明 source/target manifest 一致，也不证明 RAG 质量达标。之后仍必须运行 `go run ./cmd/rag-audit --config config.yaml --all` 和固定 RAG 评测。

checkpoint 只持久化上述稳定阶段枚举，不保存 provider、数据库驱动等原始错误正文，避免 API key、DSN 或上游响应意外落盘；完整错误仍通过命令 stderr 返回。v2 会拒绝未知版本和自相矛盾的状态组合。历史 v1 checkpoint 会在读取时于内存中升级为 v2（`completed=true` 映射为 `completed`，否则映射为 `running`），下一次保存时才写成 v2，无需为了升级直接删除可恢复进度。

`rag-eval` 也通过统一的 vector backend factory 读取 `rag.store`，因此切换前可以用同一套评测流程分别运行 Milvus 和 pgvector，而不是评测工具永久硬编码 Milvus。

旧版案例集运行前可以先做不调用 embedding/LLM 的预检：

```powershell
go run ./cmd/rag-eval --config .logs/config.pgvector.yaml --cases docs/eval/rag-quant-cases.yaml --preflight-only --progress
```

预检会按 task 聚合报告任务是否存在、默认 embedding model 是否出现在 PostgreSQL chunk 中，以及 PostgreSQL chunk manifest 与目标 vector manifest 是否一致。发现软删除任务或 hash/count 漂移时会返回非零退出码，而不是把它们统计成普通的 no-result case。

当前用于迁移对照的活跃数据子集位于 `docs/eval/pgvector-migration-cases.yaml`，它只引用仍存在的 task 2 和 task 14。

## 一分钟维护者入口

先记住四个边界，后续 AI 或开发者不需要重新猜迁移设计：

1. **PostgreSQL `video_chunks` 是事实源**：chunk 文本、`vector_id`、`content_hash`、模型和维度以这里为准。
2. **Milvus / pgvector 是可重建的检索投影**：投影发布失败不能反向修改或删除 PostgreSQL chunk。
3. **运行时只选一个 backend**：`vector.NewStore` 和 `vector.BackendConfigFromApplication` 是统一入口；不要在 server、评测命令或新脚本中复制连接参数和 backend switch。
4. **迁移工具固定写 pgvector**：`cmd/rag-reindex` 故意不跟随 `rag.store`，以免在回滚配置下误写 Milvus；checkpoint 在成功 upsert 后才推进。

服务仍支持显式配置文件，适合隔离验证或回滚演练：

```powershell
go run ./cmd/server --config .logs/config.pgvector.yaml
```

当前正式配置为 `config.yaml`，其中 `rag.store: pgvector` 已生效。回滚时先用 `docker compose --profile milvus up -d` 启动回滚依赖，再显式改回 `rag.store: milvus` 并重启服务，不需要修改 PostgreSQL chunk 数据。

正式配置、临时配置和评测配置都必须保持同一个 `embedding_dim`。不要为了让连接成功而自动截断、补零或混写不同模型的向量。

## 已验证的迁移里程碑（2026-07-17）

以下是迁移过程中的本地真实环境证据；其中早期小范围结果用于验证迁移工具，最终全量状态见“全量重建与正式配置切换”一节：

- `pgvector/pgvector:pg17` 上的 extension、schema、upsert、cosine search、tenant 隔离、manifest、重连持久化和 delete 集成测试通过；
- 从 MySQL 对 `user_id=5, task_id=9` 执行小范围重建，共 3 个 chunk，checkpoint 记录 `last_chunk_id=125`、`processed=3`、`completed=true`；
- 对 `user_id=5, task_id=14` 执行第二批重建，共 28 个 chunk，checkpoint 记录 `last_chunk_id=181`、`processed=28`、`completed=true`；
- task 9 和 task 14 共 31 组 `vector_id + content_hash + embedding_model + embedding_dim` 逐字段一致，模型为 `text-embedding-3-small`，维度为 1536；
- 使用临时 pgvector 配置启动 server 后，`/healthz`、`/readyz` 和 metrics 均返回 200，readiness 中当时的 MySQL、Redis、MinIO、Kafka、vector 均为 `up`（这是业务库迁移前的历史验证）；
- 对 task 9 的同一个真实查询 embedding，Milvus 与 pgvector 都返回 3 条结果且排名完全一致。单次本地搜索约 2.7 ms 与 2.6 ms，这只能作为连通性 smoke test，不能作为性能结论；
- 对 task 14 的 3 个固定问题做 vector-only smoke test，两个 backend 的 Top-5 `vector_id` 排名均完全一致；task 14 单次查询约 2.1 ms，同样不能外推为性能结论。

截至 2026-07-17，pgvector 已迁移全部 161 / 161 个 chunk，正式 `config.yaml` 已切换到 pgvector。当前仍处于切换后的观察窗口：Milvus 数据、代码和回滚配置保留，尚未删除双栈。README、MEMORY 和简历中的正式技术栈表述暂不在本轮立即改写，待观察窗口完成后再统一更新，避免把一次本地切换直接包装成长期生产结论。

### task 14 第二批 smoke test

对 task 14 的 28 个 chunk 执行：

```powershell
go run ./cmd/rag-reindex --config .logs/config.pgvector.yaml --task-id 14 --checkpoint .logs/rag-reindex-task-14.json
go run ./cmd/rag-reindex --config .logs/config.pgvector.yaml --task-id 14 --execute --checkpoint .logs/rag-reindex-task-14.json
```

结果为 `candidates=28`、`processed=28`，MySQL 与 pgvector 均为 28 行，`vector_id/content_hash/embedding_model/embedding_dim` 差异为 0。使用 3 个针对当前 task 14 内容的 vector-only 查询分别对比 Milvus 和 pgvector，两个 backend 的 Top-5 排名均一致。这是迁移语义对齐的 smoke test，不是完整 RAG 质量评测。

### task 2 三案例评测结果

在 task 2 的 10 个 chunk 已重建后，使用 `docs/eval/rag-quant-cases.yaml` 中前 3 个固定问题，分别运行了现有 `rag-eval` 的完整流程。产物仅保存在 `.logs/`：

```powershell
go run ./cmd/rag-eval --config config.yaml --cases .logs/rag-eval-task2-smoke.yaml --output .logs/rag-eval-task2-milvus.md --top-k 5 --candidate-k 10 --timeout 10m --progress
go run ./cmd/rag-eval --config .logs/config.pgvector.yaml --cases .logs/rag-eval-task2-smoke.yaml --output .logs/rag-eval-task2-pgvector.md --top-k 5 --candidate-k 10 --timeout 10m --progress
```

可用于迁移判断的结果是：

- Vector-only：Milvus 与 pgvector 均为 Recall@5 `66.7%`、MRR `0.500`、No Result `0%`，3 个问题的命中与首个命中 rank 完全一致；
- Vector + BM25 + RRF：两者同样为 Recall@5 `66.7%`、MRR `0.500`、No Result `0%`，keyword/vector source count 也一致；
- rewrite、model rerank、ordinary/agentic answer 的结果受外部 LLM 非确定输出影响，本轮出现差异，不能把差异归因给向量库；
- 该样本只有 3 个问题，且 latency 是本地单次/少量请求，不足以决定 HNSW、连接池或正式下线 Milvus。

### 活跃迁移评测集的双后端对照

为避免把已软删除 task 5、task 6 的历史案例误当成当前质量基线，新增了只引用仍存在的 task 2、task 14 的固定评测集：`docs/eval/pgvector-migration-cases.yaml`，共 6 个案例。两次运行均先通过 preflight，确认 MySQL chunk manifest 与对应 backend 的 vector manifest 在数量、`vector_id/chunk_index` 和 `content_hash` 上一致。产物仅保存在 `.logs/`：

- Milvus：`.logs/rag-eval-active-milvus.md`
- pgvector：`.logs/rag-eval-active-pgvector.md`

在 vector-only 和 Vector + BM25 + RRF 两个可归因于 vector backend 的模式中：

- 两个 backend 都是 Recall@5 `83.3%`、MRR `0.750`、No Result `0%`；
- 6 个案例的命中情况和首个命中 rank 完全一致；
- hybrid 模式的 source count 也一致（keyword `12`、vector `18`）；
- 本轮平均检索延迟分别为 Milvus `2.57 ms` / pgvector `2.18 ms`（vector-only），但样本小、运行次数少，且不包含共享 query embedding API 调用，不能据此宣称 pgvector 性能更好。

rewrite、window、model rerank、ordinary answer 和 agentic answer 依赖外部 LLM，两个 backend 的运行结果存在差异，不能把这些差异归因给向量数据库。该对照用于支持后续切换决策：在 6 个活跃固定案例上，pgvector 与 Milvus 的核心检索指标、命中和排名一致；这不是全量质量证明，也不代表 pgvector 性能全面优于 Milvus。当前正式配置已切换，但仍处于观察窗口。

需要注意评测数据漂移：当前 `docs/eval/rag-quant-cases.yaml` 中 task 5 的 32 个问题和 task 6 的 9 个问题对应的任务已经软删除，当前 PostgreSQL `video_chunks` 中没有这两个 task 的 chunk。因此这 41 个案例不能作为本轮全量评测结果；观察窗口内仍应重新绑定到仍存在的任务或补充新的、可复现的固定案例。

所以当前结论是“pgvector 在已迁移小样本上的检索语义与现有 Milvus baseline 对齐”，不是“pgvector 全面优于 Milvus”。

### 全量重建与正式配置切换（2026-07-17）

在迁移前先执行了全量 dry-run：MySQL 发现 `161` 个候选 chunk，未调用 embedding API，也未写入 PostgreSQL。随后按 task 分批执行剩余 `120` 个 chunk，每个 task 使用独立 checkpoint：

- task 8：5
- task 10：3
- task 11：6
- task 12：6
- task 13：13
- task 15：15
- task 16：7
- task 24：19
- task 25：6
- task 26：3
- task 30：30
- task 33：7

所有 15 个 task checkpoint（包括此前的 task 2、9、14）均为 `completed=true`，合计 `processed=161`。迁移后使用 MySQL 与 PostgreSQL 导出的 manifest 做全量对照：

```text
mysql_rows=161
pgvector_rows=161
source_only=0
target_only=0
metadata_differences=0
```

对迁移后的 pgvector 运行 `rag-eval`，6 个活跃固定案例的 vector-only 结果为 Recall@5 `83.3%`、MRR `0.750`、No Result `0%`；Vector + BM25 + RRF 结果同样为 Recall@5 `83.3%`、MRR `0.750`、No Result `0%`。该结果与迁移前的 Milvus/pgvector 对照基线一致，说明新增的其他 task 没有破坏已验证任务的检索隔离。最新产物为 `.logs/rag-eval-active-pgvector-post-migration.md`。

随后使用正式配置构建并启动 server，验证：

- `/healthz` 返回 HTTP 200；
- `/readyz` 返回 HTTP 200，当时的 MySQL、Redis、MinIO、Kafka 和 vector 均为 `up`；
- metrics listener 返回 HTTP 200；
- 日志明确记录 `RAG 向量库连接成功: backend=pgvector`。

这一步完成的是“数据重建 + 配置切换”，不是删除 Milvus。Milvus 已移到显式 `milvus` profile，观察窗口内仍保留代码和数据；继续记录 readiness、检索错误和固定案例结果后，再决定是否彻底移除。

## 业务数据库单库收敛（2026-07-17）

向量后端切换完成后，又执行了 MySQL 业务库到 PostgreSQL `public` schema 的迁移。20 张业务表的 count/digest、关系有效性和 19 个 sequence 均通过独立审计；161 条向量 manifest 也保持 source-only、target-only 和 metadata diff 全为 0。详细证据和退出计划见 [`postgresql-single-database-migration.md`](postgresql-single-database-migration.md)。

最终配置不再保留顶层 `postgres`：`database.*` 是正式 PostgreSQL，`rag.vector_table` 是 pgvector 表名，`legacy_mysql.*` 只供离线迁移工具使用。Compose 中 PostgreSQL 默认启动，MySQL 仅属于 `legacy-mysql` profile。

在 MySQL 容器停止后，最终配置的 server smoke 再次得到 `/healthz=200`、`/readyz=200`，且 `database`、Redis、MinIO、Kafka、vector 均为 `up`。因此下面保留的 MySQL 数字和对照描述属于“向量迁移发生时”的历史证据，不代表当前 server 仍依赖 MySQL。

## 索引重建的一致性边界

`RAGIndexService.BuildTaskIndex` 先生成 embedding 并在 PostgreSQL 中替换 `video_chunks`，随后发布向量投影：

- pgvector 实现了 `ReplaceTaskChunks`，在 PostgreSQL 单库事务中完成 task/model scope 的旧向量删除和新向量写入；插入失败会回滚，避免留下“已删旧向量但新向量只写了一部分”的中间状态。
- Milvus 不支持同等的删除/插入事务，因此继续走兼容的 delete + upsert 路径，并明确记录其较弱的失败边界。
- PostgreSQL `video_chunks` 是事实源，同库 pgvector 表是可重建投影。当前服务按阶段分别提交事实写入和向量发布，并未把两者合并为一个事务；向量发布失败时，RAG index 会标记为 `failed`，之后通过重试或 `cmd/rag-reindex` 从 PostgreSQL 重新建立投影。

因此当前不能把整条“chunk 事实写入 + 向量发布”描述成原子提交、双阶段发布或强一致；现有保证是 pgvector task/model scope 内部 replace 的事务性和失败后的可重建性。

## 验证与回滚

迁移前已经执行并留存以下验证：

1. 全量重建前执行 dry-run，确认候选范围、模型、维度和目标表。
2. 使用固定评测集分别运行 Milvus 和 pgvector，比较 Recall@K、MRR、no-result rate 和延迟。
3. 确认 embedding model、维度、`vector_id` 和内容哈希一致后，再切换默认 backend。

当前观察窗口的策略：

4. 保留 Milvus 数据和 `rag.store: milvus` 回滚开关；回滚只改向量 backend 配置并重启，不回写 PostgreSQL chunk。
5. 持续记录 pgvector readiness、检索错误率和固定案例结果；发现问题时停止扩大变更并回滚。
6. 观察窗口完成后，再从 README / MEMORY 中移除 Milvus 回滚说明，并基于实际规模和延迟证据决定是否删除 Milvus 代码、依赖与 Compose profile。

## 维护期 manifest 对账

正式切换后，建议在 reindex、配置变更或发现 RAG 结果异常时，先对明确范围执行只读对账：

```powershell
go run ./cmd/rag-audit --config config.yaml --user-id <user-id> --task-id <task-id> --model <embedding-model>
```

该命令比较 PostgreSQL `video_chunks` 与当前配置向量后端的 manifest，报告 source-only、target-only、metadata mismatch、invalid/duplicate 等问题。它不会自动删除目标向量，不会自动调用 embedding API；只有确认范围和原因后，才执行：

```powershell
go run ./cmd/rag-reindex --config config.yaml --user-id <user-id> --task-id <task-id> --model <embedding-model> --execute
```

单 scope 命令适合日常定位和修复。全量迁移完成后必须额外运行：

```powershell
go run ./cmd/rag-audit --config config.yaml --all
```

`--all` 仅允许 pgvector，按 PostgreSQL 与 pgvector 的 scope 并集逐组报告，因此不会漏掉只存在于目标端的孤儿 scope。`rag-audit` 和 `rag-eval --preflight-only` 复用同一套单 scope 对账语义；该检查能发现投影漂移，但不代表 PostgreSQL 事实表与向量投影处于同一事务，也不能证明线上检索质量或生产级一致性。
