# 面试题库（简历六点 · 自测用）

每点按「概念 / 设计 / 对比选型 / 实现方式 / 边界兜底 / 系统设计」出题，**答点提示基于真实代码**（不是通用模板）。建议先盖住提示自测，卡壳的标出来重点复盘。标 ⚠️ 的是"代码真相 ≠ 简历话术"的高危 landmine。

---

## 第 1 点：Kafka 异步化

### 概念
- **为什么要异步化？同步处理会怎样？** → 视频处理分钟级，同步会阻塞 HTTP 请求线程、占连接、超时；异步让接口只落库+投递，立即返回。
- **at-least-once 是什么？和 exactly-once 区别？** → 至少一次，可能重复投递；exactly-once 需要事务/幂等保证，成本高。这里用 at-least-once + 业务幂等。
- **消费者组（Consumer Group）怎么实现负载均衡？** → 同组多消费者分摊分区，一个分区只被组内一个消费者消费。
- **为什么用 MD5 做 message key？** → 同一视频 MD5 相同 → 进同一分区 → 同一消费者串行处理，避免同视频被并发处理（consumer.go:42 注释，配合锁双保险）。

### 设计
- **HTTP 接口和消费者的职责怎么划分？** → 接口只做校验+建 task(Pending)+EnqueueXxx 投递+返回；下载/ASR/摘要/索引都在消费者里。
- **4 个 topic（analyze/transcribe/download/rag_index）为什么分开？** → 不同任务类型、不同耗时、独立失败重试、独立并发控制。
- **手动提交 offset（CommitInterval:0）为什么？** → 业务成功才 commit，失败不 commit 由 Kafka 重投，保证不丢消息。
- **为什么 analyze 用分布式锁，transcribe/download/rag_index 不用？** → analyze 用 `vidlens:lock:<md5>` 锁防同视频并发；其余靠 `UpdateStatusAndStageIf` 状态机 CAS 做幂等，不抢锁。

### 对比选型
- **Kafka vs RocketMQ/RabbitMQ？** → Kafka 高吞吐、分区有序、消费组；RocketMQ 自带事务消息/DLQ（参考项目 other-grill 用的）。这里手写 DB 重试替代 DLQ。
- **手动提交 vs 自动提交 offset？** → 自动提交可能"已提交但业务没成功"丢消息；手动提交可控但要自己处理重投幂等。
- **业务失败靠 Kafka 重投 vs DB 重试调度？** → 这里业务失败 return nil（commit，不重投）→ 靠 RetryScheduler 兜底；只有基础设施失败（解析/DB）才 return err 不 commit 走重投。⚠️ 见边界题。

### 实现方式
- **handleAnalyze 的六步流程？** → 解析消息→MD5 抢锁→幂等校验(状态)→CAS 改 Running→processVideo→改 Completed。consumer.go:393。
- **processVideo 的处理链？** → 复用已有转录？否则：下载视频→ffmpeg 提音频→切片 ASR（带分片级续传）→存转录→触发 RAG 索引→LLM 摘要。
- **transcribeAudio 怎么做分片级续传？** → 音频按秒切片，每片 ASR 前查 `transcription_chunk` 表，已完成的片直接复用，失败片可重试。consumer.go:579。
- **消费者用 context.Background() 而不是请求 ctx，有问题吗？** → 没问题，消费者是后台 goroutine，没有 HTTP 请求生命周期，不应随请求结束而取消。

### 边界兜底
- **业务失败（ASR 挂了）消息会重投吗？** ⚠️ → 不会。recordTaskFailure 记 task_job 后 return nil → commit → 不重投，靠 RetryScheduler 兜底。所以"失败可恢复"是 DB 驱动，不是 Kafka 重投/DLQ。
- **基础设施失败（消息解析失败/DB 查询失败）呢？** → return err → 不 commit → Kafka at-least-once 重投。
- **同一条消息被重复消费怎么办？** → 锁（analyze）+ 状态 CAS（其余）保证幂等，重复执行会被状态校验拦下。
- **消费者宕机，正在处理的消息呢？** → 没 commit，Kafka 重投给组内其他消费者。
- **重试到死怎么标记？** → 超过 maxRetries → TaskStatusDead（见第 6 点）。

### 系统设计
- **消息积压怎么处理？** → 增加分区+消费者、检查慢消费、临时扩容。
- **要保证消息顺序怎么做的？** → 用 MD5 做 key，同视频同分区顺序消费。
- **怎么保证"投递了但消费者没跑"不丢？** → task 落库 + 投递失败会回滚状态/记失败；RetryScheduler 扫描 due 任务补投。

---

## 第 2 点：Redis 锁 + WatchDog + MD5 复用

### 概念
- **分布式锁要解决什么？** → 多实例下保护临界区（同视频分析、分片合并），避免重复处理。
- **WatchDog 看门狗是干嘛的？** → 自动给锁续期，防止长耗时业务（视频处理）期间锁过期被别人抢走。
- **MD5 复用（秒传）是什么？** → 同内容只存一份，asset 按 FileMD5 去重，多 task 共享。

### 设计
- **为什么不用 SETNX + 固定过期时间？** → 固定 TTL 在长任务下会过期，导致误放他人进临界区；WatchDog 续期解决。
- **为什么锁要带 owner 标识？** → 释放/续期前要确认"这锁还是我的"，防止误删别人的锁。
- **为什么用 Lua 脚本做释放/续期？** → "判断 owner + 删除/续期"必须原子，否则有竞态。
- **为什么 asset 和 task 拆表？** → asset 是内容级（FileMD5 唯一），task 是用户行为级；多 task 共享一个 asset，删除时引用计数。

### 对比选型
- **你这套锁 vs Redisson？** → Redisson 用 UUID+threadId、可重入、Lua、watchdog 都有；这里是手写简化版。⚠️ 不是 Redisson。
- **MD5 复用 vs 不复用？** → 复用省存储和重复处理；代价是 asset 全局共享无 user 隔离（同 MD5 跨用户复用是 by-design）。
- **SETNX vs SET NX EX？** → 后者一条命令原子设值+过期，更安全；代码用了 SetNX+TTL（go-redis SetNX 带过期参数，等价）。

### 实现方式
- **TryLock 怎么实现的？** → 循环 SetNX(key,value,30s)，成功启 watchdog；失败到 deadline 前每 100ms 重试。redis_lock.go:37。
- **WatchDog 多久续一次？** → ttl/3 = 10s，Lua 校验 owner 后 EXPIRE 重置 30s。redis_lock.go:75。
- **Unlock 安全在哪？** → close stopChan 停 watchdog + Lua（owner 匹配才 DEL）。redis_lock.go:109。
- **MD5 复用在代码哪？** → `FindByMD5` 命中复用 asset；`createAssetFromLocalFile` 并发冲突时重查复用+删自己的对象。media.go:104/229。

### 边界兜底
- **锁持有者宕机怎么办？** → watchdog 也死，锁 30s TTL 自动释放，不死锁。
- **owner 怎么生成？为什么用 UUID 不用时间戳？** → 用 `uuid.New().String()`（redis_lock.go:45）。**原先用 `UnixNano()`，高并发同纳秒撞同一 owner 会让释放脚本误删别人的锁——已修复成 UUID**。面试可讲发现过程。
- **renew 续期失败怎么处理？** → `renew()` 检查 Eval 返回 + 记日志（redis_lock.go:108）。**原先忽略返回值，续期静默失败可能导致锁过期——已修复**。续期失败不退 watchdog（瞬时故障下次续上），并发安全由业务幂等兜底。
- **这个锁可重入吗？能跨 goroutine 共享吗？** ⚠️ → 不能。value/stopChan 在 struct 上是有状态的，每次获取必须 `NewRedisLock` 新建实例。
- **两个用户同时传同内容建 asset？** → FileMD5 唯一索引兜底，一个赢一个撞键→重查复用→删自己对象。

### 系统设计
- **Redlock 了解吗？为什么没用？** → Redlock 多节点多数派，更抗单点；单 Redis 场景用单实例锁够，且本项目容忍偶发失效（有 DB 幂等兜底）。
- **锁和数据库唯一索引谁是最后防线？** → DB 唯一索引是底线，锁是性能优化（挡昂贵操作）。这条在第 3 点合并里也成立。

---

## 第 3 点：分片上传与断点续传

### 概念
- **分片上传 vs 断点续传 vs 秒传？** → 分片=拆开传；续传=补传缺失片；秒传=整份已存在不传。当前是存储级去重，非带宽秒传。
- **ComposeObject 服务端合并是什么？** → MinIO 内部 UploadPartCopy 合并，数据不过 Go，省内存/带宽。
- **为什么 5MB？** → S3 多段上传硬下限，除末尾片外每片 ≥5MB。

### 设计
- **为什么 Redis Set 记分片状态？** → 天然去重 + O(1) SISMEMBER + SMEMBERS 取全量。
- **为什么先落盘后记账？** → 反过来崩溃会"账上有、MinIO 没"，合并失败；当前最坏多传一次。
- **为什么合并用非阻塞锁？** → 重复请求快速返回"进行中"，DB 唯一索引才是底线。
- **为什么前端切片后端不切？** → 后端只存+记+合并；前端切片减后端负担、支持并发上传。

### 对比选型
- **Redis Set vs Bitmap vs Hash？** → Set 直观去重；Bitmap 省内存但要编号稠密；Hash 适合带单片元数据（若加单片 MD5 防篡改就该换 Hash）。
- **ComposeObject vs Go 内存拼接？** → 服务端省内存/带宽；内存拼接吃内存且数据过 Go。
- **前端直传 MinIO（预签名）vs 后端中转？** → 直传省后端带宽但权限/回调复杂；当前后端中转，逻辑集中。

### 实现方式
- **UploadChunk 流程？** → validateFileMD5→大小校验→PutObject 落 MinIO→SAdd+Expire 记 Redis。media.go:582。
- **完整性校验怎么做的？** → for 循环 SIsMember 查 0..n-1（media.go:629）；⚠️ N 次往返，可优化成一次 SMembers+本地比对。
- **秒传短路在哪？** → MergeChunks 开头 FindByMD5 命中即建 task 返回。media.go:609。

### 边界兜底
- **分片传到 99% 断了？** → check-upload 拿已传片，只补缺失。
- **合并请求被重复/重试发起？** → 非阻塞锁+双检；DB 唯一索引兜底。⚠️ 不是"用户点按钮"，是 HTTP 重试/重发。
- **客户端伪造 MD5？** ⚠️ → 能骗秒传拿别人内容的 task；当前无校验，要单片 MD5 或 SHA-256。
- **合并后临时分片怎么处理？** → `cleanupMergedChunks` 合并成功后 best-effort 删 `chunks/<md5>/*` + Redis key，保留 `:status=COMPLETED`（media.go:670）。**原先不清理是存储泄漏——已修复**。注意：崩溃在 ComposeObject 与 Asset.Create 之间的孤儿合并对象不在清理范围（仍需回收，见 L7）。
- **合并中途崩溃？** → 留孤儿对象；重试时锁过期→重新合并→建成功；靠清理回收。

### 系统设计
- **支持 GB/TB 级要改什么？** → 5MB×10000=50GB 封顶要加分片大小；前端 MD5 要 Worker+采样；临时盘压力。
- **万人同时上传瓶颈？** → 后端带宽、临时盘、Redis 连接、MinIO 吞吐；预签名直传卸载、横向扩。

---

## 第 4 点：Redis Lua 令牌桶限流

### 概念
- **令牌桶是什么？** → 桶按速率生成令牌，请求消耗令牌，满则拒绝；允许突发（桶容量）。
- **令牌桶 vs 漏桶？** → 令牌桶允许突发（攒令牌）；漏桶匀速出水，平滑但无突发。
- **为什么用 Lua？** → 令牌计算+扣减+写回要原子，否则并发下超卖令牌。

### 设计
- **怎么实现"按用户和接口维度"？** → key = `<route>:user:<userID>`（未登录用 IP），每个 (路由,用户) 一个独立桶。ratelimit.go:117。
- **不同接口怎么配不同限额？** → SetRouteLimit 给路由配专属容量/速率，覆盖全局默认；configFor 取配额注入 Lua ARGV。
- **为什么 Redis 异常时 fail-open（放行）？** → 限流是保护手段非关键路径，不应成单点故障；可用性 > 精度。ratelimit.go:101。

### 对比选型
- **Redis 限流 vs 网关限流（Nginx）vs 应用内限流？** → Redis 跨实例共享精确；网关层早拦但粗；应用内单机不共享。
- **令牌桶 vs 滑动窗口 vs 计数器？** → 令牌桶平滑+突发；滑动窗口精确防突发；固定窗口有临界突发。
- **Lua vs WATCH/MULTI 事务？** → Lua 一次往返原子、性能好；WATCH 乐观锁有重试开销、并发高退化。

### 实现方式
- **令牌桶 Lua 脚本逻辑？** → HMGET tokens/last_time→按 elapsed 补令牌(cap 上限)→tokens>=1 扣 1 返 1 否则返 0→HMSET+EXPIRE 60s。ratelimit.go:60。
- **令牌怎么按时间补充？** → `new_tokens = elapsed/1000 * rate`，elapsed 是毫秒。
- **BM25/IDF 参数？**（串第5点）→ k1=1.5,b=0.75。

### 边界兜底
- **Redis 挂了限流就失效？** ⚠️ → 是，fail-open 放行，有被刷风险；可加本地兜底限流或降级策略。
- **时钟漂移会影响吗？** → now 由 Go 传入（time.Now().UnixMilli），多实例时钟偏差会让桶计算略偏，影响小。
- **桶 key 60s 过期后会怎样？** → 下次请求 tokens=nil → 初始化为 capacity（满桶），等于空闲后重置。
- **capacity/rate 能动态调吗？** → 默认值构造时固定；按路由可通过 SetRouteLimit 覆盖，但运行时改默认需重启或加刷新机制。

### 系统设计
- **分布式限流怎么保证多实例一致？** → 限流状态在 Redis 单点，所有实例共享 key，天然一致（牺牲 Redis 单点依赖）。
- **怎么限"高成本 AI 接口"更严？** → 给 AI 路由 SetRouteLimit 更小容量/速率。
- **海量用户下 Redis 压力？** → key 多（每用户每路由一个），用 EXPIRE 清理空闲桶；可抽样/分级限流。

---

## 第 5 点：RAG 混合检索（BM25 + 向量 + RRF）

### 概念
- **RAG 是什么？为什么要检索增强？** → 检索相关片段喂给 LLM 生成，减少幻觉、可引用溯源。
- **混合检索（hybrid）是什么？** → 向量语义召回 + 关键词精确召回，互补。
- **RRF 融合是什么？** → Reciprocal Rank Fusion，按排名而非分数融合多路结果，`score=Σ1/(k+rank)`，避免不同打分体系不可比。

### 设计
- **为什么 BM25 + 向量一起？** → 向量懂语义但漏专有名词/精确词；BM25 懂关键词但不懂语义；互补。
- **为什么用 RRF 而不是加权分数？** → 向量余弦和 BM25 分数量纲不同，直接加权无意义；RRF 只用排名，robust。
- **chunkSize 800、overlap 120 怎么定？** → 太大切不精确召回，太小上下文不足；overlap 保证跨片语义连续。

### 对比选型
- **BM25 vs TF-IDF？** → BM25 有文档长度归一化（b 参数）和饱和（k1），比 TF-IDF 更合理。
- **Milvus vs ES vs pgvector？** → Milvus 专用向量库高吞吐；ES 全文+向量但重；pgvector 轻量集成 PG。
- **RRF vs 线性加权 vs 学习排序（LTR）？** → RRF 无需训练、robust；加权需调参；LTR 效果好但要标注数据。

### 实现方式
- **文本怎么切片？** → `SplitTextIntoChunks` 按 rune 切，chunkSize 800 step=chunkSize-overlap，trim。chunk_splitter.go:10。
- **BM25 怎么算的？** → k1=1.5,b=0.75，IDF=`log(1+(n-df+0.5)/(df+0.5))`，TF 归一化。video_chunk.go:94。
- **RRF 融合代码逻辑？** → 两路按 rank 累加 `1/(k+rank)`(k=60)，按 chunk key 去重，排序取 topK。retrieval_fusion.go:17。
- **中文 query 怎么分词？** → ⚠️ 不是真分词，是 CJK 2-4 字 n-gram 滑窗（addCJKTerms）+ ASCII 词(≥2 字符)。retrieval_fusion.go:163。

### 边界兜底
- **BM25 在哪跑的？** ⚠️ → 在应用内存：ListByTaskID 把该 task 全部 chunk 拉到 Go，再 strings.Count 算词频。不是 MySQL FULLTEXT/ES，chunk 多了扩展性差。
- **embedding 维度不匹配怎么办？** → 维度必须 == 系统配置(1536)，否则 BuildTaskIndex 直接 markFailed。rag_index.go:134。⚠️ 不能混用不同维度模型。
- **重建索引会重复吗？** → 先 DeleteTaskChunks 清旧（按 model）再 ReplaceTaskChunks，幂等。
- **召回为空怎么办？** → BM25 terms 为空返回 nil；融合后可能无结果，LLM 兜底回答。

### 系统设计
- **chunk 数量很大 BM25 慢怎么办？** → 换 ES/MySQL FULLTEXT 做倒排；或离线预算索引；当前在内存只适合单 task 规模。
- **怎么提升召回质量？** → query 改写/扩展、HyDE、调 chunkSize/overlap、多路召回扩 candidate_k。
- **多模型 embedding 共存？** → 按 embedding_model 隔离（chunk 表有该字段），但维度要匹配配置。

---

## 第 6 点：任务失败治理

### 概念
- **失败治理要解决什么？** → 外部 AI/下载服务不稳定，需要分类失败、自动重试、终态标记、可观测。
- **重试策略的要素？** → 哪些错可重试、退避间隔、最大次数、终态（失败/死信）。

### 设计
- **为什么用 DB 驱动重试而不是 Kafka DLQ？** → Kafka 无原生 DLQ（不像 RocketMQ），用 task_job 表 + RetryScheduler 轮询补投，可控可观测。
- **可重试 vs 不可重试怎么分？** → 字符串匹配错误信息：网络/超时/5xx/429/minio/milvus 可重试；API key/无权/文件不存在/维度不符/空结果 不可重试。retry.go:51。
- **退避为什么是 [60,300,900]s 固定数组？** → 简单可控；非指数抖动（可优化点）。

### 对比选型
- **DB 重试调度 vs Kafka 重投 vs 定时任务？** → DB 调度可控可查；Kafka 重投依赖消息仍在；定时任务粗粒度。这里 DB 调度。
- **立即重试 vs 退避重试？** → 退避避免风暴/压垮下游；立即重试对瞬时错有用但易放大。
- **终态 Failed vs Dead？** → Failed=不可重试直接失败；Dead=重试耗尽死亡。

### 实现方式
- **RetryScheduler 怎么跑的？** → 每 30s 一轮，FindDueRetryTasks 批量取(batch 20)，ClaimRetryTask 乐观认领，补投对应 topic。retry.go:193。
- **recordTaskFailure 怎么决策？** → 不可重试→terminal Failed；可重试且未超→记 nextRetryAt；超 maxRetries→Dead。retry.go:103。
- **task_job 表干啥？** → 每 (task,jobType) 一行，跟踪 dispatching/running/completed/failed 状态，唯一索引防重复。
- **转写的分片级续传？** → transcription_chunk 表记每段 ASR 状态，失败段可单独重试，不用重跑整段。

### 边界兜底
- **未知错误（不在两个列表里）会重试吗？** ⚠️ → 不会。isRetryableError 对未知错误返回 false → 当 non_retryable 终态 Failed。这有争议，未知错其实该重试。
- **错误信息文案变了怎么办？** ⚠️ → 字符串匹配会失效（下游改了报错文本），分类就错了。脆弱。
- **补投时 enqueue 失败？** → RestoreRetryAfterDispatchFailure，1 分钟后再试，不丢任务。
- **多个 RetryScheduler 实例会重复补投吗？** → ClaimRetryTask 乐观认领（CAS），保证只有一个拿到。
- **重试期间又来新请求？** → 任务状态机 CAS（UpdateStatusAndStageIf）拦截重复提交。

### 系统设计
- **怎么让重试更稳健？** → 错误分类用类型/错误码而非字符串、加指数退避+抖动、熔断保护下游、死信可人工重试入口。
- **怎么观测失败？** → trace_id 贯穿、task_job 状态/失败码/重试次数、RetryScheduler 日志、失败率监控。
- **死信任务怎么处理？** → DB 里 Dead 状态，可加后台告警 + 人工/自动复活入口。

---

## 自测建议

1. 先盖住提示过一遍，标出卡壳题。
2. 卡壳题对照对应点的专题文档（01–06）+ 代码证据路径深挖。
3. ⚠️ 标记的 landmine 是最高危区，必须能说出"代码真相 + 为什么这么设计/怎么改"。
4. 跨点综合题（如"从上传到 AI 摘要全链路"）串 1+3+5+6 一起练。
