# Spec 03: 内容级去重 + 分析幂等

> 推进顺序第三份。决策母本见 `docs/specs/00-refactor-decisions.md`（第 9.1 节 ⑧）；领域语言见 `CONTEXT.md`。
> 与 spec 02 的关系：投递-消费双层中的消费侧内容层。spec 02 的 Redis SETNX 是 MQ 消息级幂等（同一 MessageId 重复投递），本 spec 是内容+分析目标级幂等（同一视频同一分析重复请求）。两层正交，不可混淆。

## Problem Statement

VidLens 的 AI 调用（ASR、摘要、RAG 索引）又慢又贵又烧 token。现有代码在文件层做了秒传：`UploadFile` 用 `file_md5` 命中已有 `VideoAsset` 时复用资产对象、不再传 MinIO（`media_file_upload.go` 的 `FindByMD5` + `CreateOrRestore`，asset 表 `file_md5` 唯一索引）。**但这层秒传只到文件，不到分析**——同一视频被重复上传（同用户传两次、或不同用户传同一文件），Asset 复用了，下游 Task 和 Job 照样各跑一遍 ASR + 摘要 + 索引，白烧 token。

更具体地看现有缺口：

1. **Task 层不去重**：`createTaskFromAsset` 每次都 `Task.Create`，同 asset 重复上传建多个 task，没有"这内容已经处理过了"的短路。
2. **Job 层只做单 task 内幂等**：`RequestAnalysis`/`RequestTranscribe` 有 `force` 语义和"任务已完成可直接查看结果"的短路，但短路只看当前 task 的 transcription/summary 行——跨 task 同内容不查。用户 A 处理过的视频，用户 B 再传一次，照样重新 ASR。
3. **URL 上传的 `md5(URL)` 是占位哈希**：URL 上传链路是本地测试用的便利功能（非对外主叙事，见 memory `url-upload-not-narrative`），其 `FileMD5 = md5(URL)` 不是内容指纹，不纳入本 spec 主去重语义。

痛点从用户视角：作为这个项目的维护者，我没法诚实回答"同一视频重复上传，你的 ASR/索引 token 烧了几遍"。文件秒传做了，分析层没秒传，等于省了带宽没省 AI 成本——AI 成本才是大头。这是决策记录 9.1 节 ⑧ 写的"按内容指纹 + 分析目标去重，避免相同视频重复解析烧 token"的真稀缺点。

## Solution

把"已完成可直接查看结果"的短路语义，从单 task 内提到 (内容指纹, 分析目标) 级，复用现有 Asset 秒传 + `RequestAnalysis`/`RequestTranscribe` 的 force 短路 seam，不重建：

1. **内容指纹 = Asset 的 `file_md5`**（直接上传链路已算好）。分析目标 = job type（转写 / 摘要 / 索引三类）。去重键 = `(file_md5, job_type)`。
2. **三层分工写死**（spec 必须画清，否则实现撞车）：
   - **文件层**（已有）：Asset `file_md5` 唯一索引 + `FindByMD5`，复用资产对象不重传 MinIO。本 spec 不碰。
   - **内容+目标层**（本 spec 新增）：同 `(file_md5, job_type)` 已有成功结果 → 复用，不重跑 AI。Redis SETNX 短期防并发抢占 + DB 唯一约束 `(file_md5, job_type)` 长期持久。
   - **MQ 消息层**（spec 02 已做）：Redis SETNX on `MessageId`，同一消息重复投递去重。本 spec 不碰。
3. **命中后状态 = 秒传到 Completed**：重复上传命中已有成功结果时，新 task 直接置 `Completed`，结果指向已有 transcription/summary/index 行，不 enqueue 任何 job。叙事支撑"零 AI 调用"。
4. **实现形态借鉴 DOVideo 的"内容指纹 + 目标加锁"形态，Go 侧用 Redis SETNX + DB 唯一约束**（决策记录 9.1 已拍，不用 Redisson）。

## User Stories

1. 作为项目维护者，我想要同内容视频重复上传时复用已有 transcription/summary/index，以便不重复跑 ASR/摘要/索引烧 token。
2. 作为项目维护者，我想要内容指纹是 asset 的 file_md5（直接上传链路已算好），以便不引入第二套指纹机制。
3. 作为项目维护者，我想要分析目标作为去重键的第二维（job_type：转写/摘要/索引），以便同一视频的转写和摘要各自独立去重、不互相误杀。
4. 作为项目维护者，我想要重复上传命中已有成功结果时新 task 秒传到 Completed，以便"零 AI 调用"叙事有可观察证据。
5. 作为项目维护者，我想要秒传 task 的结果行指向已有 transcription/summary/index（按 file_md5 关联），以便新 task 详情页能直接展示已有结果。
6. 作为项目维护者，我想要 Redis SETNX 在并发上传同内容时只让一个请求跑 AI、其余等待复用，以便并发场景不重复跑。
7. 作为项目维护者，我想要 DB 唯一约束 `(file_md5, job_type)` 作为 Redis 失效后的长期持久兜底，以便 Redis 宕机不破坏去重语义。
8. 作为项目维护者，我想要 force 语义保留（force=true 仍可强制重跑），以便内容去重不破坏"重新分析"能力。
9. 作为项目维护者，我想要内容去重与 spec 02 的 MQ 消息级 SETNX 两层分工写进代码注释，以便面试能答清"你为什么有两层幂等"。
10. 作为项目维护者，我想要去重命中的计数可观测（命中次数），以便简历"省 __ 次 AI 调用"有可跑统计来源。
11. 作为项目维护者，我想要部分命中场景正确处理（转写已有但摘要没有时，只跑摘要不重跑转写），以便分析目标级而非视频级去重。
12. 作为项目维护者，我想要索引去重键是 (file_md5, rag_index) 而非简单复用 transcription，以便索引重建（分块策略变更）后能强制重索引而不被旧索引挡住。
13. 作为项目维护者，我想要 URL 上传链路不纳入主去重语义（其 md5(URL) 是占位），以便不为本地图测试便利功能绑架主设计。
14. 作为项目维护者，我想要失败结果不被复用（只复用 status=Completed 的成功结果），以便一次失败的 ASR 不会让后续重试都秒传到一个坏结果。
15. 作为项目维护者，我想要内容去重只在直接文件上传链路（UploadFile）生效，URL 上传（UploadByURL）维持现状，以便主叙事聚焦直接上传。

## Implementation Decisions

### 复用现有 seam，不重建

- 文件层 Asset 秒传（`FindByMD5` + `CreateOrRestore` + `file_md5` 唯一索引）——已有，本 spec 不碰。
- `RequestAnalysis`/`RequestTranscribe` 的 force 短路范式（"已完成可直接查看结果"）——已有，本 spec 把它的查询从"当前 task 的 transcription/summary"提到"按 file_md5 查任意 task 的成功 transcription/summary"。
- `enqueueInitialTask` + dispatch lease（spec 02 已命名）——已有，去重命中时不调用它，直接置 Completed。

### 去重键与查询

- 去重键 = `(file_md5, job_type)`。job_type 取 model 现有 `TaskJobType`（Transcribe / Analyze / RAGIndex）。
- 命中判定：按 `file_md5` 查任意 task 下、`status=Completed` 的对应结果行（transcription / summary / video_chunks 索引）。只复用成功结果。
- 部分命中：逐 job_type 独立判定。转写已有但摘要没有 → 新 task 只 enqueue 摘要 job，不重跑转写。

### 状态与结果引用

- 命中后新 task 直接置 `TaskStatusCompleted`，不走 Pending→Queued→Running 链路。
- 结果引用：新 task 详情查询时按 `file_md5` 关联已有 transcription/summary/index 行展示（不复制行，按 file_md5 join）。
- 索引去重特别注意：(file_md5, rag_index) 而非复用 transcription，索引重建（分块/embedding 模型变更）后强制重索引，旧索引不挡。

### 并发与持久

- Redis SETNX `mq:dedup:content:<file_md5>:<job_type>` 短期防并发抢占（TTL = 处理时长 + guard），一个请求跑 AI 期间其余等待复用。
- DB 唯一约束 `(file_md5, job_type)` 长期持久兜底（Redis 失效后仍保证只有一个成功结果行）。
- DB 唯一约束的实现选择：在 transcription / summary / 索引结果表上加 partial unique index（where status=Completed），还是新建一张 `content_analysis_result` 注册表，留给实现会话按现有 schema 形态选最小改动方案，但必须写清选择理由。

### 三层分工边界（写进代码注释 + spec）

- 文件层（Asset file_md5）：复用资产对象，省带宽。
- 内容+目标层（本 spec）：复用分析结果，省 AI token。
- MQ 消息层（spec 02 SETNX on MessageId）：去重重复投递，保证 at-least-once 无副作用。
- 三层正交，各填各的窗口。这层分工是面试最可能被追的点，必须写清。

### 单一测试 seam

- 验收 seam 两个：
  1. `internal/service/content_dedup_test.go`：直接上传同一文件两次，第二次不 enqueue、task 秒传到 Completed、结果指向已有行。
  2. `internal/repository/task_dedup_test.go`：`(file_md5, job_type)` 唯一约束并发下只有一个成功结果行。
- 外部行为 = 同 (file_md5, job_type) 已有成功结果时重复上传的可观察结果。不新增多个入口。

## 验收命令（数字不许估算，必须能跑）

```bash
# 服务层：去重命中/秒传/部分命中/失败不复用/force/SETNX 锁/命中计数
go test ./internal/service/ -run ContentDedup -v -count=1
# 仓储层：(file_md5, job_type) 唯一约束并发兜底
go test ./internal/repository/ -run "TranscriptionFileMD5|SummaryFileMD5|RAGIndexFileMD5|DedupLookup" -v -count=1
# 命中计数（简历数字来源）
go test ./internal/service/ -run ContentDedupHitCountIsObservable -v -count=1
```

预期：服务层 8 例 + 仓储层 4 例全 PASS；`ContentDedupHits()` 验收跑出命中 `4` 次（见下"数字占位符"）。

## Testing Decisions

### 什么算好测试

- 只测外部行为：重复上传同一文件，第二次是否不 enqueue、是否秒传到 Completed、结果是否指向已有行；部分命中是否只跑缺失的 job；失败结果是否不被复用。
- 不测 Redis SETNX 内部实现（spec 02 已覆盖消息级层）。
- 不测 Asset 秒传（已有测试覆盖）。

### 测试模块

- `internal/service/content_dedup_test.go`（新增）：复用 `media_file_upload_test.go` 的 fake repos + fake mq 范式。
- `internal/repository/task_dedup_test.go`（新增）：复用 `task_lease_test.go` 的并发 CAS 范式。

### Prior art

- `internal/service/media_file_upload_test.go` —— 直接上传 + Asset 秒传的现有测试范式。
- `internal/repository/task_lease_test.go`、`task_dispatch_initial.go` 的 CAS 范式。
- `internal/mq/idempotency.go` —— spec 02 消息级 SETNX 的范式（对照参考，不复用）。

## Out of Scope

- **不碰文件层 Asset 秒传**。已有，本 spec 只在它之后加分析层去重。
- **不碰 spec 02 的 MQ 消息级 SETNX**。两层正交，不合并。
- **不纳入 URL 上传链路**。URL 上传是本地测试便利功能，非对外主叙事（见 memory `url-upload-not-narrative`）；其 `md5(URL)` 占位不动。
- **不新建内容指纹机制**。复用 asset `file_md5`，不引入第二套哈希。
- **不做跨用户可见性治理**。秒传到 Completed 的跨用户复用边界（用户 A 的结果用户 B 能否直接用）留作后续，本 spec 只保证"同内容不重跑 AI"。
- **不做"省 token"的端到端成本测**。省 AI 调用只作为命中次数的派生结论（"命中 N 次 → 等价省 N 次 ASR/索引调用"），不单独跑成本测。
- **不破坏 force 语义**。force=true 仍强制重跑。

## Further Notes

### 与 00-refactor-decisions.md 的对齐

- ⑧ 内容级去重 + 消费幂等（决策记录第 9.1 节）✅
- 借鉴 DOVideo "内容指纹 + 目标加锁"形态，Go 侧用 Redis SETNX + DB 唯一约束（决策记录第 9.1 节）✅
- 与 spec 02 投递-消费双层（决策记录隐含的分层）✅

### 数字占位符（本 spec 产出的简历可用数字）

- 命中次数 `4` 次（验收命令 `go test ./internal/service/ -run ContentDedupHitCountIsObservable` 跑出：3 次重复上传全命中 + 1 次 RequestAnalysis 命中已有摘要；长期运行后从 `service.ContentDedupHits()` 计数采集，非本 spec 阻塞）
- 等价省 AI 调用 `4` 次（命中次数 × 对应 job_type 的 AI 调用数，1:1 派生结论：每次命中 = 省一次对应 ASR/摘要 LLM/embed 调用）
- 三层幂等分工（文件 / 内容+目标 / MQ 消息）—— 非数字，是叙事稀缺点

### 简历允许写什么 / 禁止写什么（本 spec 对应 ⑧ bullet 的预演）

**允许写**：
- "文件秒传之上加分析级去重：按 (内容指纹, 分析目标) 复用已有成功 ASR/摘要/索引结果，重复上传秒传到 Completed、零 AI 调用；Redis SETNX 防并发抢占 + DB 唯一约束持久兜底；与 MQ 消息级幂等形成三层分工，命中 `__` 次省 `__` 次 AI 调用。"
- "三层幂等分工：文件层复用资产省带宽、内容+目标层复用结果省 token、MQ 消息层去重重复投递保证 at-least-once 无副作用。"
- 具体数字（命中次数、省 AI 调用次数）—— **必须**在本 spec 的验收命令跑出来后填，不许估算。

**禁止写**：
- "我用了 Redisson 做内容去重" —— Go 侧不用 Redisson，用 Redis SETNX + DB 唯一约束。
- "全链路内容去重" —— URL 上传不纳入，不能写"全链路"。
- "省了 __ token 成本" —— 只能写"省 __ 次 AI 调用"，token 成本未测不许写。
- 任何未在本 spec 验收命令下产出的数字。
