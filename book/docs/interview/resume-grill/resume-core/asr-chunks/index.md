# 简历主线二：长视频分段 ASR 与片段复用

> 练法：先只看问题口述 30 秒，再展开答案；每题都要能说出当前边界。

## Q1：为什么整段音频不能直接做 ASR？

**直接回答：**
长音频更容易触发请求体积、时长、超时或 provider 截断。系统先提取音频，再按默认约 300 秒切片，把失败范围缩小到单片。

**追问与防守：**
切片降低风险，不代表消除了语义边界问题。

**项目证据：** `internal/mq/consumer.go:590-607`；`internal/mq/consumer.go:709-728`。

## Q2：片段的稳定标识是什么？

**直接回答：**
恢复依据是 task_id 加 chunk_index，而不是临时文件名。重试重新生成临时切片后，仍按 index 查询已持久化结果。

**追问与防守：**
前提是切片参数和顺序稳定；改变切片策略需要版本化。

**项目证据：** `internal/mq/consumer.go:730-742`；`internal/repository/transcription_chunk.go:1-160`。

## Q3：片段有哪些状态？

**直接回答：**
调用前写 running，成功写 completed 和 content，失败写 failed 与错误。最终全文只在所有必要片段完成后按顺序拼接并 Upsert。

**追问与防守：**
单独状态让恢复依据落在数据库，而不是进程内存。

**项目证据：** `internal/mq/consumer.go:744-780`；`internal/mq/consumer.go:809-833`。

## Q4：如何做到只重跑失败片段？

**直接回答：**
循环处理前查询对应 chunk；completed 且 content 非空就直接追加并 continue，未完成才调用 strategy.Transcribe。

**追问与防守：**
准确说是复用 ASR 结果；重试仍可能重新下载视频、提取音频和生成临时切片。

**项目证据：** `internal/mq/consumer.go:735-751`；`internal/mq/consumer.go:795-806`。

## Q5：某一片失败后，后续片段会怎样？

**直接回答：**
当前实现遇到某片 ASR 失败就记录 failed 并返回，因此后续未处理片段留待下一次任务重试。下一次先跳过之前 completed 的片段。

**追问与防守：**
这是串行 fail-fast，不应说成所有片段同时独立重试。

**项目证据：** `internal/mq/consumer.go:750-765`。

## Q6：怎样保证最终文本顺序正确？

**直接回答：**
parts 按 chunk 循环顺序 append，复用结果和新结果走相同顺序，最后用双换行 Join。chunk index 是顺序依据。

**追问与防守：**
若未来并行 ASR，需要按 index 收集后排序，不能按 goroutine 返回顺序拼接。

**项目证据：** `internal/mq/consumer.go:730-742`；`internal/mq/consumer.go:780-792`。

## Q7：空 ASR 结果怎么处理？

**直接回答：**
单片文本会 TrimSpace；所有片段最终都没有有效内容时返回“ASR 返回空结果”，重试分类把它视为不可重试，避免无限消耗。

**追问与防守：**
更完善的实现可区分静音片段和 provider 异常空响应。

**项目证据：** `internal/mq/consumer.go:770-787`；`internal/mq/retry.go:60-71`。

## Q8：长视频转写过短怎么排查？

**直接回答：**
依次检查切片数量、每片输出字符数、completed chunk 数量、最终拼接字符数和数据库字段。代码记录这些观测点，能判断问题发生在 FFmpeg、provider、持久化还是拼接。

**追问与防守：**
任务 completed 只代表流程结束，不自动证明文本完整。

**项目证据：** `internal/mq/consumer.go:716-728`；`internal/mq/consumer.go:767-792`。

## Q9：为什么当前没有并行调用所有片段？

**直接回答：**
串行更容易控制 provider 速率、成本和结果顺序，也避免瞬间放大 BYOK 用户的请求。缺点是总耗时随片段数增长。

**追问与防守：**
后续可用有界 worker pool，而不是为每片无上限启动 goroutine。

**项目证据：** `internal/mq/consumer.go:730-782`；`internal/middleware/ratelimit.go:1-220`。

## Q10：切片边界截断一句话怎么办？

**直接回答：**
当前固定时长切片可能切断语句，这是明确限制。可改成静音检测切分或带 overlap 的窗口，并在合并时做文本去重。

**追问与防守：**
不能把固定 300 秒说成语义最优切分。

**项目证据：** `internal/mq/consumer.go:716-718`；`internal/pkg/ffmpeg/ffmpeg.go:1-220`。

## Q11：片段持久化带来什么成本？

**直接回答：**
增加表记录和写放大，但换来失败定位、结果复用和可观测性。长视频 ASR 调用成本远高于几次数据库写入，这个取舍值得。

**追问与防守：**
还需定期清理删除任务对应的 chunk，避免孤儿记录。

**项目证据：** `internal/model/transcription_chunk.go:1-80`；`internal/repository/transcription_chunk.go:1-160`。
