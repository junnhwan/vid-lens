# PostgreSQL 单库迁移说明

## 结论

VidLens 当前本地正式运行架构是 **PostgreSQL + pgvector 单库**：

- PostgreSQL 业务表保存用户、任务、转写、摘要、聊天、RAG chunk、重试和审计数据；
- 同一个 PostgreSQL 实例通过 `vector` 扩展保存 `vidlens_rag_vectors` 向量投影；
- Redis 继续负责锁、限流、临时上传状态和最近聊天；
- Kafka 继续负责异步任务；
- MinIO 继续负责对象存储。

pgvector 是 PostgreSQL 扩展，不是一个必须与关系数据库并存的独立数据库。因此在这个项目规模下，没有业务价值长期维护 MySQL + PostgreSQL 两套主库。

当前 **没有双写**。server、RAG audit/eval/reindex 命令都读取正式 `database.*` PostgreSQL 配置。MySQL 只在迁移观察期作为离线回滚源保留，不参与 `/readyz`，也不是 server 启动依赖。

## 远程部署现状与切换门禁（2026-07-18）

只读核对得到以下事实：

- GitHub Actions 的最近一次成功 Server 部署是 [run 29399085073](https://github.com/junnhwan/vid-lens/actions/runs/29399085073)，部署提交为 `c5b69a1f2b73b3ac4813d72302527836abb0f585`；构建、上传、重启和公网健康检查步骤均成功。
- 该提交的 `cmd/server/main.go` 明确使用 `gorm.Open(mysql.Open(...))` 和 `vector.NewMilvusStore(...)`，`go.mod` 也没有 PostgreSQL driver。因此当前远端已部署二进制仍属于 MySQL + Milvus 运行时，不能因为本地工作区已迁移就声称线上已切换。
- `http://vidlens.wanjune.qzz.io/health` 在核对时返回 HTTP 200，只证明旧服务仍存活；该旧接口不报告 PostgreSQL/pgvector readiness，不能用作数据库迁移证据。
- 本轮直接 SSH 只读审计没有建立连接，因为当前执行环境无法读取本机 `~/.ssh/config`，`baiduyun` 别名未被解析。远程容器、数据量、磁盘、配置文件和备份目录仍属于待核对项。

为使 SSH 恢复后使用同一套受测命令收集证据，工作区提供 `deploy/server-preflight-audit.sh`。它应在创建 `.runtime-generation` **之前**运行。以 Git Bash 为例：

```bash
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
ssh baiduyun \
  'DEPLOY_PATH=/opt/vidlens DATA_ROOT=/opt/vidlens/data SERVICE_NAME=vidlens.service LOCAL_BASE_URL=http://127.0.0.1:18083 bash -s' \
  < deploy/server-preflight-audit.sh \
  | tee ".logs/server-preflight-${stamp}.txt"
status=${PIPESTATUS[0]}
test "$status" -eq 0
```

该命令通过标准输入执行本地受测脚本，不向远端复制密钥或配置。报告只包含配置文件元数据和 SHA-256，不包含 `config.yaml` 内容；还会收集数据目录大小、部署所在文件系统容量、systemd 摘要、VidLens 容器、监听/连接计数以及本机 `/health`、`/healthz`、`/readyz` 的状态码和内容类型。执行后仍应人工检查报告，并避免把主机名、路径、镜像版本和制品摘要公开发布。

退出语义：

- `0`：所有必需采集器均成功，报告末尾为 `audit.collection_complete=true`；这只证明证据收集完整，不代表允许切换；
- `2`：至少一个必需采集器缺失、失败或数据不可解析，报告仍保留已收集内容并以 `audit.collection_complete=false` 结尾；不得继续迁移；
- `1`：路径、服务名、URL 等调用参数不安全，或发生脚本级致命错误；不得继续迁移。

缺少 marker、端点返回 404/000、旧数据库端口仍监听等属于审计发现，不应被“采集器完整”掩盖。报告必须和远程 MySQL 表量、Milvus manifest、备份恢复结果及迁移对账报告一起审核，不能单独作为切换授权。

当前工作区已经把远程部署逻辑收敛到 `deploy/server-deploy.sh`，并由 `deploy/server-deploy_test.sh` 覆盖门禁、成功激活、重启失败和 readiness 失败回滚。`.github/workflows/deploy-server.yml` 不再复制一份难以测试的内联部署实现，而是先执行脚本测试，再把同一脚本通过 SSH 输入远端执行。新版本必须通过本机回环地址的 `/readyz`，不再只依赖兼容旧监控的 `/health`。

远端必须存在以下人工迁移授权标记，且内容完全匹配，脚本才会在任何 release backup 或文件替换前继续：

```text
/opt/vidlens/.runtime-generation = postgres-pgvector-v1
```

缺少标记、标记不匹配或标记被拆成多行时，部署会安全失败，当前 server 和前端不会被替换。这个标记只表达“维护者已完成本次运行代际的迁移准备并允许部署”，**不是数据库 readiness，也不是迁移对账证明**。它不能替代 PostgreSQL/pgvector 连接检查、表级 count/digest、关系/sequence/vector manifest 对账和备份恢复演练。

部署脚本的自动回滚边界是本次 release 的 server 二进制和前端目录。它会备份当前 `config.yaml` 作为诊断/人工恢复资产，但不会自动改写配置，也不会把 PostgreSQL 观察期内的新写入反向同步回 MySQL。首次数据库代际切换失败时，如果需要退回 MySQL + Milvus，仍必须按维护窗口的数据库回滚清单恢复旧配置和旧数据依赖；不能把“二进制已恢复”等同于“数据库迁移已回滚”。

这些工作区改动尚未改变最近一次线上部署的事实。解除远程切换门禁至少需要：

1. 恢复只读 SSH 审计能力，只提供 host/user/port 或可用 alias，不复制、输出私钥和密码；
2. 核对 `vidlens.service`、实际配置路径、当前进程的 3306/5432/19530 连接和容器状态；
3. 核对远程 MySQL 表量、Milvus manifest、可用磁盘和已有备份；
4. 备份旧配置、二进制、前端、MySQL 和 Milvus manifest，并在隔离环境验证数据库备份可恢复；
5. 在维护窗口停止写入并部署 PostgreSQL + `vector` 扩展，先执行关系 dry-run，再执行关系迁移和独立关系审计；
6. 从迁移后的 PostgreSQL `video_chunks` 执行 `rag-reindex --all --execute`，再以 `rag-audit --all` 通过全量向量 manifest 门禁；
7. 使用固定案例运行 RAG 评测，并准备、校验 PostgreSQL + pgvector 正式配置和 `/readyz` 所需依赖；
8. 只有前七项证据全部通过后，才以原子写入方式创建 `.runtime-generation`，随后触发受门禁保护的部署；
9. 验证核心任务、RAG 检索和公网入口后进入观察期，期间保留旧库资产，不立即删除 MySQL/Milvus。

标记应通过同目录临时文件原子替换，避免部署进程读到半写内容：

```bash
umask 077
printf '%s\n' 'postgres-pgvector-v1' > /opt/vidlens/.runtime-generation.tmp
mv /opt/vidlens/.runtime-generation.tmp /opt/vidlens/.runtime-generation
```

如果数据库层实际回滚到 MySQL + Milvus，必须同时删除或改回该标记，防止后续 workflow 再次把 PostgreSQL 版本当成已获准运行。

## 迁移命令职责与标准顺序

后续维护者不要再把关系迁移和向量迁移写进同一个命令：

| 阶段 | 唯一职责 | 不负责 |
|---|---|---|
| `cmd/mysql-to-postgres` | MySQL → PostgreSQL 的 20 张关系表、关系审计与 sequence | embedding、pgvector/Milvus manifest、RAG 质量 |
| `cmd/rag-reindex` | PostgreSQL `video_chunks` → pgvector 的可恢复重建与 checkpoint | MySQL 读取、关系表迁移、最终一致性判定 |
| `cmd/rag-audit --all` | PostgreSQL chunks ↔ pgvector 的全量 scope 并集对账 | 写向量、调用 embedding、关系迁移、质量评测 |
| `cmd/rag-eval` | 使用固定案例验证检索质量 | 数据复制或一致性修复 |

标准执行顺序是：

```text
停止写入
→ 旧库/配置/制品备份
→ 隔离恢复演练
→ mysql-to-postgres dry-run
→ rehearsal execute + audit
→ public execute --confirm-target-schema=public
→ mysql-to-postgres --audit
→ rag-reindex --all --execute
→ rag-audit --all
→ 固定 RAG 评测
→ runtime marker
→ 部署与 /readyz、核心业务验证
```

`mysql-to-postgres` 的 advisory lock 只阻止另一个迁移进程同时进入关系迁移，不会阻止 API、Kafka consumer、retry scheduler 或 cleanup scheduler 写数据库，因此维护窗口和停写检查仍是硬前置。关系报告中的 `relational_committed_audit_pending` 表示复制事务已经提交但独立关系审计尚未完成，此时应运行 `--audit`，不能盲目重新复制。

## 配置和 Compose 边界

正式配置语义：

```yaml
database:       # PostgreSQL：业务表 + RAG 元数据
  # connection fields

rag:
  store: pgvector
  vector_table: vidlens_rag_vectors

legacy_mysql:   # 仅 cmd/mysql-to-postgres 和观察期审计使用
  # connection fields
```

Compose 默认启动 PostgreSQL。MySQL 被放入显式 profile：

```powershell
docker compose up -d postgres
docker compose --profile legacy-mysql up -d mysql
```

`data/mysql` 数据目录、迁移前 dump 和 MySQL 容器定义在观察期内都保留，但 server 不会读取 `legacy_mysql.*`。Milvus 的 `milvus` profile 和数据也暂留，它只解决向量后端回滚，与业务数据库回滚是两件不同的事。

## 已完成的数据迁移证据

2026-07-17 已把 MySQL 业务数据迁入 PostgreSQL `public` schema，并执行独立审计：

```text
business_tables=20
all_tables_match=true
source_relationships_valid=true
target_relationships_valid=true
sequences=19
vector_source=161
vector_target=161
vector_missing=0
vector_target_only=0
vector_metadata_diff=0
```

审计产物：

- `.logs/mysql-to-postgres-public-execute.json`
- `.logs/mysql-to-postgres-public-independent-audit.json`

迁移前备份：

- `.logs/vidlens-mysql-pre-public-20260717-201417.sql`
- SHA-256: `8fbffce7b82cfdd75d4b821ed486c6b4909d02d26beffeb5a4e15626740c3493`
2026-07-18 又把该 dump 恢复到全新的临时 `mysql:8.0` 容器和独立 Docker volume，并将恢复后的 20 张 catalog 业务表逐表与迁移报告核对：`all_catalog_counts_match=true`，包括 161 条 `video_chunks`。演练结束后只删除了本次临时容器和临时 volume，原 `vidlens-mysql`、`data/mysql` 和 dump 均未修改。无密钥恢复证据保存在：

- `.logs/mysql-backup-restore-check-20260718.json`

这只证明上述本地 dump 可读取并能恢复到全新 MySQL 8.0 实例；远程切换前仍必须为远程 MySQL 单独生成、校验并演练其备份，不能拿本地证据替代远程备份。

这些数字证明的是本次本地迁移数据对齐，不应外推成生产级零停机迁移或通用迁移平台。

## 已完成的运行时验证

在 MySQL 容器为 `exited` 的情况下，使用最终 `config.yaml` 重新构建并启动 server，验证结果为：

```text
/healthz = HTTP 200
/readyz = HTTP 200
status = ready
database = up
redis = up
minio = up
kafka = up
vector = up
```

另外，真实 PostgreSQL 集成测试非跳过执行，覆盖：

- GORM schema migration；
- PostgreSQL `FOR UPDATE` 阻塞和 lease 竞争；
- retry budget、usage ledger、repository upsert；
- pgvector extension/schema/upsert/search/delete/manifest。

最终 smoke 日志保存在：

- `.logs/postgres-final-smoke.stdout.log`
- `.logs/postgres-final-smoke.stderr.log`

这证明当前工作区 server 运行时不依赖 MySQL；它不等同于已经把远端部署环境完成切换。

## 一致性边界

关系事实表和 pgvector 表位于同一个 PostgreSQL，并不自动意味着所有业务步骤已经处于同一个事务：

- `video_chunks` 是文本、hash、模型和稳定 chunk ID 的事实源；
- `vidlens_rag_vectors` 是可重建投影；
- pgvector 的 task/model scope replace 在向量表内部使用事务；
- 当前索引服务仍按“写 chunk 事实 -> 发布向量 -> 更新 RAG 状态”分阶段执行。

因此面试中可以说“消除了 MySQL/PostgreSQL 跨库部署和数据同步成本”，但不能说“RAG 索引全链路已经强一致”或“所有资源清理都在一个数据库事务里”。

## 观察期和最终删除计划

观察期内保留：

- MySQL 容器定义和 `legacy-mysql` profile；
- `data/mysql` 数据目录；
- 迁移前 SQL dump 与审计报告；
- `cmd/mysql-to-postgres`、`internal/dbmigration` 和 MySQL GORM 驱动；
- Milvus 容器、数据和 adapter，作为独立的向量回滚资产。

满足以下条件后，再单独执行删除阶段：

1. PostgreSQL 本地和部署环境经过足够观察期；
2. 核心读写、任务重试、清理、RAG 检索和备份恢复均验证；
3. MySQL dump 可恢复性已抽查；
4. 用户明确确认不再需要业务库回滚。

届时才删除 `legacy_mysql` 配置、MySQL Compose service、MySQL 驱动、迁移命令和迁移包。不要提前删除数据目录或备份。

## 面试表述

推荐说法：

> 我最开始为了接 pgvector 临时形成了 MySQL 业务库加 PostgreSQL 向量库的双数据库结构。复盘后发现项目规模没有必要承担两套关系数据库的连接池、备份、迁移和一致性成本，所以我把 20 张业务表迁入 PostgreSQL，让业务数据和 pgvector 共用一个数据库。迁移后做了表级 count/digest、关系约束、sequence 和 161 条向量 manifest 对账，并在停止 MySQL 后验证了服务 readiness 和核心 PostgreSQL 集成测试。MySQL 目前只作为观察期离线回滚源保留，没有双写，也不参与运行时。

不应声称：

- MySQL 数据、容器和驱动已经物理删除；
- 远端生产环境已经完成同样切换；
- 同库后所有 RAG 步骤天然强一致；
- Milvus 已经删除。
