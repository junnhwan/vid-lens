# 本轮重构决策记录（spec 母本）

> 创建于 2026-07-28。grilling 会话共识的固化。每份 spec 必须与此一致；与此冲突的 spec 不得进入实现。
> 只记录决策与理由，不复制实现细节。

## 1. 简历目标稿（4 条 bullet，校招/实习单项目上限 5，留 1 余量给候选⑦）

| # | bullet | 侧 | 核心稀缺点 |
|---|---|---|---|
| **A** | 意图识别前置路由层 | AI 预防 | 级联分类短路省 LLM 调用 + 指代消解二次提取 |
| **B** | 多阶段检索 + 评测驱动上线 | AI 检索 | 评测证据支撑混合检索/rerank 线上化决策（非"我接了检索"） |
| **①** | 投递一致性与任务状态机 | 后端 job/片级 | transactional outbox 消除首次投递窗口 + 片级失败复用折进 |
| **④** | AI 调用可靠性层 | 后端 AI 调用 | 模型语义降级到"无 LLM 模式"（功能性降级，非普通熔断） |

### 不写什么（显式 no）

- **⑥ 全链路 OTel 不写**：校招问 trace 概率低，"压测→定位→优化"叙事无真做即空话。OTel 整个 E 项砍。BYOK/令牌桶折进④。
- **⑦ 协作式取消不进默认稿**：边界微妙（worker 取消粒度/已扣费 AI 调用/崩溃恢复），做砸是面试污点。留候选池。
- **③ 分片上传断点续传不写**：极常见简历点，差异化弱。③ 的 durable session 思想折进①当对称应用一句。
- **② "状态成功但文本截断"隐性故障叙事不写**：AI 拔高，非真实根因。真实根因是 ASR 模型单次≤10MB 显性约束。② 降级为"片级状态机 + 失败复用"折进①。
- **Agent 不写**：video_agent.go 是实验路径、产品默认不走，无"必须 Agent 才能解决"的痛点。
- **跨视频不另立 bullet**：折进 (B) 作为检索 scope 一档（显式 Collection，会话绑定集合，topic_compare/series_locate intent 放大多视频）。

## 2. 技术栈审视结论

| 组件 | 决策 | 理由 |
|---|---|---|
| Gin | 不动 | 无面试负债 |
| PostgreSQL + pgvector | 不动，单 DB 担事实源 + 向量投影 | 呼应⑤"单源真相、可重建"；⑤折进(B)证据链 |
| Redis | 用法重定位：退到 cache/限流/lease 优化，不当 SoT | 旧版拿 Redis 当上传分片 SoT 是过时弱点（已随③砍掉） |
| MinIO | 不动 | S3 兼容 |
| GORM | 混用：CRUD 留 GORM，可靠性路径（outbox/lease CAS）走 sqlc/原生 SQL | ① spec 子决策 |
| **消息队列** | **RabbitMQ**（推翻原 Kafka 决策） | 见下"消息队列选型"节 |

## 2.1 消息队列选型（推翻原 Kafka 决策）

**决策**：用 RabbitMQ，不用 Kafka。

**痛点驱动**：vid-lens 用 MQ 的真实痛点是 ① 耗时任务出 HTTP 请求、② 失败可恢复（AI 服务挂任务不丢）、③ 削峰（ASR 配额有限需排队）。**没有一个需要 Kafka 的大数据量/日志聚合/高吞吐特性**。吞吐量级是用户级并发（个位到几十 QPS），不是日志管道（万级 TPS）。Kafka 在此场景是杀鸡牛刀，选型说头弱（"我选 Kafka 因为高吞吐" → 你吞吐不高，答含糊），且单 topic 单 consumer 会被追到 partition/consumer group 八股。

**RabbitMQ 契合点**：任务队列场景正解（AMQP 的 ack 重投 / 死信队列 / 优先级 / 路由天然咬合"失败可恢复 + ASR 排队"痛点）。选型说头强且诚实：面试问"为什么不用 Kafka"能硬答"我的痛点是任务可靠投递不是高吞吐日志"。

**配置**：classic persistent queue + publisher confirm + manual ack + outbox。
- RabbitMQ 原生保证：消息从 queue 到 consumer 的 at-least-once（consumer ack 前挂了不丢）。
- outbox 保证：DB 业务事务提交与消息进入 MQ 的原子性，填"DB 提交后 publisher confirm 回来前进程崩"的窗口 —— 这是 RabbitMQ publisher confirm 填不了的（confirm 是异步回调，崩在回调前消息丢）。
- 分工边界必须写进① spec：outbox 不是多余，是补 publisher confirm 的盲区。
- quorum queue（基于 Raft 强一致）作加分项提一句不上 —— 单人项目面试问 Raft 答不实易翻车。

## 3. AI 侧两条的共享基础设施

### 共享 query 池（A 与 B 共用）

- 同一批真实视频（公开课/讲座/教程）人工标注的 query 集，规模需有统计意义（30-50 条，覆盖各 intent 类别）。
- 每条 query 带两类黄金标签：**黄金 intent**（A 用）+ **黄金 evidence chunk**（B 用）。
- 一套基础设施，两份 spec 共用。

### 评测边界（防 overclaim 红线）

- **(B) MRR/Recall@5 只在完整链路（含 rerank）算**。`small_talk`/`video_overview` 这类零检索 intent 不进 MRR 分母。
- **④ 降级模式不进 (B) 评测**。④ 单独测降级行为（rerank 失败→向量基线 MRR；LLM 失败→无 LLM 模式答案可用率），并诚实标注"降级模式 MRR 略低但保证可用性避免请求全废"。
- 两份 spec 数字边界清晰，不混淆。

## 4. (A) 意图识别的借鉴度与诚信约束

- **借鉴形态、自己设计参数与边界**：级联分类 + 指代消解的形态借鉴（通用模式，非 wali 专利），但阈值/signal 维度/intent taxonomy 全用 vid-lens 自己领域语义。
- **不 1:1 移植 wali**：三段打分阈值、0.8 短路、二次提取等全套照搬有面试露馅风险。
- **intent taxonomy**（vid-lens 语义，不用 wali 的 DIAGNOSE/CONFIGURE）：`video_overview`/`direct_qa`/`topic_compare`/`series_locate`/`timeline_locate`/`small_talk`。
- **ExecutionPolicy**（intent → 检索/生成参数）：Retrieve / TopK / Rerank / Rewrite / UseSummary / UseLLM / Scope。一个 switch 消掉 prepareChatByMode 硬编码。
- **(A) spec 必须含"每阈值/维度选择理由"audit trail**：这是反 overclaim 硬约束的直接落地。spec 里每个阈值（如规则层短路阈值、LLM 兜底回退阈值）和每个 signal 维度必须写为什么这么定。
- **(A) 验收 = 仅分类层评测**：固化 case 集（正例/反例/边界/0.79vs0.81 短路）跨规则层与 LLM 层跑准确率/召回/短路率/LLM 兜底命中率。省 LLM 调用只作为短路率的派生结论（"规则层短路率 X% → 等价省 X% 的 LLM 分类调用"），不单独跑端到端成本测。

## 5. (B) 的⑤证据链与跨视频

- **⑤事实源/投影分离折进(B)**：PostgreSQL `video_chunks` 为事实源、pgvector 为可重建投影，evidence ID 绑定，禁止无主向量写入；rag-audit 三类漂移对账 + rag-reindex 断点续跑。作为 (B) 检索可信度的证据链，不单立 bullet。
- **跨视频 = 显式 Collection**：用户命名集合，会话绑定集合；topic_compare/series_locate intent 将检索过滤到集合内 video_ids，答案按视频分组引用。新增 Collection 领域概念。
- **(B) 评测集来源 = 真实视频 + 人工标注**。规模有统计意义。不用 LLM 自动生成（易偏，MRR 被高估）。

## 6. ④ 的降级档位

- **稀缺点 = 档2功能性降级**：LLM 超时/失败 → 回退"无 LLM 模式"（检索片段 + 摘要直拼，标注"参考片段无生成"）。要求系统有意识设计两个交付档。
- 档1（rerank 失败→向量基线）作为降级链的一环，不单独作为稀缺点。
- 前置诚信检查：spec 必须标注"无 LLM 模式"是新建能力还是现有能力补全。

## 7. spec 固定结构（每份必须含）

```
1. 动机（对应哪条 bullet + 真实痛点，非技术名词）
2. 非目标（明确不做什么，防范围膨胀）
3. 设计（含技术选型理由）
4. 可执行验收命令（数字不许估算，必须能跑）
5. 简历允许写什么 / 禁止写什么
```

文件命名：`docs/specs/NN-<slug>.md`，NN 按推进顺序编号。

## 8. 推进顺序

A1（评测基础设施）→ ① → (B) → (A) → ④

- 评测基础设施最便宜且立刻产出数字，验证"规格→实现→验收→回填简历"流水线。
- ① 零依赖纯后端硬骨头，早做早稳。
- (B) 依赖评测基础设施。
- (A) 依赖 (B) 的 ExecutionPolicy 参数语义（intent → 检索预算）。
- ④ 依赖 (B) 的检索链路稳定（降级基线 = 无 rerank 的检索）。

## 9. 已拍板的子决策

1. **① outbox 形态**：transactional outbox 表 + poller/dispatcher（非 durable dispatch intent）。投递目标是 RabbitMQ exchange（非 Kafka partition）。outbox 填"DB 提交后 publisher confirm 回来前进程崩"窗口，补 publisher confirm 盲区。
2. **GORM 混用边界**：CRUD 留 GORM，可靠性路径（outbox/lease CAS/evidence 绑定）走原生 SQL 手写 + 手动 scan。不引 sqlc（单人项目边际成本 > 收益），sqlc 作后续优化项提一句。
3. **(A) 规则层打分维度**：关键词命中 + signal 模式（时间戳/比较句式/概览句式/闲聊）+ 历史 intent 加权。重新定维度，不搬 wali 三段式权重（不适配 vid-lens 视频问答语义）。具体权重数值留给 (A) spec 的 audit trail 写理由。

## 9.1 借鉴 DOVideo-AI 后的候选池扩展（2026-07-28）

对照 `D:\dev\agent-learn\other\DOVideo-AI` 的 README（非你之前给的旧文档版，以仓库 README 为准），三个真实增量 + 三个陷阱：

**新增候选（进池）**：
- **⑧ 内容级去重 + 消费幂等**：按内容指纹（视频 MD5/哈希）+ 分析目标去重，避免相同视频重复解析烧 token。从 ① outbox 的消费幂等独立成 bullet。真稀缺点，呼应"省 AI 成本"主线。借鉴 DOVideo "Redisson 内容指纹+目标加锁"，但 Go 侧不用 Redisson，用 Redis SETNX + DB 唯一约束。
- **⑨ 轻量证据约束生成**（用户拍板"轻量证据约束"）：不做完整 Planner-Executor-Critic Agent Loop（撞车 DOVideo + 工作量大），但在生成阶段加"LLM 生成必须引用检索到的 evidence id，超范围结论被拒"机制。与 (A)/(B) 检索给证据形成完整可追溯叙事。差异化在"证据约束"不在"Agent"。
- **⑩（候选）SSE 阶段推送 + 失败主题可重投**：前端 SSE 接任务阶段 + 失败写独立主题+失败表可重投。真工程点但偏前端编排，AI 平台岗问的概率中等，留候选不进默认。

**陷阱（不跟）**：
- DOVideo 分片上传用 Redis 当分片 SoT —— 你 00-handoff 已标为过时弱点，③ 已砍，不跟。
- DOVideo 多模态 VideoContext（ASR+OCR 并行）—— 会改你 RAG 数据基底（chunk 从纯 ASR 变多模态），推翻 (B) 评测集基于 ASR baseline 的设计。不跟，OCR 留作未来扩展。
- DOVideo Planner-Executor-Critic 完整 Agent Loop —— 撞车 + 工作量大，已拍板用 ⑨ 轻量证据约束替代。

## 10. 全部子决策已拍板

1. **④ 无 LLM 模式呈现**：显式标注 `degraded: true`。API 响应带 degraded 标志，UI 标注"参考片段（AI 摘要暂不可用）"并折叠生成区。显式告知降级态但不暴露内部术语，体验与诚实平衡。面试能答"用户知道当前降级"。

---

至此 grilling 阶段所有决策已钉死。下一步：按推进顺序（评测基础设施 → ① → (B) → (A) → ④）逐份落 spec。spec 落地前如遇新决策点，回此文档追加，不散落到 spec 里。
