# 文档入口（先看这里）

> 代码与测试始终高于文档。文档冲突时按本页层级处理。

## AI 铁律（开发前必读）

1. **正式栈**：PostgreSQL（唯一关系库）+ **pgvector**（正式向量）。MySQL / Milvus 仅迁移或显式回滚，不是默认运行时。
2. **生产检索配置**以 `cmd/server/wiring.go` 的 `productionRetrievalConfig` 为准：
   - 向量检索：开
   - BM25 hybrid：生产默认**关**（`EnableBM25 = false`）
   - `rewrite_queries > 1` 时启用 LLM rewrite
   - `rerank_model` 非空时启用 model rerank
   - 评测默认配置 ≠ 生产配置
3. **禁止把下列路径当作实现真相**（默认不要读）：`docs/archive/**`、`docs/grill/**`、`docs/superpowers/**`、本机 `book/`（已从 git 主路径移除）、`.worktrees/**`、`.logs/**`、`.tmp/**`。
4. 改业务先改代码 + 测试，再按需同步 `docs/backend-maintenance-map.md`；不要把实现细节写进面试归档。

## 后续 AI：默认只读这些

| 优先级 | 文件 | 用途 |
|---|---|---|
| L0 | `cmd/server/`、`internal/**`、`config.yaml` | 运行时真相 |
| L1 | `docs/backend-maintenance-map.md` | 改哪里、不变量、测试门禁 |
| L1 | `README.md` | 项目入口与启动方式 |
| L2 | `docs/pgvector-migration.md` | 向量迁移/回滚边界 |
| L2 | `docs/postgresql-single-database-migration.md` | 单库迁移边界 |
| L2 | `docs/eval/` | 仅改 RAG 评测/数据集时（见该目录 README） |
| L2 | `docs/stress-test-guide.md` | 压测时 |

本地 AI 指引（通常 gitignore，本机有则读）：

- `CLAUDE.md`：会话上手卡（必须与当前栈一致）
- `AGENTS.md`：证据规则与禁止夸大项

## 默认不要读（省上下文）

| 路径 | 原因 |
|---|---|
| `docs/archive/**` | 面试/简历/历史规划归档（见 [`docs/archive/README.md`](archive/README.md)） |
| `docs/grill/**` | 本地面试拷打材料（gitignore） |
| `docs/superpowers/**` | 本地规划草稿（gitignore） |
| `book/**` | 旧面试 VitePress 站；**已 untrack + gitignore**，非产品事实源，可能仍含 MySQL/Milvus 叙述 |
| `.worktrees/**` | 实验 worktree，不代表 main |
| `.logs/**`、`.tmp/**` | 本地产物 |
| 根目录 `server` / `*.exe` / `coverage.out` | 本地构建产物（gitignore） |

## 当前正式栈（摘要）

- 关系库：**PostgreSQL**（唯一正式 DB）
- 向量：**pgvector**（`rag.store: pgvector`）；Milvus 仅显式回滚
- 异步：Kafka topics `video-download` / `video-transcribe` / `video-analyze` / `video-rag-index`
- 对象存储：MinIO；缓存/锁/限流：Redis
- 问答默认：stream / sync `ChatService`；**Agent 模式为实验功能**
- 评测/审计/重建：`cmd/rag-eval`、`cmd/rag-audit`、`cmd/rag-reindex`，实现在 `internal/ragtool`（非产品主路径）
- 可选 Python 离线辅助：`tools/rag_eval`（Ragas 等），**不是** server 依赖

## 维护时最小同步

1. 改代码 + 测试  
2. 若改了 owner/调用链，更新 `docs/backend-maintenance-map.md`  
3. 不要把实现细节复制进面试归档文档  
4. 后端：`go test -count=1 ./...`
