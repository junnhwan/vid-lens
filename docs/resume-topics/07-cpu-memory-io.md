# CPU / 内存 / IO 满载排查（对应 VidLens）

> 面试高频问题："线上 CPU 满了 / 内存满了 / IO 打满了怎么办？"
> 本文把这个通用问题**钉到本项目**的真实代码和架构上，给出现象、原因、排查命令、解决方案、一句话话术和追问防守。
> 诚实原则：区分"已做"和"我知道但还没做"。

---

## 0. 先讲方法论（30 秒定调，比背命令值钱）

不管问 CPU 还是内存，先给一个**统一的分层排查框架**：

```
1. 现象定性：突发飙高还是缓慢爬升？是不是偶发抖动？
2. 分层定位：应用进程 → 容器 → 中间件 → 外部依赖
3. 由外到内：先 docker stats 看谁吃资源，再钻进进程 pprof，
            再看 MySQL processlist / redis info 定位中间件。
4. 区分两种"CPU 跟吞吐脱节"的情况（不要混）：
   - **CPU 低（个位数）但接口慢 / QPS 上不去** → 在等（IO / 锁 / 连接池 / 外部 API）。本项目最常见。
   - **CPU 高（接近满）但吞吐不涨** → CPU 被非业务开销吃掉了（GC 回收、goroutine 调度爆炸、锁自旋、加解密），不是被业务计算有效利用。
```

> 关键认知：**"在等外部网络 API"不会让 CPU 到 100%**。Go 里阻塞在网络读上时 goroutine 会被 netpoller（epoll）挂起、释放 OS 线程，基本不耗 CPU。所以"在等"的真实信号恰恰是 **CPU 很低 + 接口很慢**。CPU 真飙满却没产出，要怀疑 GC / 调度 / 自旋 / 加解密，而不是"在等"。
> （旁注：Linux `load average` 会把磁盘 IO 卡住的 D 态进程算进去，所以磁盘瓶颈会出现 "load 飙但 CPU 低"；网络 IO 等待是可中断 S 态，不算进 load，也不烧 CPU。两者别混。）

本项目的三层资源热源对照：

| 资源 | 本项目最可能的热源 | 关键代码 |
|------|--------------------|----------|
| CPU | FFmpeg 子进程（音频提取+300s 切片）、并发 ASR 的 base64 编码 | `internal/pkg/ffmpeg/ffmpeg.go`、`internal/ai/mimo.go:174` |
| 内存 | ASR 把整段音频读进内存 + base64 膨胀 33% | `internal/ai/mimo.go:164`、`internal/ai/siliconflow.go:94` |
| 磁盘 IO | FFmpeg 中间音频/切片、yt-dlp 下载、上传临时文件 | `internal/mq/consumer.go`、`internal/service/media.go:244` |
| 网络 IO | ASR/LLM/Embedding 外部 API、Milvus、MinIO | `mimo.go:45`（5 分钟超时）、`internal/vector/milvus.go` |

---

## 1. CPU 满了

### 1.1 本项目真实热源

1. **FFmpeg 子进程是头号 CPU 消费者**
   每个转写任务都会 spawn 一个 ffmpeg 进程做音频提取 + 切片（`-ac 1 -ar 16000 -acodec libmp3lame -b:a 32k`）。
   长视频会切出多段（15 分钟≈3 段），转码是 CPU 密集型。
   如果多个 Kafka 任务同时被消费，等于同时跑多个 ffmpeg。

2. **ASR 的 base64 编码**：`mimo.go:174` `base64.StdEncoding.EncodeToString` 是纯 CPU 运算，文件越大越烧。

3. **AES-GCM 加解密**（用户 API Key，`internal/pkg/secret/crypto.go`）：CPU 密集，但量小、调用频次低，一般不是瓶颈。

4. **goroutine 规模**：4 个 consumer（analyze/transcribe/download/rag-index，`main.go:213-216`）+ retry 调度器（每 30 秒扫一次）+ 每个长任务的 watchdog 续期协程。单机高并发时 goroutine 数会涨。

### 1.2 排查命令

```bash
# 第一层：谁在吃 CPU（容器维度）
docker stats --no-stream

# 第二层：钻进 Go 进程
# Go 标准库 runtime/pprof 可用；net/http/pprof 当前未挂到 Gin 路由（演进项）
# 线下/压测时可临时挂载：
#   import _ "net/http/pprof"; go http.ListenAndServe(":6060", nil)
#   go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30   # CPU
#   go tool pprof http://localhost:6060/debug/pprof/goroutine            # goroutine 数

# 第三层：MySQL 是不是在烧 CPU（慢查询/大扫描）
docker exec vidlens-mysql mysql -uroot -proot -e "SHOW FULL PROCESSLIST"
# 看 Sending data / 时间 > 1s 的行
```

### 1.3 解决方案

| 手段 | 状态 | 说明 |
|------|------|------|
| FFmpeg 低码率 ASR 友好参数（16k/单声道/32k） | ✅ 已做 | 大幅降低转码 CPU 和音频体积 |
| ASR 300 秒分片，天然限制单次峰值 | ✅ 已做 | 见记录 001 |
| 限制同时处理的转写任务数（worker 限流/信号量） | ⏳ 演进 | 当前 consumer 无并发上限，多个任务会同时 spawn ffmpeg |
| 视频处理节点与 API 节点分离部署 | ⏳ 演进 | 让 ffmpeg 不抢 Web 请求的 CPU |
| 接入 net/http/pprof 持续 profiling | ⏳ 演进 | 当前未暴露 |

### 1.4 一句话话术

> "这个项目里 CPU 最吃的是 FFmpeg 子进程——每个转写任务都会起一个 ffmpeg 做音频提取和 300 秒切片。我已经用低码率参数（16k 单声道 32kbps）把单次转码 CPU 压下来了；排查时我会先 docker stats 看是不是 ffmpeg 在烧，再用 Go pprof 看应用侧是不是 base64 或加解密。下一步我想做的是给 consumer 加并发上限，避免多个长视频同时转码把 CPU 打满。"

---

## 2. 内存满了（OOM）

### 2.1 本项目真实热源

1. **ASR base64 是最经典的内存热点**
   `mimo.go:164` `os.ReadFile(audioPath)` 把**整段音频读进内存**，再 `base64` 编码（膨胀约 33%），还有 10MB 上限校验（`mimo.go:55`）。
   一段音频 ≈ 文件大小 + base64 副本，瞬时占用是文件体积的 2~3 倍。
   多个任务并发 ASR 时，内存会叠加。

2. **上传大文件**：`max_file_size: 2GB`（`config.yaml:69`）。
   **但这里做得对**：`copyStreamToTempAndHash`（`media.go:243`）用 `io.Copy` + `io.TeeReader` 边写临时文件边算 MD5，**不会把 2GB 全量读进内存**。分片合并走 MinIO 服务端 `ComposeObject`（`minio.go:106`），也不进应用内存。这是可以主动展示的好设计。

3. **转写/总结大文本**：垂直分表（transcription/summary 独立建表）避免大文本污染任务主表，查询任务列表不会把大文本捞进内存。✅

4. **聊天上下文**：Redis 只缓存最近 N 轮（`recent_turns: 8`），MySQL 存完整记录，避免 prompt 无限膨胀。✅

### 2.2 排查命令

```bash
# Go 进程内存
docker stats --no-stream | grep vidlens

# Heap 详情（需先挂载 pprof）
go tool pprof http://localhost:6060/debug/pprof/heap
#   (pprof) top          —— 占用最大的对象
#   (pprof) list <func>  —— 看具体函数分配

# Redis 内存
docker exec vidlens-redis redis-cli info memory | grep used_memory_human
```

### 2.3 解决方案

| 手段 | 状态 | 说明 |
|------|------|------|
| ASR 300 秒分片，限制单次音频大小 | ✅ 已做 | 10MB 上限 + 分片，单次峰值可控 |
| 上传流式落盘 + TeeReader 哈希 | ✅ 已做 | 2GB 文件不进内存 |
| 分片合并走 MinIO 服务端 Compose | ✅ 已做 | 不在应用内存拼接 |
| 限制并发 ASR 数量 | ⏳ 演进 | 多任务并发时 base64 副本叠加 |
| 流式/分块上传 base64 给 ASR | ⏳ 演进 | 当前一次性编码 |

### 2.4 一句话话术

> "内存热点我印象最深的是 ASR：MiMo 的语音转写要把整段音频读进内存再 base64 编码，体积膨胀三分之一，多个任务并发时内存会叠加。我的处理是按 300 秒切片，每段单独转写，把单次内存峰值压住。反面上传这里我做得比较好——2GB 上限的文件我用了 io.Copy 加 TeeReader 流式写临时文件边算 MD5，分片合并也走 MinIO 服务端 Compose，所以大文件不会全量进内存。"

---

## 3. IO 满了（磁盘 + 网络）

### 3.1 磁盘 IO 热源

1. **FFmpeg 写中间音频 + 切片文件**：每个任务临时产物（提取的 mp3 + N 个切片）。
2. **yt-dlp 下载到本地临时文件**（`consumer.go` 下载链路）。
3. **上传临时文件**：`media.go:244 os.CreateTemp("vidlens_upload_*")`。
4. **分片上传逐片落盘 MinIO**。

风险：临时文件如果不清理会吃满磁盘（服务器部署 README 也提到要补磁盘监控和日志轮转）。

### 3.2 网络 IO 热源

1. **外部 AI API**：ASR/LLM/Embedding，超时设 5 分钟（`mimo.go:45`）。慢响应会占住 HTTP 连接和 goroutine。
2. **Milvus 向量写入/检索**（`internal/vector/milvus.go`）：索引构建批量写入。
3. **MinIO 上传/下载**：预签名 URL 下载走客户端，但服务端下载（yt-dlp 后上传、FGGetObject）走服务端网络。

### 3.3 排查命令

```bash
# 磁盘
df -h                       # 磁盘空间
iostat -x 1                 # 磁盘 IO 等待（%util、await）

# MySQL 慢查询（磁盘 IO 常来自全表扫描/大结果集）
docker exec vidlens-mysql mysql -uroot -proot \
  -e "SELECT * FROM information_schema.PROCESSLIST WHERE TIME > 1;"

# Kafka 消费积压（消费者处理不过来，本质是下游 IO/计算瓶颈）
docker exec vidlens-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 --describe --group vidlens-worker
# 看 LAG 列
```

### 3.4 解决方案

| 手段 | 状态 | 说明 |
|------|------|------|
| URL 下载改 Kafka 异步（不阻塞 HTTP） | ✅ 已做 | 见记录 008 |
| 下载限制 720p，降低网络/磁盘量 | ✅ 已做 | 见记录 007 |
| AI 调用分级超时 + 错误分类重试 | ✅ 已做 | 见记录 009 |
| 临时文件清理 + 磁盘监控 | ⏳ 演进 | README/troubleshooting 已列为待办 |
| 分片上传/下载做并发控制 | ⏳ 演进 | 当前分片可并发上传 |

### 3.5 一句话话术

> "磁盘 IO 主要来自 FFmpeg 的中间音频、yt-dlp 下载的临时文件和上传临时文件；网络 IO 主要卡在外部 ASR/LLM API 和 Milvus。我重点做了三件事：把 URL 下载从同步 HTTP 改成 Kafka 异步任务，避免慢下载阻塞接口；下载限制 720p 减少网络和磁盘量；外部 API 错误分类，超时和 5xx 才重试。我还知道临时文件清理和磁盘监控是下一步要补的。"

---

## 4. 连接资源（连接池）—— 高价值诚实点

这块值得单独讲，因为它是"我知道但还没做"的典型，能同时展示**压测经验**和**诚实**：

```go
// cmd/server/main.go:59 —— 当前用默认配置，没调池
db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
// cmd/server/main.go:69
rdb := redis.NewClient(&redis.Options{...})  // 默认 PoolSize = 10×GOMAXPROCS
```

### 面试怎么讲

> "我压测读接口（登录+任务列表）时遇到过一个现象：**QPS 上不去，但 CPU 利用率很低**。这说明瓶颈不在计算，而在等待——最常见的就是数据库连接池。我看了一下，GORM 当前用的是默认配置，没设 `SetMaxOpenConns/SetMaxIdleConns`，高并发时连接打满，请求都排队等连接。Redis 用的也是默认池。这是我压测发现的、明确知道怎么改但当前 demo 还没落的优化点。"

修复方向（被追问"怎么改"时答）：
- MySQL：`SetMaxOpenConns`（按 DB 规格给，如 50~100）、`SetMaxIdleConns`（复用连接降抖动）、`SetConnMaxLifetime`（避免长连接被 DB 端踢）。
- Redis：调 `PoolSize`、`MinIdleConns`，监控 `redis-cli info clients | grep connected_clients`。

---

## 5. 综合排查工具速查表（背这张）

| 现象 | 第一步命令 | 本项目大概率原因 |
|------|-----------|------------------|
| CPU 100% | `docker stats` → Go `pprof profile` | ffmpeg 并发转码 / base64 / 加解密 |
| CPU 高但吞吐不涨 | Go `pprof profile` / 看 goroutine 数 | GC（大对象 base64）/ 调度爆炸 / 锁自旋 / ffmpeg 并发 |
| CPU 低但接口慢/QPS 低 | `SHOW PROCESSLIST` / 看 goroutine | 等连接池 / 等外部 ASR/LLM API / 等锁（这才是"在等"） |
| 内存涨 / OOM | `pprof heap` → top | ASR base64 整文件读内存 / 并发叠加 |
| goroutine 泄漏 | `pprof goroutine` | chan/锁阻塞、watchdog/context 未释放 |
| 磁盘打满 | `df -h` / `iostat -x` | 临时文件没清理 / 视频文件堆积 |
| 接口慢 | `SHOW PROCESSLIST`、hey 测 P99 | MySQL 慢查询 / 深翻页 OFFSET |
| 消息积压 | `kafka-consumer-groups --describe` 看 LAG | 下游 ASR/LLM 太慢 |

---

## 6. 追问防守（高频追问）

**Q：CPU 100% 怎么第一时间定位？**
A：先看是哪个进程（top/docker stats），再用 `pprof` 的 CPU profile 看 top/cum，定位到函数。如果 CPU 高但吞吐不涨，转向查锁、连接、外部 IO 等待，而不是继续抠计算。

**Q：怎么判断是 CPU 瓶颈还是 IO 瓶颈？**
A：看 **CPU% 和吞吐的关系**，两条分开：
- **CPU 很低但 QPS 上不去 / 接口慢** → 在等（IO / 锁 / 连接池 / 外部 API）。goroutine 被 netpoller 挂起不耗 CPU。本项目典型就是等外部 ASR/LLM API 或等连接池。
- **CPU 接近满但吞吐不涨** → CPU 被浪费在 GC / goroutine 调度 / 锁自旋 / 加解密，不是有效业务计算。要怀疑 base64 大对象导致 GC、ffmpeg 并发转码。

一句话记住："在等"是 **CPU 低**，不是 CPU 满；CPU 满却没产出要找浪费 CPU 的东西。

**Q：OOM 怎么排查？**
A：heap pprof 看 top 占用，找大对象。本项目我会先怀疑 ASR 的 base64（整文件读内存）。确认后做分片/限流并发，而不是简单加内存。

**Q：goroutine 泄漏怎么发现和防？**
A：`pprof goroutine` 看数量和阻塞点（chan receive / 锁）。本项目用 context 超时 + channel 关闭做防护，watchdog 在 unlock/进程退出时停止续期。

**Q：你们做了什么来"防止"这些？**
A：限流（Redis Lua 令牌桶）、异步化（Kafka 削峰）、分片（ASR 300 秒）、流式处理（上传 TeeReader）、超时控制（context + API 超时）、错误分类重试。这些都是把"不可控外部能力"包进"可控后端流程"的思路。

**Q：CPU/内存/IO 三个一起飙怎么办？**
A：先保命——降级非核心功能（如 Milvus 挂了只降级 RAG，其他功能不受影响，见记录 005），限流挡住入口流量；再分层定位是哪个中间件或外部 API 拖垮的；最后按瓶颈逐个修。

---

## 7. 不要夸大的点

- 当前是 demo/单机部署，不是生产级多副本高可用。
- consumer 当前没有显式并发上限，"限制并发任务数"是演进项不是已完成。
- pprof HTTP 端点当前未挂载到 Gin 路由。
- MySQL/Redis 连接池当前用默认值，调优是压测发现、待落地。
- 没有接入 Prometheus/OTel 指标系统，排查靠日志 + processlist + pprof（线下）。
