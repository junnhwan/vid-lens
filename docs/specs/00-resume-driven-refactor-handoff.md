# 简历驱动重构 —— 交接文档

> 创建于 2026-07-27。这是本轮重构的**唯一入口**，供后续会话接手。
> 只记录决策与契约，不复制实现细节。实现事实以代码和 `docs/backend-maintenance-map.md` 为准。

## 1. 本轮工作流

用户定义的推进顺序，不要跳步：

```
讨论简历怎么写 → 反推功能目标 → 写规格文档 → 代码 + 测试 → 审查 + 体验收尾
```

当前进度：**第 1 步已完成（本文档），第 2 步已完成（下方改造项表），第 3 步待开始**。

## 2. 已确认前提

| 项 | 结论 |
|---|---|
| 求职阶段 | 校招 / 实习 |
| 简历方向 | AI 平台 —— 后端可靠性与 AI 工程各占一半 |
| 时间预算 | 不设上限，可走完整路线图 |
| 量化方式 | 真实压测/评测跑出来，不接受估算 |

校招含义：**一个点讲透 > 六个技术名词**；个人项目权重高；诚实的能力边界是加分项而非减分项。

## 3. 硬约束（贯穿全程）

本项目最大资产是既有的反 overclaim 严谨性（见 `docs/archive/interview/resume-final-draft.md` 的"不能陈述"清单）。简历驱动天然有过度包装的引力，因此：

- **代码 + 测试 + 可复现证据三者齐备后，才允许把简历里的 `__` 占位符替换成实数。**
- 每条 bullet 必须能指到具体文件 + 一条能跑的验证命令。
- 每份改造 spec 必须包含"简历允许写什么 / 禁止写什么"一节。
- 数字不许估算、不许外推、不许从单次运行结论。

## 4. 目标简历稿

数字一律为 `__` 占位符，未跑出来前不得填写。

**VidLens —— AI 长视频理解与可追溯问答后端**
Go · Gin · GORM · PostgreSQL/pgvector · Redis · Kafka · MinIO · FFmpeg · OpenAI-compatible AI · Vue 3

### A 组：异步任务可靠性

**① 投递一致性与任务状态机** `需改造 B`

> 设计 task/job 两级状态机与 processing lease（token + version + expiry 的数据库 CAS），把 ASR、摘要、RAG 索引移出 HTTP 请求；用 transactional outbox 消除"DB 已提交但 Kafka 投递失败"的丢任务窗口。构建 `__` 组故障矩阵测试（DB 回滚 / enqueue 失败 / 响应丢失 / 进程崩溃 / 重复消息），注入故障下任务不丢失、重复消费无副作用。

现稿短板：只讲重复消费，没讲首次投递窗口——而这正是现有"不能陈述"清单里的一条。

**② 长音频分段 ASR 与失败复用** `现有能力 + 需量化`

> 定位长视频转写"状态成功但文本截断"的隐性故障（base64 请求体超限 + 模型长音频截断），改为 FFmpeg 压缩 + 300s 分片转写并持久化每片状态；失败恢复只重跑缺失片段。实测 `__` 分钟视频单片失败场景下，外部 ASR 调用量减少 `__`%、恢复耗时下降 `__`%。

现稿里最好的一条，只差数字。"状态成功但结果错"的隐性故障叙事对校招很有效。

**③ 大文件断点续传（服务端 durable session）** `现有能力 + 需量化`

> 分片上传以 PostgreSQL 为正确性事实源：user-bound session + 不可变 manifest + chunk ledger + completion lease，服务端校验每片 SHA-256、精确分片大小与全文件 MD5，`io.Pipe` 流式合并并在事务内完成 asset/task CAS。`__`GB 文件中断后仅补传缺失分片，重传量下降 `__`%。

**注意：现稿写的"Redis Set 记录已上传片号"是已被重构掉的旧版本，属于过时且更弱的表述，必须改。**

### B 组：RAG 检索工程

**④ 评测驱动的检索线上化** `需改造 A1 + D` — **主钩子**

> 建立固定 RAG 评测集与离线评测工具，以单变量消融对比纯向量 / BM25 混合 / RRF 融合 / 模型 rerank：Recall@5 `__`→`__`、MRR `__`→`__`、P95 检索延迟 `__`ms。据此决定把混合检索推到线上默认，并为 rerank 配置超时降级与失败回退基线配置。

整份简历最值钱的一条。价值不在"我接了检索"，而在**有评测证据支撑上线决策**——AI 平台方向最想看的能力。

**⑤ 事实源与向量投影分离** `现有能力 + 需量化`

> 将 PostgreSQL `video_chunks` 定为 RAG 事实源、pgvector 定为可重建投影，二者以稳定 evidence ID 绑定并禁止无主向量写入；提供 `rag-audit` 三类漂移对账（source-only / target-only / metadata mismatch）与 `rag-reindex` 断点续跑重建。实测从"投影发布失败"状态完整恢复 `__` 条向量，耗时 `__`。

**⑥ 成本治理与全链路可观测** `需改造 A2 + E`

> 用户级 BYOK（ASR/LLM/Embedding 三类 profile，key 以 AES-GCM 加密入库）+ Redis Lua 令牌桶限制高成本 AI 调用；OTel 跨 HTTP → Kafka → consumer → AI provider 全链路 trace，Prometheus 覆盖任务阶段耗时、AI 调用失败率与 token 成本。压测下定位 `__` 瓶颈，优化后 `__` 提升 `__`%。

**⑦（候选）长任务协作式取消** `需改造 C`

> 长任务删除写入 tombstone 意图后立即返回，worker 在阶段边界检查取消标记并释放 lease；配合 durable cleanup intent 保证 MinIO 对象、向量投影与关系数据最终回收。取消到资源完全回收 P99 `__`s。

差异化最强的钩子（现状：queued/running 删除直接返回 409），但边界微妙、最容易做成半成品。建议放最后。

### 裁剪版本

校招简历单项目 **5 条**为上限，7 条显堆砌。按岗位切：

| 岗位 | 保留 |
|---|---|
| 偏后端基础设施 | ① ② ③ ⑤ ⑥ |
| 偏 AI 平台 | ① ② ④ ⑤ ⑥ |
| 通用投递（默认稿） | ① ② ④ ⑤ ⑥ |

① ② 是后端侧主力，④ ⑤ 是 AI 侧主力，⑥ 两边通吃，③ ⑦ 备选。

## 5. 反推出的改造项

| # | 改造 | 供给简历条 | 依赖 | 状态 |
|---|---|---|---|---|
| **A1** | 固化 RAG 评测集 + 单变量消融报告 | ④ | — | 待开始 |
| **B** | transactional outbox / durable dispatch intent | ① | — | 待开始 |
| **D** | 混合检索线上化 + rerank 超时降级 | ④ | A1 | 待开始 |
| **A2** | 系统压测基线（上传 / 问答 / 异步吞吐） | ⑥ ② ③ ⑤ | B、D 稳定后 | 待开始 |
| **E** | OTel 跨进程 trace 传播 | ⑥ | A2 | 待开始 |
| **C** | 协作式取消 + tombstone | ⑦ | 独立 | 待定，见第 7 节 |

**推进顺序：A1 → B → D → A2 → E → C**

- A1 最便宜且立刻产出数字，用它验证"规格 → 实现 → 验收 → 回填简历"流水线是否走得通。
- B 是纯后端硬骨头且零依赖，早做早稳。
- A2 必须等 B、D 落地后再压，否则数字会被架构变更作废。

### 现状对照（改造起点）

- **B**：`docs/backend-optimization-roadmap.md` 的 P1，已列出故障矩阵草稿和两个候选方案（durable dispatch intent / 小型 outbox）。RetryScheduler 的重试补投已闭环，缺的只是**首次**创建与投递之间的窗口。
- **D**：生产 wiring 中 `EnableBM25 = false`，模型 rerank 仅存在于离线 `cmd/rag-eval`。线上只有 `service.DeterministicReranker`。详见 maintenance map 第 10 节。
- **A1**：`cmd/rag-eval` 与单变量消融约束已存在（`internal/service/rag_eval_config.go`），缺固化评测集与报告产物。
- **C**：`task_cleanup*` 的 durable intent 与 scheduler 已完备，缺的是 worker 侧取消协议；当前 queued/running 删除返回 409。

## 6. 每份改造 spec 的固定结构

```
1. 动机（对应简历哪一条 bullet）
2. 非目标（明确不做什么，防止范围膨胀）
3. 设计
4. 可执行验收命令
5. 简历允许写什么 / 禁止写什么
```

文件命名：`docs/specs/NN-<slug>.md`，NN 按推进顺序编号。

## 7. 待用户拍板

1. **⑦ 协作式取消是否进池子？** 差异化最强，但 worker 取消粒度、已扣费 AI 调用的处理、取消中途崩溃恢复这几处边界很容易做成半成品，做砸会变成面试污点。
2. **③ 分片上传是否保留？** 实现扎实，但与 ② 同属"分段 + 恢复"母题，同时写有重复感。倾向保留并压缩成一行。
3. **⑥ 中的 OTel 是否值得做？** 校招问 trace 概率不高，但"压测 → 定位瓶颈 → 优化"的叙事需要它当证据链。若性价比低，可退化为"复用现有 trace_id + Prometheus"，省掉整个 E 项。

## 8. 相关既有文档

- `docs/archive/interview/resume-final-draft.md` —— 当前简历稿（本轮将被本文档的目标稿取代）
- `docs/backend-maintenance-map.md` —— 实现事实源、不变量、改动落点
- `docs/backend-optimization-roadmap.md` —— 已完成项清单与 P0/P1/P2 优先级
- `docs/stress-test-guide.md` —— A2 的起点
- 全局技能 `resume-project-interview-prep` —— 面试拷打手册生成
