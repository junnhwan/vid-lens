# 简历主线一：Kafka 异步调度、状态与重试

> 练法：先只看问题口述 30 秒，再展开答案；每题都要能说出当前边界。

## Q1：为什么使用 Kafka，而不是同步 HTTP？

**直接回答：**
下载、FFmpeg、ASR、摘要和索引都可能耗时或受外部服务影响。HTTP 只创建任务和投递消息，consumer 执行长任务，避免请求超时后状态失联。

**追问与防守：**
如果只用 goroutine，进程重启时内存任务会丢，无法依靠消费组和持久化任务状态恢复。

**项目证据：** `internal/service/media.go:150-183`；`internal/mq/consumer.go:268-281`。

## Q2：任务状态和处理阶段为什么分开？

**直接回答：**
status 表示 pending、running、completed、failed、dead 等生命周期；stage 表示 downloading、transcribing、summarizing、indexing。失败时二者结合才能说明“在哪一步以什么状态结束”。

**追问与防守：**
只用一个 status 会让用户和运维都看不到具体卡点。

**项目证据：** `internal/model/task.go:12-18`；`internal/model/task.go:31-38`。

## Q3：为什么还要拆 TaskJob？

**直接回答：**
VideoTask 是整体工作流，TaskJob 按 download、transcribe、analyze、rag_index 保存独立状态、重试次数和错误。这样某个子作业失败不会抹掉整条链路的信息。

**追问与防守：**
它不是微服务工作流引擎，而是项目内的持久化子作业模型。

**项目证据：** `internal/model/task_job.go:5-37`。

## Q4：Kafka 能保证消息只处理一次吗？

**直接回答：**
不能把 at-least-once 说成 exactly-once。consumer 先 claim processing lease，busy 时返回错误，stale 或 terminal 时跳过，再用幂等 Upsert 和状态条件限制副作用。

**追问与防守：**
消息可能重复，正确性必须由业务状态和幂等写入共同保证。

**项目证据：** `internal/mq/consumer.go:568-586`；`internal/mq/consumer.go:615-620`。

## Q5：阶梯退避是怎么计算的？

**直接回答：**
默认等待 60、300、900 秒。retry count 映射到对应档位，超过数组长度后继续使用最后一档，避免高频重试持续冲击外部服务。

**追问与防守：**
这是确定性阶梯退避，当前没有在这段策略中加入随机抖动。

**项目证据：** `internal/mq/retry.go:24-53`。

## Q6：哪些错误应该重试？

**直接回答：**
timeout、network、429、5xx、MinIO 或 Milvus 临时错误可重试；配置缺失、无权限、文件不存在、ASR 空结果和 embedding 维度错误属于确定性问题，不应反复调用。

**追问与防守：**
当前分类主要基于错误文本，未来更适合统一 typed error/code。

**项目证据：** `internal/mq/retry.go:55-100`。

## Q7：Kafka 投递和数据库更新双写怎么处理？

**直接回答：**
当前关键路径会先持久化 job 状态，enqueue 失败再记录失败和 next_retry_at；RAG 子任务投递失败还会执行 processing handoff。

**追问与防守：**
这缩小了不一致窗口，但不是完整 transactional outbox，进程在两次外部操作间崩溃仍是边界。

**项目证据：** `internal/service/media.go:174-183`；`internal/mq/consumer.go:1027-1074`。

## Q8：消费者执行中宕机会怎样？

**直接回答：**
processing lease 有 token、kind、expire time 和 version；consumer 获取后启动 heartbeat。宕机后租约过期，后续实例可以重新 claim，而旧执行者失去 lease 后不应继续提交副作用。

**追问与防守：**
这比单纯把状态写 running 更能防止僵尸消费者覆盖新结果。

**项目证据：** `internal/model/task.go:65-68`；`internal/mq/consumer.go:572-586`。

## Q9：如何观察任务卡在哪一步？

**直接回答：**
任务和 job 保存 trace_id、stage、started/finished、last_error、retry_count；consumer 日志也携带 task/job correlation。回答时要落到这些字段，不能只说“加了日志”。

**追问与防守：**
现有可观察性是项目级实现，不包装成成熟 APM 平台。

**项目证据：** `internal/model/task.go:54-75`；`internal/model/task_job.go:19-36`。

## Q10：Kafka 积压时怎么扩容？

**直接回答：**
先按 topic/job 判断瓶颈：ASR 受 provider 配额约束，RAG 受 embedding 和 Milvus 约束。可增加同消费组实例并扩 partition，但并发上限必须受外部 API 限流和数据库连接数约束。

**追问与防守：**
增加 consumer 不等于线性提速，单 partition 仍只能由组内一个 consumer 消费。

**项目证据：** `internal/mq/consumer.go:268-281`；`internal/middleware/ratelimit.go:1-220`。

## Q11：为什么 Go 项目用 Kafka，不照搬参考项目的 RocketMQ？

**直接回答：**
选型服从当前 Go 生态和已有部署。Kafka 的消费组、partition 和成熟 Go 客户端足够支撑视频异步链路；RocketMQ 在 Java 业务消息场景有优势，但不是本项目已实现技术。

**追问与防守：**
面试中比较能力和业务约束，不贬低另一种中间件。

**项目证据：** `cmd/server/main.go:100-140`；`internal/mq/producer.go:1-120`。
