<div align="center">

# 映知

**观之以映，释之以知** — 面向视频的 AI 知识库与问答平台

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL%2Bpgvector-17+-4169E1?style=flat&logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![Redis](https://img.shields.io/badge/Redis-DC382D?style=flat&logo=redis&logoColor=white)](https://redis.io)
[![RabbitMQ](https://img.shields.io/badge/RabbitMQ-FF6600?style=flat&logo=rabbitmq&logoColor=white)](https://www.rabbitmq.com)
[![MinIO](https://img.shields.io/badge/MinIO-C72E49?style=flat&logo=minio&logoColor=white)](https://min.io)
[![FFmpeg](https://img.shields.io/badge/FFmpeg-007808?style=flat&logo=ffmpeg&logoColor=white)](https://ffmpeg.org)
[![Next.js](https://img.shields.io/badge/Next.js-14-000000?style=flat&logo=nextdotjs&logoColor=white)](https://nextjs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

---

## 📖 项目简介

映知是一个面向视频的 AI 知识库与问答平台：上传视频并转写后，把长视频变成可检索、可引用的文本知识，随后直接对视频内容提问，回答带引用片段、可回溯到原始视频。

项目重点不在于简单调用模型，而是围绕长耗时任务、大文件传输、处理失败恢复和检索结果可追溯性，搭建一条可观察、可重试的处理链路。

视频处理任务通过 RabbitMQ 异步调度，处理阶段、转写分片和聊天记录统一落库到 PostgreSQL；MinIO 负责对象存储，PostgreSQL 内的 pgvector 与关键词检索共同支撑视频 RAG 问答。`video_chunks` 是文本事实源，向量表是可重建的检索投影。

## 🖼️ 项目截图

| 工作台 | ASR 文字提取 |
|---|---|
| ![工作台总览](docs/images/readme-01-dashboard.png) | ![ASR 文字提取](docs/images/readme-02-transcription.png) |

| AI 摘要 | 用户 AI 配置 |
|---|---|
| ![AI 摘要分析](docs/images/readme-03-summary.png) | ![用户 AI 配置](docs/images/readme-04-ai-profile.png) |

| 视频 RAG 问答 |
|---|
| ![视频 RAG 问答](docs/images/readme-05-rag-chat.png) |

## ✨ 核心功能

- **异步任务与失败恢复**：RabbitMQ 调度 ASR、摘要和 RAG 索引任务，PostgreSQL 记录阶段状态，失败任务按退避策略重试。选 RabbitMQ 而非 Kafka，是因为真实痛点是任务可靠投递、失败恢复与削峰，而非高吞吐日志。
- **长视频分段 ASR**：分段转写并持久化结果，失败时只重试对应片段，已完成片段可以复用。
- **分片上传与断点续传**：Redis Set 记录已落入 MinIO 的分片编号，前端恢复时只补传缺失分片，完成后由 MinIO 服务端合并最终对象。
- **可恢复资源清理**：任务删除先持久化 cleanup intent，再通过 lease 与后台扫描恢复 pgvector、MinIO 和 PostgreSQL 的幂等清理；共享 asset 只由最后一个引用的 owner 删除。
- **视频 RAG 问答**：以 ASR 转写为知识源，pgvector 向量检索并带引用片段返回；可选 query rewrite / model rerank。
- **AI 服务配置**：支持按用户配置 ASR、LLM、Embedding 服务，密钥加密保存。
- **访问与调用治理**：Redis Lua 令牌桶限制高成本接口，并记录 AI 调用与任务处理指标。
- **可观测性**：输出结构化日志，提供 Prometheus 指标和 Grafana 看板，便于定位任务阶段、重试和外部服务错误。

## 🏗️ 技术架构

![映知后端架构图](docs/images/vidlens-architecture.png)

典型处理流程：

```text
视频上传 → RabbitMQ 任务 → 分段 ASR → PostgreSQL 持久化转写
                              ├→ LLM 摘要
                              └→ Embedding → pgvector（可选 rewrite/rerank）→ 引用式问答
```

## 🛠️ 技术栈

| 类别 | 技术 |
|---|---|
| 后端 | Go、Gin、GORM |
| 数据与存储 | PostgreSQL + pgvector（主库与向量库）、Redis、MinIO |
| 消息队列 | RabbitMQ |
| 检索 | pgvector（向量检索）、BM25（关键词检索） |
| AI 接入 | OpenAI-compatible API、用户级 ASR / LLM / Embedding 配置 |
| 前端 | Next.js 14（`web-next`） |
| 音视频处理 | FFmpeg（音频提取与切片） |
| 监控 | Prometheus、Grafana |

## 🚀 快速开始

### 1. 准备环境

- Go 1.24+
- Docker / Docker Compose
- FFmpeg，并在 `config.yaml` 中配置 `tools.ffmpeg_path`
- 可用的 ASR、LLM、Embedding 服务

### 2. 启动中间件

```bash
docker compose up -d
```

默认启动 PostgreSQL + pgvector、Redis、RabbitMQ、MinIO；`--profile observability` 额外启动 Prometheus 与 Grafana。容器数据落在项目下的 `data/` 目录。

### 3. 配置本地参数

按本机环境修改 `config.yaml` 中的 PostgreSQL、Redis、MinIO、RabbitMQ 与 FFmpeg 配置，确认 `rag.store: pgvector`。配置加载会拒绝未知字段，拼写错误会导致启动失败。登录后可在“模型配置”页面填写自己的 ASR、LLM、Embedding 服务。

### 4. 启动后端

```bash
go run ./cmd/server
```

存活检查：`http://localhost:8080/healthz`（`/health` 保留兼容）；依赖就绪检查：`http://localhost:8080/readyz`。

### 5. 启动前端（可选）

前端（Next.js 14）：

```bash
cd web-next
npm install
npm run dev -p 5173
```

开发页面：`http://127.0.0.1:5173`（`/api` 改写代理到 `:8080`）。

## 📁 项目结构

```text
vid-lens/
├── cmd/server/       # 服务入口与运行时组装（main / wiring / router）
├── internal/
│   ├── ai/           # AI 客户端、Provider 与调用治理
│   ├── handler/      # HTTP 接口层
│   ├── mq/           # RabbitMQ 生产者、消费者、重试与租约
│   ├── service/      # 媒体、任务、RAG、聊天等业务服务
│   ├── repository/   # 数据访问层
│   ├── storage/      # MinIO 对象存储
│   └── vector/       # 向量存储接口（pgvector）
├── web-next/         # Next.js 14 前端
├── docs/             # 文档与设计说明
├── docker-compose.yml
└── config.yaml
```

## 📚 文档

- [文档入口](docs/README.md)：文档组织与阅读指引。
- [后端维护地图](docs/backend-maintenance-map.md)：主链路、文件职责与常见修改入口。
- [评测资料说明](docs/eval/README.md)：离线 RAG/ASR 评测用例。

## 📄 License

MIT License
