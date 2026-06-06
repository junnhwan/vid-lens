# VidLens RAG Chat 后端规划

> 目标：在现有“视频上传 -> ASR 转写 -> AI 总结”的基础上，新增用户自带模型配置、基于 Milvus 的视频内容 RAG 问答、会话记忆和可防守的后端工程实现。

## 1. 背景与结论

当前项目已经实现了用户体系、视频上传、URL 下载、MinIO 存储、Kafka 异步任务、FFmpeg 音频处理、ASR 转写和 LLM 摘要。后续如果只做“把总结结果继续喂给 AI 聊天”，功能会比较薄，面试时也很容易被追问出没有检索、没有分块、没有记忆、没有引用来源。

公开部署时，ASR、LLM、Embedding 都不能默认消耗服务端自己的 Key。用户本人可以继续使用 MiMo ASR，但其他用户要使用 AI 能力时也必须自己配置对应 provider 的 Key。

更合理的升级方向是：

1. 将 ASR 转写全文作为视频知识源。
2. 后端对转写文本进行 chunk 切分。
3. 调用用户配置的 Embedding 模型生成向量。
4. 写入 Milvus，按 `user_id + task_id` 做数据隔离。
5. 用户对某个视频提问时，先向量检索 Top-K 相关片段。
6. 结合最近会话上下文和检索片段调用 LLM。
7. 返回答案、引用片段和检索分数。

这样才能把项目升级为“基于视频转写内容的 RAG 问答系统”，而不是简单的 AI 聊天壳。

## 2. 范围

### 本轮后端要做

- 用户级 AI 配置，也就是 BYOK：用户自己填写 LLM、ASR、Embedding 各自的 endpoint / baseURL、apiKey 和模型名。
- 新增 OpenAI-compatible Chat 和 Embedding client。
- 新增 Milvus 向量存储接入。
- 新增视频转写文本 chunk 切分与索引构建。
- 新增聊天会话、消息持久化和 Redis 最近消息记忆。
- 新增视频 RAG 问答接口。
- 保证公开部署时服务端不配置平台级 AI Key 也能运行，只是 AI 能力要求用户自己配置。

### 本轮不做

- 不做前端 UI 实现，只提供接口契约给前端 AI。
- 不做 SSE 流式输出，第一版先使用普通 HTTP JSON 返回。
- 不做跨视频知识库问答，第一版只针对单个视频 `task_id` 检索。
- 不做 rerank 模型，第一版只做向量召回 + 阈值过滤。
- 不做复杂长期记忆，第一版采用 MySQL 全量历史 + Redis 最近 N 轮热上下文。
- 不做用户 API Key 余额查询，因为不同 provider 的接口不统一。

## 3. 架构设计

### 后端链路

```text
视频上传 / URL 入库
  -> Kafka 转写任务
  -> FFmpeg 提取音频并切片
  -> 用户 AI 配置
  -> ASR 转写
  -> video_transcriptions 保存全文
  -> chunk 切分
  -> Embedding
  -> Milvus upsert 向量
  -> video_chunks 保存 chunk 元数据

用户提问
  -> 校验登录态和 task 所属权
  -> 读取用户 AI 配置
  -> query embedding
  -> Milvus 按 user_id + task_id 检索 Top-K
  -> 读取 Redis 最近会话消息
  -> 组装 Prompt
  -> 调用 LLM
  -> 保存 chat_messages
  -> 更新 Redis 最近消息
  -> 返回 answer + citations
```

### 模块边界

```text
internal/ai/
  strategy.go              # 现有 ASR / Summary 策略接口，可保留
  chat.go                  # ChatClient 接口与 OpenAI-compatible 实现
  embedding.go             # EmbeddingClient 接口与 OpenAI-compatible 实现
  factory.go               # 根据用户配置分别创建 ASR / Chat / Embedding client

internal/vector/
  milvus.go                # MilvusClient 封装
  schema.go                # collection schema / index 初始化

internal/model/
  ai_profile.go            # 用户 AI 配置
  video_chunk.go           # 转写文本 chunk 元数据
  chat.go                  # 会话与消息

internal/repository/
  ai_profile.go
  video_chunk.go
  chat.go

internal/service/
  ai_profile.go            # BYOK 配置管理
  rag_index.go             # 转写文本切分、Embedding、Milvus 写入
  chat.go                  # RAG 检索、Prompt 组装、LLM 调用、记忆维护

internal/handler/
  ai_profile.go
  chat.go
```

不要把 RAG 问答塞进 `MediaHandler`。`media` 负责视频资产和任务，`chat/rag` 负责基于已有转写结果的问答。

## 4. 数据模型

### user_ai_profiles

用户自己的模型服务配置。公开部署时服务端可以不放任何 AI Key。

```text
id                  bigint primary key
user_id                       bigint index not null
name                          varchar(100) not null
llm_provider                  varchar(50) not null       # openai_compatible / mimo / siliconflow
llm_base_url                  varchar(500) not null      # 例如 https://example.com/v1
llm_api_key_ciphertext        text not null
llm_model                     varchar(100) not null
asr_provider                  varchar(50) not null       # mimo / siliconflow / openai_compatible
asr_base_url                  varchar(500) not null
asr_api_key_ciphertext        text not null
asr_model                     varchar(100) not null
embedding_provider            varchar(50) not null       # openai_compatible
embedding_endpoint            varchar(500) not null      # 完整 endpoint，例如 https://router.tumuer.me/v1/embeddings
embedding_api_key_ciphertext  text not null
embedding_model               varchar(100) not null
embedding_dim                 int not null
is_default                    bool default false
created_at                    datetime
updated_at                    datetime
```

约束：

- 同一个用户只能有一个 `is_default = true`。
- LLM、ASR、Embedding 的 API Key 都不能明文入库。
- 返回给前端时只返回脱敏信息，例如 `sk-****abcd`。
- 删除 profile 前，如果有任务正在运行，不需要阻止；消费者读取不到配置时任务失败并写入明确错误。
- LLM 和 Embedding 不假设来自同一个服务。Embedding 支持保存完整 `/v1/embeddings` endpoint，调用时不再自动拼接路径。
- ASR 也走用户配置。当前用户自己的 ASR 可继续配置为 MiMo：`asr_provider=mimo`、`asr_base_url=https://token-plan-cn.xiaomimimo.com/v1`、`asr_model=mimo-v2.5-asr`。

### video_chunks

保存 chunk 元数据和原文。Milvus 保存向量和必要 payload，MySQL 保存可展示、可回查的文本。

```text
id              bigint primary key
user_id         bigint index not null
task_id         bigint index not null
chunk_index     int not null
content         text not null
content_hash    char(32) not null
token_count     int default 0
embedding_model varchar(100) not null
embedding_dim   int not null
vector_id       varchar(100) unique not null
created_at      datetime
updated_at      datetime
```

索引：

```text
unique(task_id, chunk_index, embedding_model)
index(user_id, task_id)
index(content_hash)
```

### chat_sessions

```text
id          bigint primary key
user_id     bigint index not null
task_id     bigint index not null
title       varchar(200)
created_at  datetime
updated_at  datetime
```

约束：

- 会话必须绑定一个视频任务。
- 查询会话时必须校验 `user_id` 和 `task_id` 所属权。

### chat_messages

```text
id                  bigint primary key
session_id          bigint index not null
user_id             bigint index not null
role                varchar(20) not null       # user / assistant
content             longtext not null
retrieval_snapshot  json                       # assistant 消息保存本轮引用片段
model_name          varchar(100)
created_at          datetime
```

`retrieval_snapshot` 示例：

```json
[
  {
    "chunk_id": 12,
    "chunk_index": 3,
    "score": 0.82,
    "content": "这里是被召回的视频片段..."
  }
]
```

## 5. Milvus 设计

### SDK 选择

实现过程中先尝试过新版 Go SDK：

```bash
go get -u github.com/milvus-io/milvus/client/v2
```

但新版 SDK 会把 `github.com/milvus-io/milvus/pkg/v2` 等较重的服务端依赖带入当前 Windows 编译链，出现平台相关编译问题。为了让本项目在当前本地环境稳定编译，实际实现改用旧版轻量 SDK：

```bash
go get github.com/milvus-io/milvus-sdk-go/v2@v2.4.2
```

对应 import 方向：

```go
import (
  "github.com/milvus-io/milvus-sdk-go/v2/client"
  "github.com/milvus-io/milvus-sdk-go/v2/entity"
)
```

同时将 `github.com/milvus-io/milvus-proto/go-api/v2` 固定在 SDK 兼容版本，避免 proto 版本过新导致 `SearchRequest` 字段不匹配。Milvus 服务端镜像使用 `milvusdb/milvus:v2.4.15`，和 SDK 主版本保持一致。

### collection

建议 collection 名：

```text
vidlens_video_chunks
```

字段：

```text
vector_id       VarChar primary key
user_id         Int64
task_id         Int64
chunk_id        Int64
chunk_index     Int64
embedding_model VarChar
content_hash    VarChar
embedding       FloatVector(dim = user_ai_profiles.embedding_dim)
created_at      Int64
```

Metric：

```text
COSINE
```

Index：

```text
AUTOINDEX
```

过滤表达式：

```text
user_id == {userID} and task_id == {taskID} and embedding_model == "{model}"
```

注意点：

- Milvus collection 的 vector dim 固定。如果用户可能使用不同维度的 embedding 模型，有两种方案：
  1. 第一版限制全站只允许一个 embedding 维度，由配置项控制。
  2. 按维度拆 collection，例如 `vidlens_video_chunks_1024`、`vidlens_video_chunks_1536`。
- 推荐第一版用方案 1，后端校验用户填写的 `embedding_dim` 必须等于系统配置的 `rag.embedding_dim`。
- 如果后续确实要支持多维度，再拆 collection。

## 6. 配置规划

新增配置：

```yaml
rag:
  enabled: true
  chunk_size: 800
  chunk_overlap: 120
  top_k: 5
  min_score: 0.35
  recent_turns: 8
  embedding_dim: 1536
  collection: "vidlens_video_chunks"

milvus:
  address: "127.0.0.1:19530"
  username: ""
  password: ""
  token: ""
  database: "default"
```

`chunk_size` 和 `chunk_overlap` 先按字符数实现，不要第一版就引入复杂 tokenizer。中文视频转写以字符数近似足够，后续再替换为 token 估算。

当前用户提供的 Embedding 服务信息：

```text
embedding_endpoint = https://router.tumuer.me/v1/embeddings
embedding_model    = text-embedding-3-small
embedding_dim      = 1536
```

`text-embedding-3-small` 默认维度是 1536。如果第三方 router 支持 `dimensions` 参数并主动降维，Milvus collection 的维度也必须同步调整。实现时不要只相信用户填写的 `embedding_dim`，配置测试接口必须实际调用一次 embeddings API，并用返回向量长度校验。

## 7. API 设计

### 用户 AI 配置

#### 创建 / 更新配置

```http
POST /api/v1/ai/profiles
Authorization: Bearer <jwt>
```

请求：

```json
{
  "name": "我的 OpenAI Compatible 服务",
  "llm_provider": "openai_compatible",
  "llm_base_url": "https://example.com/v1",
  "llm_api_key": "sk-llm-xxx",
  "llm_model": "deepseek-chat",
  "asr_provider": "mimo",
  "asr_base_url": "https://token-plan-cn.xiaomimimo.com/v1",
  "asr_api_key": "tp-asr-xxx",
  "asr_model": "mimo-v2.5-asr",
  "embedding_provider": "openai_compatible",
  "embedding_endpoint": "https://router.tumuer.me/v1/embeddings",
  "embedding_api_key": "sk-embedding-xxx",
  "embedding_model": "text-embedding-3-small",
  "embedding_dim": 1536,
  "is_default": true
}
```

响应：

```json
{
  "id": 1,
  "name": "我的 OpenAI Compatible 服务",
  "llm_provider": "openai_compatible",
  "llm_base_url": "https://example.com/v1",
  "llm_api_key_masked": "sk-****xxxx",
  "llm_model": "deepseek-chat",
  "asr_provider": "mimo",
  "asr_base_url": "https://token-plan-cn.xiaomimimo.com/v1",
  "asr_api_key_masked": "tp-****xxxx",
  "asr_model": "mimo-v2.5-asr",
  "embedding_provider": "openai_compatible",
  "embedding_endpoint": "https://router.tumuer.me/v1/embeddings",
  "embedding_api_key_masked": "sk-****xxxx",
  "embedding_model": "text-embedding-3-small",
  "embedding_dim": 1536,
  "is_default": true
}
```

#### 查询配置

```http
GET /api/v1/ai/profiles
```

#### 测试配置

```http
POST /api/v1/ai/profiles/test
```

请求同创建配置。后端测试：

1. LLM chat 是否可用。
2. Embedding 是否可用。
3. 返回 embedding 维度是否等于 `embedding_dim`。
4. 如果 `embedding_endpoint` 已经以 `/embeddings` 结尾，客户端直接请求该 URL；不要再追加 `/embeddings`。

### RAG 索引

#### 手动触发索引构建

```http
POST /api/v1/media/task/:id/rag-index
```

行为：

- 校验任务归属。
- 校验任务已有转写文本。
- 校验用户已配置 AI profile。
- 如果已有相同 `task_id + embedding_model` 的 chunks，可以选择重建或跳过。
- 第一版可以同步执行；如果视频文本很长，后续再改成 Kafka 异步 topic。

响应：

```json
{
  "task_id": 2,
  "indexed": true,
  "chunks": 18,
  "embedding_model": "text-embedding-3-small"
}
```

### 聊天会话

#### 创建会话

```http
POST /api/v1/chat/sessions
```

请求：

```json
{
  "task_id": 2,
  "title": "这节课的知识点"
}
```

响应：

```json
{
  "id": 10,
  "task_id": 2,
  "title": "这节课的知识点",
  "created_at": "2026-06-06T..."
}
```

#### 查询会话列表

```http
GET /api/v1/chat/sessions?task_id=2
```

#### 查询消息

```http
GET /api/v1/chat/sessions/:session_id/messages
```

### RAG 问答

```http
POST /api/v1/chat/sessions/:session_id/messages
```

请求：

```json
{
  "question": "这个视频里讲的分布式锁为什么要校验 owner？",
  "top_k": 5
}
```

响应：

```json
{
  "message_id": 101,
  "answer": "视频中提到，释放锁时校验 owner 是为了避免误删其他请求持有的锁...",
  "citations": [
    {
      "chunk_id": 12,
      "chunk_index": 3,
      "score": 0.82,
      "content": "分布式锁释放时要校验 owner..."
    }
  ],
  "model": "deepseek-chat"
}
```

错误返回建议：

```text
400: 请先完成文字提取
400: 请先配置 AI 服务
400: 当前视频尚未构建 RAG 索引
400: 未检索到足够相关的视频片段
403: 无权访问此会话
```

## 8. Prompt 规划

系统 Prompt 核心约束：

```text
你是 VidLens 的视频内容问答助手。
你只能基于给定的视频片段和必要的会话上下文回答。
如果检索片段中没有答案，直接说明“当前视频片段中没有找到相关信息”，不要编造。
回答应尽量引用具体片段，不要把外部常识当成视频内容。
```

组装顺序：

```text
system prompt
retrieved chunks
recent chat history
user question
```

不要把完整转写全文放进 prompt。RAG 的意义就是只召回相关片段。

## 9. 会话记忆

Redis 保存最近 N 轮消息：

```text
vidlens:chat:session:{sessionID}:recent
```

Value：

```json
[
  {"role": "user", "content": "..."},
  {"role": "assistant", "content": "..."}
]
```

策略：

- MySQL 保存全量消息，Redis 只保存最近 `rag.recent_turns` 轮。
- 每次问答成功后，写 MySQL，再刷新 Redis。
- Redis miss 时，从 MySQL 读取最近 N 轮回填。
- TTL 可设置 7 天。

面试说法：

> 大模型本身没有记忆能力。项目里的记忆是服务端维护的上下文窗口：MySQL 保存完整对话历史，Redis 缓存最近几轮热点上下文，请求时只取必要历史参与 prompt，避免上下文无限膨胀。

## 10. 安全与成本控制

### API Key 加密

不能明文保存用户 API Key。建议：

- 配置 `security.api_key_secret` 作为 AES-GCM 加密密钥。
- 入库保存 `api_key_ciphertext`。
- 只有调用模型前在服务端解密。
- 日志禁止打印原始 API Key、Authorization、完整请求头。

配置示例：

```yaml
security:
  api_key_secret: "${VIDLENS_API_KEY_SECRET}"
```

### 权限隔离

所有 RAG 查询必须校验：

1. JWT 用户身份。
2. `chat_session.user_id == current_user_id`。
3. `video_tasks.user_id == current_user_id`。
4. Milvus 检索 filter 必须带 `user_id` 和 `task_id`。

### 成本控制

第一版至少做：

- RAG 问答接口接入现有 Redis Lua 令牌桶限流。
- 限制单次问题长度，例如 1000 字。
- 限制 Top-K，例如最大 10。
- 限制 chunk 数和转写文本最大索引长度。

后续可以做：

- 用户每日调用次数限制。
- 统计 tokens / 请求次数。
- 按用户维度记录 AI 调用日志。

## 11. 实现顺序

### 阶段 A：BYOK 用户配置

1. 新增 `UserAIProfile` model、repository、service、handler。
2. 新增 API Key 加密工具。
3. 新增配置测试接口，分别验证 chat、ASR 和 embedding。
4. 修改现有 AI provider 创建逻辑，支持按用户配置分别创建 ASR / Chat / Embedding client。
5. 修改消费者：处理 ASR / 总结时按 `task.UserID` 获取用户默认 AI profile；没有配置则任务失败并写清楚错误，不能回退使用服务端自己的 Key。

### 阶段 B：Milvus 和 Chunk 索引

1. docker-compose 增加 Milvus standalone。
2. config 增加 `milvus` 和 `rag`。
3. 新增 `vector.MilvusStore`，启动时确保 collection 和 index 存在。
4. 新增 `VideoChunk` model 和 repository。
5. 新增 chunk splitter。
6. 新增 `RAGIndexService.BuildTaskIndex(taskID, userID)`。
7. 转写完成后自动构建索引；也提供手动重建接口。

### 阶段 C：RAG Chat

1. 新增 `ChatSession` / `ChatMessage` model。
2. 新增 session 和 message repository。
3. 新增 Redis recent memory 读写。
4. 新增 `ChatService.Ask(sessionID, userID, question)`。
5. 流程：query embedding -> Milvus search -> 阈值过滤 -> prompt -> LLM -> 保存消息。
6. 新增 chat handler 和路由。

### 阶段 D：测试和文档

1. Repository 用 sqlite 做单元测试。
2. splitter 做边界测试：短文本、长文本、overlap、空文本。
3. AI client 用 fake HTTP server 测试请求格式和错误处理。
4. MilvusStore 可以先抽 interface，用 fake store 测 ChatService。
5. 更新 README 的功能描述时必须区分“已实现 RAG”和“后续演进”。
6. 更新 `docs/troubleshooting-and-interview-notes.md`，记录接入 Milvus 和 BYOK 过程中的真实问题。

## 12. 前端接口说明给其他 AI

可以把下面这段直接交给做前端的 AI：

```text
后端会提供四类接口：

1. 用户 AI 配置：
   - GET /api/v1/ai/profiles
   - POST /api/v1/ai/profiles
   - PUT /api/v1/ai/profiles/:id
   - DELETE /api/v1/ai/profiles/:id
   - POST /api/v1/ai/profiles/test

2. RAG 索引：
   - POST /api/v1/media/task/:id/rag-index

3. 聊天会话：
   - POST /api/v1/chat/sessions
   - GET /api/v1/chat/sessions?task_id=xxx
   - GET /api/v1/chat/sessions/:session_id/messages

4. 发送问题：
   - POST /api/v1/chat/sessions/:session_id/messages

前端需要做：
   - 在用户设置里让用户填写 llm_base_url、llm_api_key、llm_model、asr_base_url、asr_api_key、asr_model、embedding_endpoint、embedding_api_key、embedding_model、embedding_dim。
   - Embedding endpoint 支持完整 URL，例如 https://router.tumuer.me/v1/embeddings，不要强制用户只填 baseURL。
   - ASR 也需要用户自己配置。可以给 MiMo 模板：asr_provider=mimo，asr_base_url=https://token-plan-cn.xiaomimimo.com/v1，asr_model=mimo-v2.5-asr。
   - AI 功能未配置时提示用户先配置模型服务。
   - 在视频任务详情里新增“问问这个视频”的聊天入口。
   - 发送问题后展示 answer。
   - answer 下方展示 citations，包括 chunk_index、score、content。
   - 不需要前端自己调用模型，不需要前端保存 API Key 到 localStorage，API Key 只提交给后端。
```

## 13. 面试话术边界

实现前不能说：

```text
已经实现 RAG 和会话记忆。
```

阶段 A 完成后可以说：

```text
项目支持用户自带模型配置，公开部署时服务端不保存平台级 API Key，而是按用户维度读取加密保存的 provider 配置来调用模型。
```

注意：这里的“模型配置”包括 ASR、LLM 和 Embedding。公开部署版本不能在用户未配置 ASR Key 时调用服务端自己的 MiMo Key。

阶段 B/C 完成后可以说：

```text
项目实现了基于视频 ASR 转写文本的 RAG 问答链路。后端将转写文本切分为 chunk，调用用户配置的 Embedding 模型向量化后写入 Milvus。用户对视频提问时，系统按 user_id 和 task_id 过滤检索 Top-K 相关片段，再结合 Redis 缓存的最近会话上下文调用 LLM 生成回答，并返回引用片段。
```

不要夸大的点：

- 第一版不是跨视频知识库。
- 第一版没有 rerank。
- 第一版没有复杂长期记忆。
- 第一版没有评估体系。
- 如果只用了向量检索，就不要说“混合检索 BM25 + 向量检索”。

## 14. 开工前需要用户提供

用户需要提供：

```text
Embedding endpoint
Embedding API Key
Embedding model: text-embedding-3-small
Embedding dim: 1536
LLM baseURL
LLM API Key
LLM model
ASR baseURL
ASR API Key
ASR model
```

当前已知：LLM 和 Embedding 不是同一个 URL，也不是同一个 OpenAI-compatible 服务，不能共用 baseURL 和 API Key。Embedding 使用 `https://router.tumuer.me/v1/embeddings` 和 `text-embedding-3-small`。用户自己的 ASR 仍可配置为 MiMo，但后端不能默认替其他用户使用服务端 MiMo Key。

## 15. 风险点

- Milvus Docker 镜像较大，部署前应先检查本地和服务器已有 images，避免重复下载。
- Milvus collection 维度固定，第一版必须限制 embedding 维度。
- 用户 API Key 加密密钥一旦丢失，已保存的 Key 无法解密，需要用户重新配置。
- RAG 索引构建可能较慢，长视频建议后续改成 Kafka 异步 topic。
- ASR、Embedding、LLM 可能来自不同 provider，错误处理要明确告诉用户是哪一步失败。
- Embedding endpoint 如果是完整 `/v1/embeddings` URL，客户端不能再拼接路径，否则会请求到错误地址。
