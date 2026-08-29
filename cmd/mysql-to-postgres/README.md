# mysql-to-postgres

`mysql-to-postgres` 是 VidLens 的一次性、默认只读的**关系数据迁移工具**。它只负责把 `internal/dbmigration.Catalog()` 中的 20 张业务表从 MySQL 复制到 PostgreSQL，并验证关系数据与 sequence；它不连接、不复制、不清空、也不审计 pgvector/Milvus。

向量投影由两个独立命令负责：

- `cmd/rag-reindex`：从 PostgreSQL `video_chunks` 事实表重建 pgvector；
- `cmd/rag-audit --all`：比较 PostgreSQL chunks 与 pgvector 的全量 manifest，作为向量切换门禁。

这种职责拆分避免关系迁移与向量重建互相等待，也让失败报告能明确说明“关系事务是否已经提交”。

## 安全边界

- 不带模式参数时只执行 `dry-run`，不会创建或修改目标业务表。
- `--execute` 才会写 PostgreSQL，并要求目标业务表**全部不存在**或**全部存在且为空**。
- 对 `public` 执行时必须再次明确确认：`--execute --confirm-target-schema=public`。
- 业务表复制在一个 PostgreSQL 事务中完成；任一表失败则全部回滚。
- MySQL 源读取使用 `REPEATABLE READ + READ ONLY` 一致性快照。
- 显式保留主键、软删除记录和毫秒级时间语义，并核对 count、SHA-256 digest、逻辑关系与 sequence。
- `--execute` 使用 PostgreSQL advisory lock 防止两个关系迁移进程并发执行；锁不等于停写门禁，API、consumer 和 scheduler 仍必须在维护窗口人工停止。
- 关系事务提交后，工具关闭第一代 MySQL/PostgreSQL连接并重新连接，再执行独立 data/sequence 审计。
- 报告只包含 host、port、阶段和聚合审计结果；类型中没有数据库名、用户名、密码、DSN、chunk 正文、embedding 或向量 manifest。
- 报告路径必须在 `.logs/` 下，并通过权限受限的临时文件、`fsync` 和 rename 发布完整 JSON。

## 模式

```powershell
# 默认：只读关系预检，不创建表
 go run ./cmd/mysql-to-postgres --config config.yaml

# 仅收敛 MySQL 源 schema；执行前必须先备份，执行后必须重新 dry-run
 go run ./cmd/mysql-to-postgres --config config.yaml --upgrade-source-schema

# 先在独立 rehearsal schema 演练，不要直接从 public 开始
 go run ./cmd/mysql-to-postgres --config config.yaml `
   --target-schema vidlens_rehearsal_YYYYMMDD `
   --report .logs/mysql-to-postgres-rehearsal.json `
   --execute

# 正式 public 迁移需要精确二次确认
 go run ./cmd/mysql-to-postgres --config config.yaml `
   --target-schema public `
   --confirm-target-schema public `
   --report .logs/mysql-to-postgres-public-execute.json `
   --execute

# 对已复制目标重新连接并独立审计，不写数据
 go run ./cmd/mysql-to-postgres --config config.yaml `
   --target-schema public `
   --report .logs/mysql-to-postgres-public-independent-audit.json `
   --audit
```

`--execute`、`--audit`、`--upgrade-source-schema` 互斥。`--batch-size` 只能是 `1..1000`，默认 100；可用 `--timeout 30m` 设置全局超时。

## 报告完成状态

报告版本当前为 v3。不要只看 `success`，还要同时检查：

| `completion_state` | 含义 | 处理方式 |
|---|---|---|
| `relational_not_committed` | 关系事务没有提交 | 修复失败原因后可重新执行迁移 |
| `relational_committed_audit_pending` | 关系事务已提交，但独立审计尚未完成 | 不要重复复制；运行 `--audit` 并人工核对 |
| `relational_committed_and_audited` | 关系事务已提交且独立审计通过 | 进入 pgvector 重建阶段 |
| `complete` | dry-run、audit 或 source-schema 模式正常完成 | 结合 `mode` 和审计字段解释 |
| `failed` | 非 execute 模式失败 | 根据 `failure_stage` 处理 |

`copy_committed_at` 只有在关系复制事务已经成功提交时才出现。

## 正式迁移顺序

```text
停止 API / Kafka consumer / retry scheduler / cleanup scheduler 写入
→ 备份 MySQL、Milvus manifest、配置与制品
→ 在隔离环境恢复备份并核对
→ mysql-to-postgres dry-run
→ rehearsal schema execute + audit
→ public execute --confirm-target-schema=public
→ mysql-to-postgres --audit
→ rag-reindex --all --execute
→ rag-audit --all
→ 固定 RAG 评测
→ 写入 runtime marker
→ 部署并验证 /readyz 和核心业务
```

必须按阶段保存证据。`mysql-to-postgres` 通过不代表 pgvector 已完成；`rag-reindex` 完成也不代表向量 manifest 或检索质量已通过。

## 回滚边界

工具不会删除 MySQL、旧配置、旧二进制或 Milvus。正式切换后的观察期仍保留这些资产。迁移后如 PostgreSQL 版本发生阻断问题，应停止新版本写入并切回旧二进制与旧配置；当前方案不声称能把观察期产生的新 PostgreSQL 数据自动反向同步到 MySQL。

## 验证

```powershell
$env:VIDLENS_POSTGRES_INTEGRATION_DSN = "postgres://..."
go test -count=1 ./internal/dbmigration ./cmd/mysql-to-postgres
go test -race -count=1 ./internal/dbmigration ./cmd/mysql-to-postgres
go vet ./internal/dbmigration ./cmd/mysql-to-postgres
go build ./cmd/mysql-to-postgres
```

MySQL 只读事务集成测试另使用 `VIDLENS_MYSQL_INTEGRATION_DSN`。环境变量只保存在本机，不写入仓库或报告。
