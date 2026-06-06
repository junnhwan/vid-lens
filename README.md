<div align="center">

# 镜知 · VidLens

**以镜观视，以知见意** — AI 驱动的视频内容理解平台

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev)
[![Vue](https://img.shields.io/badge/Vue-3-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

---

## 📖 项目简介

镜知是一个面向视频内容理解场景的 Go 后端项目：上传视频或提交视频链接后，系统会异步完成视频下载、音频提取、ASR 语音转录和 LLM 智能摘要，并支持基于视频转写文本的 RAG 问答。

项目围绕视频处理中的**长耗时任务**、**重复处理**、**大文件传输**和**外部 AI 服务限制**等问题，使用 **Kafka + Redis 分布式锁 + Lua 令牌桶 + MinIO + FFmpeg** 实现异步处理、对象存储、任务状态管理和 AI 结果持久化。

## 🖼️ 项目截图

### 工作台总览

![工作台总览](docs/images/readme-01-dashboard.png)

### ASR 文字提取

![ASR 文字提取](docs/images/readme-02-transcription.png)

### AI 摘要分析

![AI 摘要分析](docs/images/readme-03-summary.png)

### 用户 AI 配置

![用户 AI 配置](docs/images/readme-04-ai-profile.png)

### 视频 RAG 问答

![视频 RAG 问答](docs/images/readme-05-rag-chat.png)

## ✨ 功能特性

- 🔐 **用户体系** — bcrypt 密码哈希 + JWT 鉴权
- 📤 **视频上传** — 普通上传 / 分片断点续传 / URL 远程下载（B站 / YouTube）
- 🎯 **内容去重** — MD5 秒传 + 唯一索引兜底，避免同一视频重复入库
- 🤖 **用户自带 AI 配置** — 按用户配置 ASR / LLM / Embedding，API Key 加密入库，公开部署不默认消耗服务端 token
- 🧠 **视频 RAG 问答** — 转写文本切分为 chunk，Embedding 后写入 Milvus，问答时按用户和视频检索相关片段并返回引用
- 🗣️ **AI 分析** — ASR 语音转录 + LLM 智能摘要，ASR、LLM、Embedding 可分别配置不同 provider
- 🎧 **长视频处理** — FFmpeg 压缩音频 + 300 秒切片 ASR，降低单次请求体积和长音频漏识别风险
- 🛡️ **接口限流** — Redis Hash + Lua 令牌桶，惰性计算、原子扣减
- 🔒 **并发安全** — Redis 分布式锁，Lua 脚本校验 owner，WatchDog 自动续期
- 💾 **私有存储** — MinIO 私有桶 + 5 分钟预签名 URL
- 📊 **垂直分表** — 任务 / 转录 / 摘要独立建表，避免大文本污染任务主表
- 🔎 **向量检索** — Milvus collection 按 `user_id + task_id + embedding_model` 过滤，避免跨用户召回
- 💬 **会话记忆** — MySQL 保存完整聊天记录，Redis 缓存最近 N 轮上下文
- 🧭 **任务可观测** — 消费者日志记录 taskID、切片数量、单片转写长度和最终转写长度

## 🏗️ 技术架构

![VidLens 后端架构图](docs/images/vidlens-architecture.png)

## 🛠️ 技术栈

| 层级 | 选型 | 说明 |
|------|------|------|
| HTTP 框架 | Gin | 路由分组、中间件链 |
| ORM | GORM | 模型定义、AutoMigrate |
| 消息队列 | Kafka (segmentio/kafka-go) | 异步任务、削峰填谷、消费组负载均衡 |
| 缓存 | Redis (go-redis) | 分布式锁、令牌桶、分片上传状态 |
| 对象存储 | MinIO | 私有桶 + Pre-signed URL |
| 数据库 | MySQL 8.0 | 用户、任务、转录、摘要独立建表 |
| 向量数据库 | Milvus | 视频转写 chunk 向量存储和 Top-K 检索 |
| AI 服务 | OpenAI-compatible / 小米 MiMo / 硅基流动 | 用户级 BYOK 配置，支持 ASR、LLM、Embedding 分离 |
| 音视频 | FFmpeg + yt-dlp | 音频提取、音频切片、视频链接下载 |
| 展示界面 | Vue 3 + Vite | 用于触发上传、查看任务状态和展示转写 / 总结结果 |

## 🚀 快速开始

### 1. 启动中间件

```bash
docker-compose up -d
```

包含：MySQL 8.0、Redis、MinIO、Zookeeper、Kafka、Kafka UI、Milvus、Milvus Etcd。

注意：如果本机没有 Milvus 镜像，首次启动会下载 `milvusdb/milvus:v2.4.15` 和 `quay.io/coreos/etcd:v3.5.18`，镜像体积较大。

本项目会把 MySQL、Redis、MinIO、Milvus 的本地数据挂载到 `data/` 目录，属于运行数据，不需要提交到 Git。

### 2. 配置 API Key 加密密钥

公开部署时不建议在服务端配置平台级 AI Key。后端会让登录用户通过 AI 配置接口提交自己的 ASR、LLM、Embedding provider 信息，并用 `security.api_key_secret` 加密保存 API Key。

PowerShell 中设置：

```powershell
$env:VIDLENS_API_KEY_SECRET="change-this-secret"
```

用户 AI 配置接口：

```text
GET    /api/v1/ai/profiles
POST   /api/v1/ai/profiles
PUT    /api/v1/ai/profiles/:id
DELETE /api/v1/ai/profiles/:id
POST   /api/v1/ai/profiles/test
```

当前用户自己的 MiMo ASR 可以这样配置：

```text
asr_provider = mimo
asr_base_url = https://token-plan-cn.xiaomimimo.com/v1
asr_model = mimo-v2.5-asr
```

Embedding endpoint 支持完整 `/v1/embeddings` URL，例如：

```powershell
embedding_provider = "openai_compatible"
embedding_endpoint = "https://router.tumuer.me/v1/embeddings"
embedding_model = "text-embedding-3-small"
embedding_dim = 1536
```

`config.yaml` 里的 `ai.*` 保留为本地兼容配置；当用户 AI profile resolver 启用后，任务会按 `task.UserID` 读取用户默认配置，未配置时返回“请先配置 AI 服务”，不会回退消耗服务端 Key。

### 3. 配置 FFmpeg 和 yt-dlp

项目依赖本地 FFmpeg 和 yt-dlp：

```yaml
tools:
  ffmpeg_path: "D:/tools/ffmpeg/bin/ffmpeg.exe"
  ytdlp_path: "D:/tools/yt-dlp/yt-dlp.exe"
  cookies_path: ""
```

FFmpeg 用于提取和切分音频，yt-dlp 用于从 B 站 / YouTube 等链接下载视频。如果你的安装路径不同，需要同步修改 `config.yaml`。

B 站链接在服务器公网 IP 上可能触发 `HTTP Error 412: Precondition Failed`，这通常是 B 站风控拦截，不是后端上传或 AI 配置问题。此时可以改用本地视频上传；如果确实要支持服务器下载 B 站链接，可以把 Netscape 格式的 B 站 cookies 文件路径配置到 `tools.cookies_path`，后端会把它传给 yt-dlp。

### 4. 启动后端

```bash
go run ./cmd/server
```

健康检查：

```text
http://localhost:8080/health
```

### 5. 启动展示界面（可选）

```bash
cd web
npm install
npm run dev
```

开发模式访问：

```text
http://127.0.0.1:5173
```

如果要让 Go 后端托管静态资源：

```bash
cd web
npm run build
```

然后访问：

```text
http://localhost:8080
```

登录后可以先在“模型配置”里填写自己的 ASR / LLM / Embedding 服务，再进行文字提取、AI 总结和视频问答。

## 📁 项目结构

```
vid-lens/
├── cmd/server/main.go         # 程序入口，初始化数据库、Redis、MinIO、Kafka、Milvus、AI 配置
├── internal/
│   ├── ai/                    # AI 策略模式、ChatClient、EmbeddingClient、用户配置工厂
│   │   ├── strategy.go            # 策略接口定义
│   │   ├── siliconflow.go         # 硅基流动实现
│   │   └── mimo.go                # 小米 MiMo 实现 + Token Plan 适配
│   ├── config/                # YAML 配置加载
│   ├── handler/               # HTTP 处理层 (Gin Handlers)
│   ├── middleware/            # JWT、CORS、Lua 令牌桶限流
│   ├── model/                 # 用户、任务、转录、摘要、AI 配置、RAG chunk、聊天模型
│   ├── mq/                    # Kafka 生产者 / 消费者
│   ├── pkg/                   # FFmpeg、yt-dlp、JWT、Redis Lock、响应封装
│   ├── repository/            # 数据访问层
│   ├── service/               # 业务逻辑层：媒体、AI 配置、RAG 索引、视频问答
│   ├── storage/               # MinIO 存储操作
│   └── vector/                # Milvus 向量库适配
├── web/                       # 简单展示界面，用于调用后端接口和查看任务结果
├── docs/images/               # README 项目截图
├── config.yaml                # 本地配置文件
├── docker-compose.yml         # 容器编排
└── README.md
```

## 🔑 核心设计

### Kafka 异步架构

- **生产端**：同步发送 + `RequiredAcks=All`，降低消息丢失风险；MD5 作为 Key 保证同一视频进入同一分区
- **消费端**：消费者组分摊分区；业务处理成功后手动 commit offset
- **消费流程**：解析消息 → 分布式锁 → 幂等校验 → 状态流转 → 业务处理 → 结果落盘

### 长视频 ASR 分片处理

- FFmpeg 将视频音频转为更适合语音识别的低码率音频：

```text
-ac 1 -ar 16000 -acodec libmp3lame -b:a 32k
```

- 后端按 300 秒切片，逐段调用 ASR，再合并文本。
- 消费者日志记录 `taskID`、切片数量、每段转写字符数和最终转写长度，便于排查长视频识别不完整的问题。

### AI 总结复用转写

如果任务已经存在 `video_transcriptions.content`，AI 总结会直接复用已有转写文本，不再重复下载视频、提取音频和调用 ASR。这样能减少外部模型调用次数，也降低长视频重复处理失败的概率。

### 用户级 AI 配置

用户通过 AI profile 配置自己的 ASR、LLM、Embedding 服务。三类模型分别保存 provider、endpoint/baseURL、model 和 apiKey，apiKey 使用 AES-GCM 加密入库。Kafka 消费者处理任务时按 `task.UserID` 读取用户默认配置，未配置时任务失败为“请先配置 AI 服务”，不会回退使用服务端 Key。

### 视频 RAG 问答

RAG 的知识源是 ASR 转写全文，不是 AI 总结。后端将转写文本按字符数切成 chunk，调用用户配置的 Embedding 模型生成向量，写入 Milvus。用户提问时，系统先将问题向量化，再按 `user_id + task_id + embedding_model` 过滤检索 Top-K 片段，结合 Redis 最近会话上下文调用 LLM，并返回答案和引用片段。

### Redis 分布式锁

基于 Lua 脚本实现原子加锁和释放，释放锁时校验 owner，避免误删其他任务持有的锁。WatchDog 协程以 `TTL/3` 间隔自动续期，适配视频处理这类长耗时任务。

### Lua 令牌桶限流

Redis Hash 存储令牌桶状态，Lua 脚本实现惰性计算 + 原子扣减，单次请求一次 Redis 往返，60 秒 Key 过期自动清理。

### 分片上传 + 断点续传

- **初始化**：Redis 记录总分片数，设置 24h 过期
- **上传**：每个分片先落盘 MinIO，再 Redis Set 记账
- **断点查询**：读取 Set 返回已传分片列表
- **合并**：分布式锁防并发 → 校验分片完整性 → MinIO ComposeObject 服务端合并

### 任务状态机

```
Pending(0) → Queued(1) → Running(2) → Completed(3)
                                    → Failed(4)
```

任务状态流转通过 `UpdateStatusIf` 做条件更新，避免已处理任务被旧请求覆盖回队列态或运行态。

## 📄 License

MIT License
