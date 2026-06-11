# VidLens 压测与故障演练指南

> 通过压测验证系统瓶颈，通过故障注入练习排查能力。
> 每个场景后记录结果，形成面试谈资。

---

## 一、环境准备

### 1.1 安装压测工具

```bash
# hey（推荐，Go 单文件，Linux/Windows/macOS 都有）
# Linux：
wget https://hey-release.s3.us-east-2.amazonaws.com/hey_linux_amd64 -O hey
chmod +x hey

# Windows PowerShell（本机压服务器）：
# 从 https://github.com/rakyll/hey/releases 下载 hey_windows_amd64 放到 PATH 里
```

### 1.2 准备测试数据

```bash
# 以下变量按你的实际环境修改
BASE="http://127.0.0.1:8080"
TOKEN="你的JWT_TOKEN"

# 准备一个小测试视频（几 MB 就够）
dd if=/dev/urandom of=/tmp/test-video.mp4 bs=1M count=5

# 确认服务健康
curl -s "$BASE/health" | python3 -m json.tool
```

### 1.3 多终端观察

压测时开 4~5 个终端窗口同步观察，养成习惯：

```bash
# 终端 2: Go 服务日志
docker logs -f <go容器名> --tail 100

# 终端 3: MySQL 状态（每秒刷新）
watch -n 1 'docker exec vidlens-mysql mysql -uroot -proot -e "
  SHOW STATUS LIKE \"Threads_connected\";
  SHOW STATUS LIKE \"Queries\";
  SHOW PROCESSLIST;
" 2>/dev/null'

# 终端 4: Redis 状态
watch -n 1 'docker exec vidlens-redis redis-cli info clients 2>/dev/null | grep connected_clients'

# 终端 5: 容器资源占用
watch -n 1 'docker stats --no-stream'
```

---

## 二、场景 1 — 读接口基线（登录 + 任务列表）

**目标：** 测 Go → MySQL 纯读链路的 QPS 天花板，作为后续所有场景的对比基准。

### 步骤

```bash
# 1. 先登录拿 token
TOKEN=$(curl -s -X POST "$BASE/api/v1/user/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"testuser","password":"testpass"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])")

# 2. 低并发热身
./hey -n 100 -c 10 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/list?page=1&page_size=20"

# 3. 正式压测：200 并发，持续 10 秒
./hey -n 2000 -c 200 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/list?page=1&page_size=20"

# 4. 翻页深水区：page=100 时可能触发慢查询
./hey -n 500 -c 50 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/list?page=100&page_size=20"
```

### 观察指标

| 指标 | 获取方式 | 说明 |
|------|---------|------|
| QPS | hey 输出 `Requests/sec` | 纯读链路的天花板 |
| P50/P99 延迟 | hey 输出 latency 分布 | P99 > 500ms 需关注 |
| MySQL 连接数 | 终端 3 观察 `Threads_connected` | 接近上限说明连接池要调 |
| 慢查询 | `docker exec vidlens-mysql mysql -uroot -proot -e "SHOW FULL PROCESSLIST"` | 有 Sending data 超过 1s 的 |

### 常见瓶颈与修复

| 现象 | 可能原因 | 修复方向 |
|------|---------|---------|
| QPS 上不去但 CPU 低 | MySQL 连接池太小 | 调大 `SetMaxOpenConns` |
| page 越大延迟越高 | `OFFSET` 深翻页慢 | 加 `(user_id, created_at)` 联合索引，或改游标翻页 |
| P99 抖动大 | 连接建立开销 | 开启 `SetMaxIdleConns` 复用连接 |

---

## 三、场景 2 — 并发上传同文件（MD5 去重 + 分布式锁）

**目标：** 多个请求同时上传相同 MD5 的文件，验证分布式锁 + 幂等校验是否真的能防止重复入库。

### 步骤

```bash
# 20 个并发上传同一个文件
for i in $(seq 1 20); do
  curl -s -o /dev/null -w "%{http_code}\n" \
    -X POST "$BASE/api/v1/media/upload" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@/tmp/test-video.mp4" &
done
wait
```

### 验证

```bash
# 查看 asset 表：同 MD5 应该只有一条记录
docker exec vidlens-mysql mysql -uroot -proot vidlens \
  -e "SELECT id, file_md5, file_size, created_at FROM video_assets ORDER BY id DESC LIMIT 25;"

# 查看 task 表：同 MD5 应该只有一个 task
docker exec vidlens-mysql mysql -uroot -proot vidlens \
  -e "SELECT id, user_id, file_md5, status, created_at FROM video_tasks ORDER BY id DESC LIMIT 25;"
```

### 判断标准

| 结果 | 含义 |
|------|------|
| 1 条 asset + 1 条 task，其余返回秒传 | ✅ MD5 去重 + 分布式锁完全生效 |
| 1 条 asset + 多条 task（状态秒传） | ✅ asset 去重生效，task 幂等可以接受 |
| 多条 asset 同 MD5 | ❌ 分布式锁有漏洞，查 `redis_lock.go` 日志 |

### 进阶：50 并发更暴力

```bash
for i in $(seq 1 50); do
  curl -s -o /dev/null -w "%{http_code} " \
    -X POST "$BASE/api/v1/media/upload" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@/tmp/test-video.mp4" &
done
wait
echo ""
```

---

## 四、场景 3 — 分片上传并发（Redis 记账 + MinIO 存储）

**目标：** 验证 10 个分片并发上传时 Redis Set 的原子性、MinIO 写入的稳定性。

### 步骤

```bash
#!/bin/bash
# stress-chunk-upload.sh
FILE_MD5="stress-test-$(date +%s)"
TOTAL=10

echo "=== 1. 并发上传 $TOTAL 个分片 ==="
for i in $(seq 0 $((TOTAL-1))); do
  dd if=/dev/urandom bs=1M count=5 2>/dev/null | \
  curl -s -o /dev/null -w "chunk$i: %{http_code}\n" \
    -X POST "$BASE/api/v1/media/upload-chunk" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file_md5=$FILE_MD5" \
    -F "chunk_number=$i" \
    -F "chunk=@-;type=application/octet-stream" &
done
wait

echo ""
echo "=== 2. 检查已上传分片 ==="
curl -s -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/check-upload?file_md5=$FILE_MD5" | python3 -m json.tool

echo ""
echo "=== 3. 合并 ==="
curl -s -X POST "$BASE/api/v1/media/merge-chunks" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"file_md5\":\"$FILE_MD5\",\"filename\":\"stress-chunk-test.mp4\",\"total_chunks\":$TOTAL}" \
  | python3 -m json.tool

echo ""
echo "=== 4. Redis 分片记录验证 ==="
docker exec vidlens-redis redis-cli GET "upload:$FILE_MD5:total"
docker exec vidlens-redis redis-cli SMEMBERS "upload:$FILE_MD5:chunks"
```

### 判断标准

| 结果 | 含义 |
|------|------|
| `check-upload` 返回 10 个分片 | ✅ Redis Set 原子写入无丢失 |
| 返回 < 10 个分片 | ❌ 并发 Redis 写入冲突，排查连接池 |
| 合并成功，MinIO 有文件 | ✅ 分片上传完整流程正确 |
| 合并失败 | 查日志，是否某个分片写入 MinIO 超时 |

### 进阶：重复上传同一分片号

```bash
# 两个请求同时传 chunk_number=3，验证幂等性
for i in 1 2 3 4 5 6; do
  dd if=/dev/urandom bs=1M count=5 2>/dev/null | \
  curl -s -o /dev/null -w "%{http_code} " \
    -X POST "$BASE/api/v1/media/upload-chunk" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file_md5=duplicate-test" \
    -F "chunk_number=3" \
    -F "chunk=@-;type=application/octet-stream" &
done
wait
echo ""

# 应该全部返回 200（覆盖写），且 Redis Set 里 chunk 3 只有一条
docker exec vidlens-redis redis-cli SMEMBERS "upload:duplicate-test:chunks"
```

---

## 五、场景 4 — Kafka 消息堆积与消费能力

**目标：** 快速灌大量消息，测消费者的吞吐量和 Kafka lag 恢复速度。

### 前置：批量造测试任务

```bash
# 上传 20 个小文件（或直接数据库批量插入 task 记录）
for i in $(seq 1 20); do
  dd if=/dev/urandom of=/tmp/test-$i.mp4 bs=1K count=100 2>/dev/null
  curl -s -X POST "$BASE/api/v1/media/upload" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@/tmp/test-$i.mp4" | python3 -c "import sys,json; print(f'task-{i}: {json.load(sys.stdin)}')"
done

# 记录所有 task ID
```

### 步骤 1：批量触发分析，瞬间灌入消息

```bash
# 假设 task ID 从 1 到 20
for task_id in $(seq 1 20); do
  curl -s -X POST "$BASE/api/v1/media/analyze/$task_id" \
    -H "Authorization: Bearer $TOKEN" &
done
wait
echo "全部投递完成，开始观察消费速度"
```

### 步骤 2：实时观察 Kafka Lag

```bash
# 方法 A：Kafka UI（更直观）
# 浏览器打开 http://服务器IP:8180 → Topics → video-analyze → Messages

# 方法 B：命令行
docker exec vidlens-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --describe --group vidlens-worker

# 每秒刷一次
watch -n 1 'docker exec vidlens-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --describe --group vidlens-worker 2>/dev/null'
```

### 观察指标

| 指标 | 说明 |
|------|------|
| LAG 列 | 当前积压的消息数，应逐渐下降 |
| 消费速度 | LAG 从 20 → 0 花了多少秒？算出 msg/s |
| CURRENT-OFFSET | 已提交的 offset，确认在推进 |
| 消费者日志 | `docker logs <go容器> \| grep "任务完成"` |

### 瓶颈分析

| 现象 | 可能原因 | 排查方向 |
|------|---------|---------|
| LAG 一直涨不降 | 消费者处理太慢（ASR/LLM 外部 API） | 看 Go 日志卡在哪一步 |
| LAG 降了但有跳变 | 某些消息消费失败跳过 | 搜索 Go 日志里的 `[Kafka]` ERROR |
| 消费者完全不消费 | 消费者组 rebalance 卡住 | 重启 Go 服务观察 |

---

## 六、场景 5 — 令牌桶限流验证

**目标：** 确认 Redis Lua 令牌桶在高压下正确拒绝超量请求。

### 步骤

```bash
# 找一个有限流的接口，比如 RAG 问答
# 需要先有一个 session（通过前端创建或手动调接口）
SESSION_ID=1

# 50 并发打 200 个请求，配置 capacity=10, rate=10
./hey -n 200 -c 50 \
  -m POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"这个视频讲了什么"}' \
  "$BASE/api/v1/chat/sessions/$SESSION_ID/messages"
```

### 判断标准

```
hey 输出的 Status code distribution:
  [200]  XX responses    ← 通过的
  [429]  XX responses    ← 被限流的
```

| 结果 | 含义 |
|------|------|
| 200 和 429 都有，比例合理 | ✅ 令牌桶生效 |
| 全是 200 | ❌ 中间件没挂到这个路由，或 key 设计有误 |
| 全是 429 | ❌ capacity/rate 参数太严或 Redis 连接异常 |

### 进阶：验证令牌恢复

```bash
# 第一轮打满令牌（预期大量 429）
./hey -n 50 -c 50 -m POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"test"}' \
  "$BASE/api/v1/chat/sessions/$SESSION_ID/messages"

# 等 3 秒让令牌恢复
sleep 3

# 第二轮应该恢复部分通过
./hey -n 10 -c 5 -m POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"test"}' \
  "$BASE/api/v1/chat/sessions/$SESSION_ID/messages"
```

---

## 七、场景 6 — 任务轮询压力

**目标：** 前端轮询任务状态是高频读操作，测 MySQL 在大量轮询下的表现。

### 步骤

```bash
# 模拟 10 个用户同时轮询各自的任务详情
# 假设有 task ID 1-10，各自对应的用户 token

# 单用户高频轮询（每秒查一次，持续 60 秒）
for i in $(seq 1 60); do
  curl -s -H "Authorization: Bearer $TOKEN" \
    "$BASE/api/v1/media/task/1" -o /dev/null -w "%{http_code} %{time_total}s\n"
  sleep 1
done

# 多用户并发轮询
./hey -n 3000 -c 100 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/task/1"
```

### 优化方向

如果 QPS 上不去：
1. 检查 `task` 表 `id` 是否有主键索引（GORM 默认有）
2. 考虑给热数据加 Redis 缓存，减少 MySQL 压力
3. 轮询改 WebSocket 推送（非必须，但面试加分）

---

## 八、场景 7 — 故障注入（混沌测试）

> **最有价值的场景。** 每次只杀一个组件，观察系统的反应，练排查能力。

### 8a. Kafka 宕机

```bash
# 1. 停掉 Kafka
docker stop vidlens-kafka

# 2. 尝试上传并触发分析
curl -s -X POST "$BASE/api/v1/media/analyze/1" \
  -H "Authorization: Bearer $TOKEN"

# 3. 观察
docker logs -f <go容器名> --tail 50

# 4. 恢复 Kafka
docker start vidlens-kafka

# 5. 再次尝试分析
curl -s -X POST "$BASE/api/v1/media/analyze/1" \
  -H "Authorization: Bearer $TOKEN"
```

**记录：**

| 问题 | 你的观察 |
|------|---------|
| 用户请求返回什么状态码？ | |
| 消息丢了还是保留了？ | |
| Kafka 恢复后消费者自动重连了吗？ | |
| 重连后积压消息能继续消费吗？ | |

**思考：** 如果是生产环境，你会加什么保护？
- 生产者重试 + 本地缓冲？
- 降级提示"系统繁忙稍后重试"？
- 告警通知？

---

### 8b. Redis 宕机

```bash
docker stop vidlens-redis

# 测试不同接口
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/media/list" -w "\nlist: %{http_code}\n"
curl -s -X POST "$BASE/api/v1/media/upload-chunk" \
  -H "Authorization: Bearer $TOKEN" \
  -F "file_md5=test" -F "chunk_number=0" -F "chunk=@/tmp/test-video.mp4" \
  -w "\nchunk: %{http_code}\n"
curl -s -X POST "$BASE/api/v1/chat/sessions/1/messages" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"test"}' -w "\nchat: %{http_code}\n"

docker start vidlens-redis
```

**记录：**

| 问题 | 你的观察 |
|------|---------|
| 任务列表还能查吗？（Redis 没参与） | |
| 分片上传还能用吗？（依赖 Redis） | |
| 限流器行为：放行还是拒绝？ | 代码里 `err != nil` 时 `return true`，预期放行 |
| 聊天上下文缓存丢了，问答还能用吗？ | |
| Redis 恢复后需要重启 Go 服务吗？ | |

**思考：**
- 限流器异常时放行是正确策略吗？什么场景下应该拒绝？
- 分片上传能不能加本地内存 fallback？

---

### 8c. MySQL 宕机

```bash
docker stop vidlens-mysql

# 测试
curl -s -X POST "$BASE/api/v1/user/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test"}' -w "\nlogin: %{http_code}\n"

curl -s "$BASE/health" -w "\nhealth: %{http_code}\n"

# 等 30 秒后恢复
sleep 30
docker start vidlens-mysql

# MySQL 恢复后测试
sleep 5
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/media/list" -w "\nlist: %{http_code}\n"
```

**记录：**

| 问题 | 你的观察 |
|------|---------|
| Go 服务直接 panic 了还是返回 500 继续跑？ | |
| health 接口受影响吗？（不依赖 MySQL） | |
| MySQL 恢复后 GORM 连接能自动重建吗？需要重启吗？ | |
| 期间 Kafka 消费者还在跑吗？任务会怎么流转？ | |

**思考：**
- 如果 GORM 连接不能自动重建，是不是需要加健康检查 + 自动重启？
- 消费者在 MySQL 不可用时，offset 没提交 → 恢复后会重试，幂等够不够？

---

### 8d. Milvus 宕机

```bash
docker stop vidlens-milvus

# 测试 RAG 问答
curl -s -X POST "$BASE/api/v1/chat/sessions/1/messages" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"视频讲了什么"}' -w "\nchat: %{http_code}\n"

# 测试 RAG 索引构建
curl -s -X POST "$BASE/api/v1/media/task/1/rag-index" \
  -H "Authorization: Bearer $TOKEN" -w "\nindex: %{http_code}\n"

# 测试普通功能（不应受影响）
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/media/list" -w "\nlist: %{http_code}\n"

docker start vidlens-milvus
```

**记录：**

| 问题 | 你的观察 |
|------|---------|
| 向量检索失败时 RAG 问答直接报错还是能降级？ | |
| 普通功能（上传、列表）是否正常？ | |
| Milvus 恢复后需要重建 collection 吗？ | |

**思考：**
- 向量检索失败时，能不能只走关键词检索？（你的 `retrieval_fusion.go` 已经支持 hybrid）
- 这就是面试常问的"熔断降级"——向量化失败时降级为纯关键词召回

---

### 8e. 网络分区模拟（进阶）

```bash
# 模拟 Go 服务和 MySQL 之间网络延迟
# 需要 tc（Linux 流量控制）
docker exec <go容器名> sh -c "
  tc qdisc add dev eth0 root netem delay 500ms 200ms
"

# 观察接口延迟变化
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/media/list" -w "\n%{time_total}s\n"

# 恢复
docker exec <go容器名> sh -c "tc qdisc del dev eth0 root"
```

---

## 九、场景 8 — 综合压测（所有组件联动）

**目标：** 同时跑读、写、分析，模拟真实使用场景，找出整体瓶颈。

```bash
#!/bin/bash
# stress-full.sh — 综合压测脚本

BASE="http://127.0.0.1:8080"
TOKEN="你的JWT_TOKEN"

echo "=== 综合压测开始 $(date) ==="

# 1. 后台持续轮询任务列表（模拟用户看板）
./hey -n 5000 -c 100 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/list?page=1&page_size=20" &
PID_LIST=$!

# 2. 后台持续轮询任务详情（模拟前端进度条）
./hey -n 3000 -c 50 \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/media/task/1" &
PID_DETAIL=$!

# 3. 并发上传（模拟用户上传）
for i in $(seq 1 10); do
  curl -s -o /dev/null -X POST "$BASE/api/v1/media/upload" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@/tmp/test-video.mp4" &
done

# 等读接口压测结束
wait $PID_LIST
wait $PID_DETAIL

echo ""
echo "=== 综合压测结束 $(date) ==="
echo "查看容器资源："
docker stats --no-stream
```

---

## 十、记录模板

每个场景跑完后，用下面的模板记录结果，保存为 `stress-test-results.md`。

```markdown
## 压测记录：<场景名>

- **日期：** 2025-xx-xx
- **环境：** xC xG 服务器 / Docker 部署
- **参数：** 并发数 xx，总请求数 xxx

### 结果

| 指标 | 数值 |
|------|------|
| QPS | |
| P50 延迟 | |
| P99 延迟 | |
| 错误率 | |

### 瓶颈

- 描述你观察到的瓶颈现象

### 排查过程

1. 第一步看了什么 → 发现了什么
2. 第二步做了什么 → 结论是什么

### 修复

- 修改了什么 / 调了什么参数
- 修复后 QPS / P99 变成多少

### 面试总结（一句话）

> "我做过 xxx 压测，发现 xxx 是瓶颈，通过 xxx 手段把 QPS 从 xx 提升到 xx / P99 从 xx 降到 xx。"
```

---

## 附：常见问题速查

| 问题 | 命令 |
|------|------|
| MySQL 慢查询 | `docker exec vidlens-mysql mysql -uroot -proot -e "SELECT * FROM information_schema.PROCESSLIST WHERE TIME > 1;"` |
| Redis 内存 | `docker exec vidlens-redis redis-cli info memory \| grep used_memory_human` |
| Redis 连接数 | `docker exec vidlens-redis redis-cli info clients \| grep connected_clients` |
| Kafka Topic 消息量 | `docker exec vidlens-kafka kafka-run-class kafka.tools.GetOffsetShell --broker-list localhost:9092 --topic video-analyze` |
| 磁盘空间 | `df -h` |
| Go 进程内存 | `docker stats --no-stream \| grep vidlens` |
| 容器日志搜索错误 | `docker logs <容器名> 2>&1 \| grep -i "error\|panic\|fatal" \| tail -20` |
| 查看 Kafka 消费者组 | `docker exec vidlens-kafka kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group vidlens-worker` |
