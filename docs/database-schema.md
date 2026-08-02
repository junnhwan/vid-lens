# VidLens 数据库 Schema 指南（面试用）

> 来源：`internal/model/*.go`（`AllModels()` 在线 schema）+ `internal/vector/pgvector.go`（`vidlens_rag_vectors`）。本文以代码为准，接线时先看 `docs/backend-maintenance-map.md`。
>
> 正式关系库：**PostgreSQL + pgvector**。MySQL/Milvus 是迁移回滚遗留，不在线。
> 共 **24 张 GORM 表 + 1 张 pgvector 投影表**。

## 0. 一张图看懂

```
                     ┌────────────┐
                     │   users    │  用户（软删，role 区分 USER/ADMIN）
                     └─────┬──────┘
                           │ user_id
        ┌──────────────────┼──────────────────┐
        │                  │                  │
   ┌────▼─────┐   ┌────────▼────────┐   ┌─────▼─────────────┐
   │video_    │   │  video_tasks    │   │ user_ai_profiles  │
   │assets    │◄──┤ (枢纽表)        │   │ (BYOK 密钥密文)    │
   │文件资产   │   │  status/stage   │   └───────────────────┘
   └──────────┘   │  lease/retry    │
                  └───┬────┬────┬───┘
      ┌───────────────┘    │    └──────────────┐
      │                    │                   │
 ┌────▼────────┐  ┌────────▼─────────┐  ┌──────▼────────┐
 │ ASR 产物     │  │ 阶段任务/可靠性     │  │ RAG 索引      │
 │ video_      │  │ task_jobs        │  │ video_chunks  │
 │ transcript  │  │ task_cleanup_jobs│  │ video_rag_    │
 │ -ions       │  │ kafka_message_   │  │  indexes      │
 │ -chunks     │  │  failures        │  │  └→ vidlens_  │
 │ visual_frames│ │ (poison 隔离)     │  │     rag_vect- │
 │ ai_summaries│  │                  │  │     ors (pgv) │
 └─────────────┘  └──────────────────┘  └───────────────┘
       知识库 / 问答              AI 可观测 / 配额
       knowledge_bases           ai_call_logs
       knowledge_base_videos     ai_retry_budgets / _attempts
       chat_sessions             ai_usage_ledgers ← quota_compensations
       chat_messages / sources   user_usage_daily
```

**面试三条主线（记这个比记字段重要）：**

1. **内容级去重三件套**：`video_assets.file_md5`（省带宽）→ `video_transcriptions.file_md5` / `ai_summaries.file_md5` / `video_rag_indexes.(file_md5, embedding_model)`（省 AI token）→ MQ 消息级 SETNX（防重复投递副作用）。**文件层/结果层/MQ 层正交分工**。
2. **任务可靠性状态机**：`video_tasks.status(0-5) + stage + processing_token/lease_*  + task_jobs(UNIQUE(task_id, job_type))` + `task_cleanup_jobs`（durable 删除 intent）+ `kafka_message_failures`（poison 隔离）。手动 commit = "业务结果可靠落库后"。
3. **AI 治理**：`user_ai_profiles`(密钥密文) → `ai_call_logs`(每次调用观测) → `ai_retry_budgets/_attempts`(共享重试预算+幂等) → `ai_usage_ledgers`(可审计配额事实源) → `quota_compensations`(Redis 缓存对账 outbox) → `user_usage_daily`(日聚合)。

整库概念的 "谁依赖谁" 关系图见 `docs/database-schema.svg`。

---

## 1. 用户与身份

### `users` — 用户

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| username | VARCHAR(50) | UNIQUE, NOT NULL | 登录名 |
| password_hash | VARCHAR(255) | NOT NULL | 只存 Hash（bcrypt），永不存明文 |
| nickname | VARCHAR(100) | | |
| avatar | VARCHAR(500) | | |
| role | VARCHAR(20) | default 'USER' | USER / ADMIN |
| created_at / updated_at | TIMESTAMP | | |
| deleted_at | TIMESTAMP | index | **软删**（GORM）。用户数据保留 |

---

## 2. 媒体资产与任务枢纽

### `video_assets` — 内容级文件资产（跨任务复用）

**面试一句**：把"文件唯一性"从"任务唯一性"里拆出来——同一文件多用户/多任务共享一份 MinIO 资产，避免重复存储；`file_md5` 唯一索引实现**秒传**。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| file_md5 | CHAR(32) | UNIQUE, NOT NULL | 内容指纹，秒传/去重键 |
| object_name | VARCHAR(500) | NOT NULL | MinIO 对象 key |
| file_size | BIGINT | default 0 | |
| content_type | VARCHAR(100) | | |
| lifecycle_state | VARCHAR(20) | NOT NULL, default 'active', index | `active` / `deleting`（删除保留，防止被新任务复用） |
| delete_owner_job_id | BIGINT | NULL, index | 只有记录在案的 cleanup job 能软删该资产 |
| created_at / updated_at | TIMESTAMP | | |
| deleted_at | TIMESTAMP | index | 软删 |

**面试一句（删除）**：资产可被多个 task 引用；最后一个 active 引用消失时，cleanup job 才把资产从 `active` reserve 为 `deleting`，`delete_owner_job_id` 是"谁能删"的所有权记录。

### `video_tasks` — 视频任务（**整个异步架构的枢纽表**）

**面试亮点（代码注释自带）**：① `file_md5` 内容级去重 + 秒传；② `status` 严格定义生命周期；③ `(status, created_at)` 联合索引供调度器捞积压任务。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| user_id | BIGINT | NOT NULL, index | |
| asset_id | BIGINT | NULL, index | 复用哪个文件资产 |
| file_md5 | CHAR(32) | NOT NULL, index | 内容指纹（task 级） |
| filename | VARCHAR(255) | NOT NULL | |
| title | VARCHAR(120) | default '' | LLM 生成的标题 |
| file_url | VARCHAR(500) | | MinIO 存储路径 |
| file_size | BIGINT | default 0 | |
| **status** | SMALLINT | default 0, **idx_status_time** | `0`pending `1`queued `2`running `3`completed `4`failed `5`dead |
| **stage** | VARCHAR(50) | default 'none', index | `none / downloading / uploaded / transcribing / visual_indexing / summarizing / indexing` |
| trace_id | VARCHAR(64) | index | 全链路追踪 |
| source_type | VARCHAR(20) | index | `upload / chunked / url` |
| source_url | VARCHAR(1000) | | URL 上传原始地址 |
| retry_count / max_retries | INT | default 0 / 3 | |
| next_retry_at | TIMESTAMP | NULL | 重试调度时间 |
| last_error_code / last_error_msg | VARCHAR | | 最近一次失败 |
| last_job_type | VARCHAR(30) | index | 最近处理的阶段 |
| **processing_token** | VARCHAR(64) | index | 当前 lease 所有者 |
| **lease_kind** | VARCHAR(20) | index | `processing` / `dispatch` |
| **lease_expires_at** | TIMESTAMP | index | 过期后他者可接管 |
| **lease_version** | BIGINT | default 0 | 数据库 CAS 版本 |
| stage_started_at / stage_finished_at | TIMESTAMP | NULL | 阶段级时间 |
| started_at / finished_at | TIMESTAMP | NULL | 任务级时间 |
| error_msg | VARCHAR(500) | | 失败原因展示 |
| created_at / updated_at / deleted_at | TIMESTAMP | 软删 | 软删 + 清理走的独立 intent 表 |

**面试关系**：`video_tasks` 只保留 `status`(兼容主状态) + `stage`；每阶段细粒度 progress 放 `task_jobs`。

### `task_jobs` — 阶段任务明细（可观测）

**面试一句**：`UNIQUE(task_id, job_type)` 确保同一个任务同一种 job 只有一行，`video_tasks` 是兼容状态源，`task_jobs` 是后端观测/重试的落点；下载/转写/分析/RAG 索引四类各自计数。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| task_id | BIGINT | NOT NULL, **UK(task_id, job_type)**, index | |
| user_id | BIGINT | NOT NULL, index | |
| job_type | VARCHAR(30) | NOT NULL, **UK(task_id, job_type)**, index | `download / transcribe / analyze / rag_index` |
| status | SMALLINT | default 0, index | 同全局状态机 |
| stage | VARCHAR(50) | default 'none', index | |
| trace_id | VARCHAR(64) | index | |
| retry_count / max_retries | INT | default 0 / 3 | |
| retry_budget_id | VARCHAR(64) | index | 关联 `ai_retry_budgets.budget_id`，provider 与调度器共享预算 |
| retry_budget_generation | INT | default 0 | |
| next_retry_at | TIMESTAMP | NULL | |
| last_error_code / last_error_msg | VARCHAR | | |
| processing_token / lease_kind / lease_expires_at / lease_version | | | 与 video_tasks 相同的 lease 契约 |
| started_at / finished_at | TIMESTAMP | NULL | |
| created_at / updated_at | TIMESTAMP | | |

### `task_cleanup_jobs` — 删除梳理的 durable intent

**面试一句**：用户 DELETE 的成功边界是"**intent 落库 + task 软删在同一 PostgreSQL 事务提交**"，而不是"外部系统已删完"。外部 MinIO/向量/Redis 是幂等清理目标；失败后 lease 过期由 scheduler 恢复，直到 `completed`。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| task_id | BIGINT | NOT NULL, **UNIQUE** | 一个 task 一个清理 job |
| user_id | BIGINT | NOT NULL, index | |
| asset_id | BIGINT | NULL, index | 要清理的资产 |
| object_name | VARCHAR(500) | | 对应 MinIO 对象 |
| file_md5 | CHAR(32) | index | |
| status | VARCHAR(20) | NOT NULL, index | `pending / running / failed / completed` |
| attempts | INT | NOT NULL, default 0 | |
| next_retry_at | TIMESTAMP | index | |
| lease_token | VARCHAR(64) | index | 完成/失败 `completed` 的 CAS 所有权凭证 |
| lease_expires_at | TIMESTAMP | index | |
| last_error | VARCHAR(1000) | | |
| completed_at | TIMESTAMP | NULL | |
| created_at / updated_at | TIMESTAMP | | |

### `kafka_message_failures` — poison 消息隔离区

**面试一句**：Kafka 消费永远"先隔离、后确认"——消息反复失败先写进这个表格，consumer offset 才能当作成功提交；`(consumer_group, topic, partition, message_offset)` 复合唯一使**重复投递幂等**（不会重复落多条）。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| consumer_group | VARCHAR(255) | NOT NULL, **UK(…)offset** | 复合唯一键的一维 |
| consumer_name | VARCHAR(50) | NOT NULL | 哪个消费者 |
| topic | VARCHAR(255) | NOT NULL, **UK(…)offset** | |
| partition | INT | NOT NULL, **UK(…)offset** | |
| message_offset | BIGINT | NOT NULL, **UK(…)offset** | 三/四列复合唯一 = 幂等键 |
| message_key | BYTEA | | 原始消息体 |
| payload | BYTEA | | |
| error_message | VARCHAR(1000) | NOT NULL | |
| created_at / updated_at | TIMESTAMP | | |

---

## 3. 内容产物：ASR / 视觉 / 摘要

### `video_transcriptions` — ASR 逐字稿（垂直拆分主表）

**面试一句（代码注释自带三个亮点）**：① **垂直拆分**——逐字稿可能数万字，拆出独立表，用户刷历史列表不加载大文本；② 本表 **无 status 列**，"行存在 = 成功"（失败 ASR 不写行，失败只在 `video_tasks` 的 error 字段），因此直接 `UNIQUE(file_md5)` 即等价 partial index；③ `file_md5` 跨 task/跨用户唯一 — **同内容只留一份成功转写，省 AI token**。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| task_id | BIGINT | NOT NULL, UNIQUE | 1:1 一个任务一行 |
| file_md5 | CHAR(32) | NOT NULL, **UNIQUE** `uk_video_transcriptions_file_md5` | 内容指纹，跨 task 去重键 |
| content | TEXT | | 转录全文 |
| words | INT | default 0 | 字数统计 |
| created_at | TIMESTAMP | | |

### `video_transcription_chunks` — 分段 ASR

**面试一句**：长视频 FFmpeg 按时间切片逐个送 ASR，`UNIQUE(task_id, chunk_index)` 每段一行；`status` 独立推进，失败只重试失败段，不必重跑整条音频。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| task_id | BIGINT | NOT NULL, **UK(task_id, chunk_index)**, index | |
| chunk_index | INT | NOT NULL, **UK(task_id, chunk_index)** | 段号 |
| audio_object | VARCHAR(500) | | MinIO 音频片段对象 |
| start_second / end_second | INT | default 0 | 时间窗 |
| status | VARCHAR(30) | NOT NULL, index | `pending / running / completed / failed` |
| content | TEXT | | 该段文本 |
| chars | INT | default 0 | |
| error_msg | VARCHAR(500) | | |
| retry_count | INT | default 0 | |
| created_at / updated_at | TIMESTAMP | | |

### `video_visual_frames` — 关键帧 OCR / 视觉证据

**面试一句**：给 RAG 加"看得见的上下文"——关键帧抽出来，`ocr_text` 是**可搜索文本事实**，`object_key` 指向 MinIO 证据图；`phash` 感知哈希去重相似帧；`source` 标明 `scene/interval/manual`。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| task_id | BIGINT | NOT NULL, **UK(task_id, frame_index)**, index | |
| frame_index | INT | NOT NULL, **UK(task_id, frame_index)** | |
| time_ms | BIGINT | NOT NULL, index | 帧时间戳 |
| object_key | VARCHAR(500) | | MinIO 证据图 |
| ocr_text | TEXT | | 可搜索 OCR 事实 |
| phash | VARCHAR(64) | index | 感知哈希（相似帧去重） |
| source | VARCHAR(30) | NOT NULL | `scene / interval / manual` |
| caption_method | VARCHAR(20) | | `vision`(多模态) / `ocr`(本地) |
| status | VARCHAR(30) | NOT NULL, index | `pending / completed / failed / skipped` |
| error_msg | VARCHAR(500) | | |
| created_at / updated_at | TIMESTAMP | | |

### `ai_summaries` — LLM 摘要

**面试一句**：与转写同样"**行存在 = Completed**"，`task_id` 1:1 + `file_md5` 跨任务唯一省 token；大批量高效渲染 —— `content` 存 **Markdown**，前端直接渲染。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| task_id | BIGINT | NOT NULL, UNIQUE | 1:1 |
| file_md5 | CHAR(32) | NOT NULL, **UNIQUE** `uk_ai_summaries_file_md5` | 内容指纹跨 task 去重键 |
| content | TEXT | | AI 总结（Markdown） |
| model_name | VARCHAR(100) | | 使用的模型名 |
| created_at | TIMESTAMP | | |

---

## 4. RAG：事实源与向量投影

### `video_chunks` — **RAG 事实源（不可删前重建保障）**

**面试一句**：PostgreSQL 里的 chunk 是**唯一事实源**；`UNIQUE(task_id, chunk_index, embedding_model)` 在任务 + 模型维度上唯一，`vector_id` 是稳定 evidence ID，用于 `rag-audit` 与 `vidlens_rag_vectors` 的逐条对账。向量后端只是可重建投影。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| user_id | BIGINT | NOT NULL, index | |
| task_id | BIGINT | NOT NULL, **UK(task_id, chunk_index, embedding_model)**, index | |
| chunk_index | INT | NOT NULL, **UK(…)** | |
| content | TEXT | NOT NULL | chunk 文本 |
| content_hash | CHAR(32) | NOT NULL, index | 文本指纹（重复/漂移校验） |
| token_count | INT | default 0 | |
| **embedding_model** | VARCHAR(100) | NOT NULL, **UK(…)** | 模型不同则维度不同，故进唯一键 |
| embedding_dim | INT | NOT NULL | |
| vector_id | VARCHAR(100) | NOT NULL, **UNIQUE** | 稳定 evidence ID，绑定束 |
| created_at / updated_at | TIMESTAMP | | |

### `video_rag_indexes` — 每 (user, task, model) 的索引状态

**面试一句**：双重唯一——`(user_id, task_id, embedding_model)` 一次构建一行；`(file_md5, embedding_model)` 内容+目标级去重，换模型重建走同一行。`chunk_manifest_sha256` + `build_version` 支持**确定性重建校验**（reindex 前先对账 manifest）。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| user_id | BIGINT | NOT NULL, **UK(user, task, model)**, index | |
| task_id | BIGINT | NOT NULL, **UK(user, task, model)**, index | |
| file_md5 | CHAR(32) | NOT NULL, **UK(file_md5, model)** | 内容级去重键 |
| embedding_model | VARCHAR(100) | NOT NULL, **UK(user, task, model)** 且 **UK(file_md5, model)** | 目标级去重第二维 |
| embedding_dim | INT | NOT NULL | |
| status | VARCHAR(30) | NOT NULL, index | `not_indexed / indexing / indexed / failed` |
| chunk_count | INT | default 0 | |
| chunker_strategy / chunker_version | VARCHAR(50) | | 分块器可版本升级 |
| chunk_size / chunk_overlap | INT | default 0 | |
| chunk_manifest_sha256 | VARCHAR(64) | | 重建校验依据 |
| last_error | VARCHAR(500) | | |
| build_version | INT | default 1 | |
| started_at / finished_at | TIMESTAMP | NULL | |
| created_at / updated_at | TIMESTAMP | | |

### `vidlens_rag_vectors` — pgvector 投影表（**非 GORM，raw SQL 创建**，`internal/vector/pgvector.go`）

**面试一句**：向量存 Post 的 `vector(n)` 列，`<=>` 余弦距离检索；`(user_id, task_id, embedding_model)` 上 scope 索引。**它不是第二事实源** —— 由 `video_chunks` 派生，`vector_id = video_chunks.vector_id` 对齐，`rag-audit` 检查 source-only / target-only / metadata mismatch。同任务重建是**同 PostgreSQL 事务内先删旧投影再写新投影**（事务性 replace）。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| vector_id | TEXT | PK | 稳定 evidence ID（跟 video_chunks 对齐） |
| user_id | BIGINT | NOT NULL | scope 检索 + 审计 |
| task_id | BIGINT | NOT NULL | |
| chunk_id | BIGINT | NOT NULL | 必须绑定正数 `video_chunks.id` |
| chunk_index | INTEGER | NOT NULL | |
| content_hash | VARCHAR(64) | NOT NULL | 漂移审计 |
| embedding_model | TEXT | NOT NULL | 检索必须同模型（维度一致） |
| content | TEXT | NOT NULL | 冗余存 chunk 文本便于 retrieval 直接出内容 |
| embedding_dim | INTEGER | NOT NULL | |
| embedding | vector(N) | NOT NULL | 维度在 `config.rag.embedding_dim` 配置 |
| created_at / updated_at | TIMESTAMPTZ | default NOW() | |
| 索引 | | `(user_id, task_id, embedding_model)` scope idx | |

---

## 5. 知识库（多视频联合检索）

### `knowledge_bases` — 用户级知识库

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| user_id | BIGINT | NOT NULL, index | |
| name | VARCHAR(100) | NOT NULL | `BeforeSave` 去头尾空白 |
| description | VARCHAR(500) | | |
| created_at / updated_at | TIMESTAMP | | |

### `knowledge_base_videos` — 多对多归属边

**面试一句**：一个视频可属于多个知识库，同一库内重复归属被唯一键忽略 → `UNIQUE(knowledge_base_id, task_id)`。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| knowledge_base_id | BIGINT | NOT NULL, **UK(kb, task)**, index | |
| task_id | BIGINT | NOT NULL, **UK(kb, task)**, index | |
| created_at | TIMESTAMP | | |

---

## 6. 会话与问答

### `chat_sessions` — 会话（支持双 scope）

**面试一句**：一个会话要么是**单视频**、要么是**知识库** —— DB CHECK 约束保证二者互斥：`scope_type='video'` 时 `task_id>0 且 knowledge_base_id=0`；`scope_type='knowledge_base'` 时反之。老 fid 是 MySQL 迁移契约（`LegacyChatSession`），在线含 scope 列。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| user_id | BIGINT | NOT NULL, index | |
| task_id | BIGINT | NOT NULL, index | 与下表复合唯一键联合约束 |
| **scope_type** | VARCHAR(30) | NOT NULL, default 'video', **CHECK** `chk_chat_sessions_scope` | `video` / `knowledge_base` |
| knowledge_base_id | BIGINT | NOT NULL, default 0, index | KB scope 时用 |
| title | VARCHAR(200) | | |
| created_at / updated_at | TIMESTAMP | | |

### `chat_messages` — 消息

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| session_id | BIGINT | NOT NULL, index | |
| user_id | BIGINT | NOT NULL, index | |
| role | VARCHAR(20) | NOT NULL | `user / assistant / system` |
| content | TEXT | NOT NULL | |
| retrieval_snapshot | JSON | NULL | **公开引用快照**（用户看到的检索依据） |
| model_name | VARCHAR(100) | | |
| created_at | TIMESTAMP | | |

**面试一句**：最近 N 条走 Redis 缓存（`chat_recent.go`），回源 PostgreSQL；`retrieval_snapshot` JSON 是"引用展示"，`chat_message_sources` 是"程序化引用"（幂等、可过滤、task 清理用）。

### `chat_message_sources` — 引用归一化/程序化来源

**面试一句**：为一条 assistant 消息记录"它基于哪些 task（视频）"，`UNIQUE(message_id, task_id)` 防重复引用；作用：**按来源过滤会话 + 任务删除时告诉清理器哪些消息该失效**。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| message_id | BIGINT | NOT NULL, **UK(message_id, task_id)**, index | |
| session_id | BIGINT | NOT NULL, index | |
| task_id | BIGINT | NOT NULL, **UK(message_id, task_id)**, index | |
| created_at | TIMESTAMP | | |

---

## 7. AI 配置（BYOK）

### `user_ai_profiles` — 用户自带 Key 配置

**面试一句**：生产路径是用户 BYOK，**密钥用 `security.api_key_secret` AES 加密后存 ciphertext 列**（`json:"-"` 不出现在响应）。LLM / ASR / Embedding 三路 + 可选 Vision，`is_default` 给消费者选默认 profile；缺配置报「请先配置 AI 服务」，无静默服务端 key 回退。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| user_id | BIGINT | NOT NULL, index | |
| name | VARCHAR(100) | NOT NULL | 别名 |
| llm_provider / llm_base_url | VARCHAR | NOT NULL | OpenAI 兼容 / MiMo / SiliconFlow |
| llm_api_key_ciphertext | TEXT | NOT NULL | **密文** |
| llm_model | VARCHAR(100) | NOT NULL | |
| asr_provider / asr_base_url / asr_api_key_ciphertext / asr_model | | NOT NULL | ASR 一路 |
| embedding_provider / embedding_endpoint / embedding_api_key_ciphertext / embedding_model | | NOT NULL | 向量一路，`embedding_endpoint` 以 `/embeddings` 结尾可推导 rerank |
| embedding_dim | INT | NOT NULL | 对齐 `vidlens_rag_vectors` 维度 |
| vision_provider / base_url / api_key_ciphertext / model | VARCHAR | default ''（可选） | 空 = 视觉索引只走本地 OCR |
| is_default | BOOL | default false, index | |
| created_at / updated_at | TIMESTAMP | | |

---

## 8. AI 可观测与重试治理

### `ai_call_logs` — 每次 AI 调用观测

**面试一句**：三类(`kind`)调用全埋点，token/时长/费用/错误一起记；`(task_id, stage)`、`(trace_id, created_at)` 索引支持排障。`token_estimated=true` 表示费用是按字符/时长估算，未知测量保持 NULL 不假报 0。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| user_id / task_id / job_id / session_id | BIGINT | index | 关联上下文 |
| trace_id | VARCHAR(64) | index (复合 trace, created) | |
| job_type / stage | VARCHAR | index | |
| attempt | INT | default 0 | 第几次尝试 |
| **kind** | VARCHAR(30) | NOT NULL, index | `asr / llm / embedding` |
| provider / model_name | VARCHAR | index | |
| **status** | VARCHAR(30) | NOT NULL, index | `success / failed` |
| duration_ms | BIGINT | default 0 | |
| input_chars / output_chars | INT | default 0 | |
| prompt_tokens / completion_tokens / total_tokens | BIGINT | NULL | |
| estimated_cost | DECIMAL(18,8) | | |
| token_estimated | BOOL | default false | |
| currency / price_version | VARCHAR | | |
| provider_request_id | VARCHAR(100) | index | 上游请求 ID |
| error_code / error_msg | VARCHAR | | |
| created_at | TIMESTAMP | index | |

### `ai_retry_budgets` — 一次逻辑 AI 操作的共享重试预算

**面试一句**：**provider 装饰器重试 和 任务调度器重试 消费同一个 `budget_id` counter**，进程重启不会"悄悄刷新"重试额度 —— 解决"AI 偶发超时被无限重试"问题，`deadline` 绝对截止。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| **budget_id** | VARCHAR(64) | **PK** | 业务生成的幂等预算 ID（由 task/job/operation 派生） |
| task_id / job_id | BIGINT | index | |
| operation | VARCHAR(40) | NOT NULL, index | 什么逻辑操作 |
| max_attempts | INT | NOT NULL | |
| attempt_count | INT | NOT NULL, default 0 | |
| first_attempt_at | TIMESTAMP | NULL | |
| deadline | TIMESTAMP | NOT NULL, index | 绝对截止 |
| created_at / updated_at | TIMESTAMP | | |

### `ai_retry_attempts` — 重试记账（幂等）

**面试一句**：`UNIQUE(budget_id, attempt_key)` 保证同一次"provider/scheduler 双层层级重试"不重复计数；`layer` 区分 `provider` / `scheduler`。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| budget_id | VARCHAR(64) | NOT NULL, **UK(budget_id, attempt_key)**, index | |
| attempt_key | VARCHAR(128) | NOT NULL, **UK(budget_id, attempt_key)** | 幂等键 |
| layer | VARCHAR(20) | NOT NULL, index | `provider / scheduler` |
| created_at | TIMESTAMP | | |

---

## 9. 配额 / 用量

### `ai_usage_ledgers` — 配额计量**事实源**（可审计流水）

**面试一句**：reserve → settle / release 三种状态记录每个计量事件，`reserved_at → actual`；**未知测量故意保留 NULL 而不报 0**。`idempotency_key` 唯一防重复入账。PostgreSQL 是 source of truth，Redis 日配额只是缓存。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| idempotency_key | VARCHAR(128) | NOT NULL, **UNIQUE** | 防重入账 |
| user_id | BIGINT | NOT NULL, **UK(user, date)**, index | |
| task_id / job_id | BIGINT | index | |
| kind / operation / provider / model_name | VARCHAR | index | 链路上下文 |
| usage_date | CHAR(10) | NOT NULL, **UK(user, date)**, index | |
| **unit** | VARCHAR(20) | NOT NULL | `token / second / call` |
| **status** | VARCHAR(20) | NOT NULL, index | `reserved / settled / released` |
| reserved_units | DECIMAL(20,6) | NOT NULL | 预占 |
| actual_units | DECIMAL(20,6) | NULL | 实测 |
| **usage_source** | VARCHAR(20) | NOT NULL, default 'unknown' | `actual / estimated / unknown` |
| prompt/completion/total tokens | BIGINT | NULL | |
| asr_seconds | DECIMAL(20,6) | | |
| estimated_cost / currency / price_version | | | |
| provider_request_id | VARCHAR(100) | index | |
| release_reason | VARCHAR(255) | | 释放原因 |
| reserved_at / settled_at / released_at | TIMESTAMP | | |
| expires_at | TIMESTAMP | NOT NULL, index | 预占过期 |
| created_at / updated_at | TIMESTAMP | | |

### `quota_compensations` — Redis 日配额对账 outbox

**面试一句**：写 PostgreSQL ledger 后再异步 `compensate` Redis 日缓存，`quota_compensations` 是**持久化 outbox**：`event_key` 唯一 → 在 Redis 里也是幂等键；失败带 lease/退避重试，`completed/dead` 兜底。**不用它就没有"PG 与 Redis 配额最终一致"这个保证。**

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| event_key | VARCHAR(160) | NOT NULL, **UNIQUE** | 幂等键（兼 Redis 键） |
| ledger_id | BIGINT | NOT NULL, index | |
| user_id / usage_date | | NOT NULL, index | |
| kind / unit | VARCHAR | NOT NULL | |
| action | VARCHAR(20) | NOT NULL | 增/减操作 |
| delta_units | DECIMAL(20,6) | NOT NULL | |
| status | VARCHAR(20) | NOT NULL, index | `pending / processing / completed / dead` |
| attempt_count / next_attempt_at | | | 重试 |
| lease_token / lease_expires_at | | index | 执行所有权 |
| last_error / completed_at | | | |
| created_at / updated_at | TIMESTAMP | | |

### `user_usage_daily` — 日聚合（展示/限额查询）

**面试一句**：`UNIQUE(user_id, date)` 每用户每天一行，ASR 秒数 + 各请求计数 + 失败数 + 字符数；是 **Redis 日缓存的库内锚点**，也是限额查询的聚合入口。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | BIGINT | PK, auto | |
| user_id | BIGINT | NOT NULL, **UK(user_id, date)** | |
| date | CHAR(10) | NOT NULL, **UK(user_id, date)** | |
| asr_seconds / asr_requests | INT | default 0 | |
| llm_requests / embedding_requests | INT | default 0 | |
| failed_requests | INT | default 0 | |
| input_chars / output_chars | INT | default 0 | |
| created_at / updated_at | TIMESTAMP | | |

---

## 10. 面试快问快答速记

**Q: 内容去重做了几层？**
文件层 `video_assets.file_md5`(省带宽/秒传) → 内容+目标层 `video_transcriptions.file_md5`、`ai_summaries.file_md5`、`video_rag_indexes(file_md5, embedding_model)`(省 token) → MQ 消息级 SETNX(at-least-once 无副作用)。三者正交分工，见 `VideoTranscription` 注释。

**Q: 任务为什么有 status 还有 stage？**
`status` = 生命周期黑白盒（0-5 严格状态机，调度器/task页用 `(status, created_at)` 捞任务）；`stage` = 处理到什么环节（下载/转写/视觉/摘要/索引），给用户展示进度。`task_jobs` 再按 job_type 拆细粒度观测。

**Q: 怎么防一个任务被两个消费者同时处理？**
`processing_token`+`lease_kind`+`lease_expires_at`+`lease_version`(CAS) 租约；获取/续租/完成/失败全部走 `internal/repository/task_lease*.go`，consumer 不直接改租约字段。

**Q: 为什么 Kafka 失败先写表再提交 offset？**
`kafka_message_failures` 复合唯一键让重复投递幂等；消息成功隔离后才允许 commit → 业务不丢、不重。

**Q: 删除任务的成功边界是什么？**
`task_cleanup_jobs` intent + `video_tasks` 软删在**同一事务**提交即算成功；MinIO/向量/Redis 是幂等异步清理，支持失败重试，不破坏主链路。

**Q: video_chunks 和 vidlens_rag_vectors 什么关系？**
前者是关系模式事实源（唯一键含 embedding_model），后者是 pgvector 投影（可由前者重建）。`rag-audit` 按 `vector_id` 对账二者，只读，不自动删向量。重建用 `cmd/rag-reindex`（显式直写 pgvector，不跟随 `rag.store`）。

**Q: 为什么配额要 ledger + outbox + daily 三张表？**
`ai_usage_ledgers` 是审计事实源（reserve/settle，未知测量留 NULL）；`quota_compensations` 是异步补偿 Redis 日缓存的 outbox（幂等）；`user_usage_daily` 是日聚合展示/限额查询锚点。Redis 只是缓存，丢了能重放。

**Q: AI Key 存哪？**
`user_ai_profiles.*_api_key_ciphertext`，AES 密文，`json:"-"` 不返回；解密与重试治理在 `internal/ai/`。