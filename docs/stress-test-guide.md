# VidLens 压测与故障演练指南

> 本文只提供**可重复的演练方法和记录模板**，不预填性能结论。没有保存命令、环境和原始结果时，不得把数字写进简历。
>
> 当前正式架构是 PostgreSQL + pgvector 单库。MySQL 与 Milvus 只在迁移观察期作为显式回滚 profile 保留，不是默认运行时依赖。

## 一、测试原则

1. 只在本地或隔离测试环境执行，不直接压公网演示实例。
2. 每次只改变一个变量：并发数、文件大小或故障组件。
3. 同时记录客户端、Go、PostgreSQL、Redis、Kafka、MinIO 和主机资源。
4. 先跑小流量 smoke，再逐级增加；出现错误率、磁盘水位或依赖饱和就停止。
5. 上传测试使用专门测试用户和可删除对象，测试后执行任务清理。
6. 故障演练前确认 PostgreSQL/MinIO 有备份，不用生产数据做破坏性实验。

## 二、环境准备

以下命令使用 Bash、`curl`、`jq`、`hey` 和 Docker Compose。Windows 可以在 Git Bash/WSL 中执行，或者按相同 HTTP 语义改写为 PowerShell。

```bash
BASE="http://127.0.0.1:8080"
TOKEN="你的测试用户 JWT"

# 准备 12 MiB 测试文件。随机字节只适合上传链路，不适合 FFmpeg/ASR。
dd if=/dev/urandom of=/tmp/vidlens-upload.bin bs=1M count=12

curl -fsS "$BASE/healthz" | jq .
curl -fsS "$BASE/readyz" | jq .
docker compose ps
```

### 观察窗口

```bash
# Go 日志（按实际运行方式选择）
docker logs -f <vidlens-server-container> --tail 100

# PostgreSQL 活跃连接和查询
docker exec vidlens-postgres psql -U vidlens -d vidlens -c \
  "select state, count(*) from pg_stat_activity where datname='vidlens' group by state;"

docker exec vidlens-postgres psql -U vidlens -d vidlens -c \
  "select pid, state, wait_event_type, wait_event, now()-query_start as age, left(query,120) from pg_stat_activity where datname='vidlens' and state <> 'idle' order by query_start;"

# Redis、Kafka 和容器资源
docker exec vidlens-redis redis-cli info clients | grep connected_clients
docker exec vidlens-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 --describe --group vidlens-worker
docker stats --no-stream
```

如果使用独立 metrics 监听器，再抓取：

```bash
curl -fsS http://127.0.0.1:19090/metrics > /tmp/vidlens-metrics-before.txt
```

## 三、场景 1：任务列表读基线

**目标：** 测量 Gin → GORM → PostgreSQL 的读链路，并观察深分页差异。

```bash
./hey -n 100 -c 10 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/list?page=1&page_size=20"

./hey -n 2000 -c 100 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/list?page=1&page_size=20"

./hey -n 500 -c 50 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/list?page=100&page_size=20"
```

记录：

- QPS、P50、P95、P99 和非 2xx 数；
- `pg_stat_activity` 连接数和等待事件；
- 容器 CPU/内存；
- page=1 与 page=100 的差异。

不要看到深分页变慢就直接声称索引失效。先用测试环境的 `EXPLAIN (ANALYZE, BUFFERS)` 验证真实执行计划，再决定联合索引或游标分页。

## 四、场景 2：普通上传的 asset 复用与任务语义

**目标：** 并发提交同一文件，验证 `video_assets.file_md5` 的物理唯一性和每用户 task 行为。

```bash
for i in $(seq 1 20); do
  curl -sS -o "/tmp/upload-$i.json" -w "$i %{http_code}\n" \
    -X POST "$BASE/api/v1/media/upload" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@/tmp/vidlens-upload.bin" &
done
wait
```

核验 PostgreSQL：

```bash
FILE_MD5=$(md5sum /tmp/vidlens-upload.bin | awk '{print $1}')

docker exec vidlens-postgres psql -U vidlens -d vidlens -v md5="$FILE_MD5" -c \
  "select id,file_md5,file_size,deleted_at from video_assets where file_md5=:'md5';"

docker exec vidlens-postgres psql -U vidlens -d vidlens -v md5="$FILE_MD5" -c \
  "select id,user_id,asset_id,status,stage,created_at from video_tasks where file_md5=:'md5' order by id;"
```

判断时分开看两条不变量：

- 同一个完整 MD5 的 active asset 应只有一个；
- task 是否复用取决于当前用户与业务幂等语义，不能把“一个 asset”误写成“全系统只能有一个 task”。

Redis 锁用于减少昂贵重复工作，PostgreSQL 唯一约束才是 asset 物理兜底。

## 五、场景 3：Redis 分片状态与 MinIO 合并

**目标：**验证并发分片写入 MinIO 后，Redis Set 能返回完整片号，并能通过 /check-upload、/upload-chunk、/merge-chunks 完成断点续传和服务端合并。

判断标准：

- Redis key 使用 upload:chunks:<file_md5> 保存已上传片号并带 TTL；
- check-upload 返回全部已上传片号，前端只补传缺失片；
- merge 前检查文件大小、分片大小、总片数和缺片；
- MinIO 合并结果大小与原文件一致；
- Redis 状态是可过期的临时续传状态，不描述成 durable upload session。

## 六、场景 4：Kafka lag 与消费能力

使用真实可解析的小视频创建一批测试任务，再触发转写或分析。不要直接向数据库插入半成品 task 作为主测试，因为那会绕过服务不变量。

```bash
for task_id in <测试任务ID列表>; do
  curl -sS -X POST "$BASE/api/v1/media/analyze/$task_id" \
    -H "Authorization: Bearer $TOKEN" &
done
wait

watch -n 1 'docker exec vidlens-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 --describe --group vidlens-worker 2>/dev/null'
```

记录：

- 各 topic/partition 的 LAG 和恢复时间；
- Consumer 日志中的 provider、FFmpeg、DB 等具体等待；
- 成功、失败、重试 task 数；
- 外部 AI 限流是否使吞吐受 provider 而非 Kafka 约束。

消息 lag 下降不代表业务成功，必须同时检查 PostgreSQL `video_tasks`、`task_jobs` 和失败原因。

## 七、场景 5：令牌桶

```bash
SESSION_ID=<聊天会话ID>
./hey -n 200 -c 50 -m POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"这个视频讲了什么"}' \
  "$BASE/api/v1/chat/sessions/$SESSION_ID/messages"
```

记录 200/429 分布和等待后令牌恢复。Redis 异常时当前限流策略可能 fail-open，应把它作为安全/可用性权衡记录，不能误写成所有请求一定拒绝。

## 八、场景 6：任务轮询

```bash
TASK_ID=<当前用户任务ID>
./hey -n 5000 -c 200 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/task/$TASK_ID"
```

观察 PostgreSQL 连接池、P99、容器 CPU 和日志。轮询场景的优化顺序通常是：确认查询和索引 → 限制前端频率/退避 → 条件请求或事件推送；不要没有证据就先引入 WebSocket。

## 九、故障演练

### 9.1 Kafka 宕机：重点验证首次投递窗口

```bash
docker stop vidlens-kafka

curl -sS -w "\nstatus=%{http_code}\n" -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/analyze/<task-id>"

curl -sS -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/task/<task-id>" | jq .

docker start vidlens-kafka
```

必须记录：

- 请求返回值；
- DB 中 task/job/dispatch 状态；
- Kafka 恢复后是否能自动补投；
- 多次补投是否仍由 Consumer lease/幂等保护。

在首次投递恢复机制完成验证前，不得声称 task 创建与 Kafka enqueue 原子，也不得声称实现了 transactional outbox。

### 9.2 Redis 宕机：未完成上传进度不可用

先开始一次尚未合并的分片上传，再执行：

```bash
docker stop vidlens-redis

curl -sS -w "\ncheck-upload=%{http_code}\n" \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/check-upload?file_md5=$FILE_MD5&file_size=$FILE_SIZE&chunk_size=$CHUNK_SIZE&total_chunks=$TOTAL_CHUNKS"

curl -sS -w "\nupload-chunk=%{http_code}\n" \
  -X POST "$BASE/api/v1/media/upload-chunk" \
  -H "Authorization: Bearer $TOKEN" \
  -F "file_md5=$FILE_MD5" \
  -F "chunk_number=0" \
  -F "chunk_size=$CHUNK_SIZE" \
  -F "chunk=@/tmp/vidlens-chunk-0000"

docker start vidlens-redis
```

预期边界：

- Redis 停止会让未完成上传无法读取临时片号，恢复后需要重新检查或重传；
- `/check-upload` 和 `/upload-chunk` 在 Redis 不可用时应明确失败，不能假装已经接受分片；
- 已经完成并写入 PostgreSQL 的 asset/task 不受未完成上传状态丢失影响；
- Redis 锁、限流和最近聊天记忆会受影响；
- 是否 fail-open/fail-closed 要按具体路径记录，不能用一个结论覆盖所有 Redis 用法。

### 9.3 PostgreSQL 宕机：业务与 pgvector 同时不可用

```bash
docker stop vidlens-postgres
curl -sS "$BASE/healthz" -w "\nhealthz=%{http_code}\n"
curl -sS "$BASE/readyz" -w "\nreadyz=%{http_code}\n"
curl -sS -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/list" -w "\nlist=%{http_code}\n"
docker start vidlens-postgres
```

应验证而不是预设：

- liveness 是否仍返回进程存活；
- readiness 是否因正式数据库失败而返回非 2xx；
- PostgreSQL 恢复后连接池是否恢复；
- Consumer 在 DB 不可用时是否避免提交业务成功；
- RAG 向量检索也会失败，因为 pgvector 与业务表在同一 PostgreSQL。

### 9.4 MySQL 停止：不应影响 server

MySQL 只在显式 `legacy-mysql` profile 下作为迁移回滚源。默认根本不应启动它。若观察期正在运行：

```bash
docker stop vidlens-mysql
curl -sS "$BASE/readyz" -w "\nreadyz=%{http_code}\n"
```

server 不读取 `legacy_mysql.*`，因此 MySQL 停止不应改变正式 readiness。该测试证明运行时边界，不证明备份可恢复。

### 9.5 向量投影故障

pgvector 与业务表共享 PostgreSQL，不能通过停止一个独立 Milvus 容器模拟正式向量后端故障。更有价值的隔离测试是：

- 在临时数据库/临时角色中拒绝目标 vector table 权限；
- 或在独立测试配置中指定不可访问的 pgvector 目标；
- 验证 RAG build/search 的错误、readiness 和普通业务接口边界；
- 恢复后运行 `cmd/rag-audit` 对账，再由显式 `cmd/rag-reindex` 修复投影。

不要在正式 `public` schema 上 rename/drop 向量表做演练。Milvus profile 只能验证回滚 adapter，不代表当前正式架构。

### 9.6 MinIO 宕机

停止 MinIO 后分别测试普通上传、chunk PUT、complete、任务媒体读取和任务列表。预期关系状态仍可读，但字节相关操作失败；恢复后重点检查 session ledger 是否错误记录了未落盘 chunk，以及是否产生孤儿候选对象。

## 十、结果记录模板

```markdown
# VidLens 压测/故障记录：<场景>

- 日期：
- Git commit / 工作树说明：
- 环境：CPU、内存、磁盘、Docker 版本
- 配置：PostgreSQL/Redis/Kafka/MinIO 版本与关键非秘密参数
- 数据：文件大小、任务数、chunk 数
- 负载：总请求、并发、持续时间

## 原始结果

| 指标 | 数值/现象 | 证据路径 |
|---|---|---|
| QPS | | |
| P50/P95/P99 | | |
| 非 2xx | | |
| PostgreSQL connections/waits | | |
| Kafka lag | | |
| Go CPU/RSS | | |
| 磁盘/MinIO | | |

## 排查

1. 先观察到什么；
2. 用什么命令排除什么；
3. 根因证据是什么；
4. 修改或配置调整是什么；
5. 同参数复测结果是什么。

## 当前结论

> 只写本次环境和样本能证明的结论，不外推生产容量。

## 未解决限制

-
```

建议把日志和原始 JSON 放在 `.logs/`，不要提交 token、密码、cookies 或完整 DSN。

## 十一、常用命令

| 目的 | 命令 |
|---|---|
| PostgreSQL 活跃查询 | `docker exec vidlens-postgres psql -U vidlens -d vidlens -c "select * from pg_stat_activity where state <> 'idle';"` |
| PostgreSQL 表大小 | `docker exec vidlens-postgres psql -U vidlens -d vidlens -c "select relname,pg_size_pretty(pg_total_relation_size(relid)) from pg_catalog.pg_statio_user_tables order by pg_total_relation_size(relid) desc;"` |
| Redis 内存 | `docker exec vidlens-redis redis-cli info memory` |
| Redis 连接 | `docker exec vidlens-redis redis-cli info clients` |
| Kafka consumer lag | `docker exec vidlens-kafka kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group vidlens-worker` |
| 容器资源 | `docker stats --no-stream` |
| 磁盘 | `df -h` |
| readiness | `curl -fsS http://127.0.0.1:8080/readyz` |
| RAG 对账 | `go run ./cmd/rag-audit --config config.yaml --user-id <id> --task-id <id> --model <model>` |

## 十二、面试口径

可以说：

> 我把压测拆成读接口、普通上传、Redis 分片续传、Kafka lag、限流和故障注入。测试时同时观察 PostgreSQL 等待、Kafka lag、Go/容器资源和错误率，并保存原始结果。由于 pgvector 与业务表在同一 PostgreSQL，我也会说明单库降低了部署复杂度，但数据库故障会同时影响业务查询和向量检索，需要备份恢复、readiness 和可重建投影来治理。

不能在没有真实记录时说：

- “系统支持万人并发”；
- “P99 优化了某个百分比”；
- “Redis 挂了所有功能都能降级”；
- “Kafka 消息绝不丢失”；
- “PostgreSQL 与 Kafka 已经 exactly-once”；
- “pgvector 性能一定优于 Milvus”；
- “MySQL/Milvus 是当前正式运行依赖”。
