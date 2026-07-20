<div align="center">

# 映知

**以镜观视，以知见意** — 面向长视频内容理解的 AI 视频处理平台

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev)
[![Vue](https://img.shields.io/badge/Vue-3-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

---

## 📖 项目简介

映知是一个以 Go 为主的 AI 视频处理后端，支持视频上传、长视频分段 ASR、AI 摘要和带引用的视频问答。项目重点不在于简单调用模型，而是围绕长耗时任务、大文件传输、处理失败恢复和检索结果可追溯性，搭建一条可观察、可重试的处理链路。

视频处理任务通过 Kafka 异步调度，处理阶段、转写分片和聊天记录统一落库到 PostgreSQL；MinIO 负责对象存储，PostgreSQL 内的 pgvector 与关键词检索共同支撑视频 RAG 问答。`video_chunks` 是文本事实源，向量表是可重建的检索投影。

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

- **异步任务与失败恢复**：Kafka 调度 ASR、摘要和 RAG 索引任务，PostgreSQL 记录阶段状态，失败任务按退避策略重试。
- **长视频分段 ASR**：分段转写并持久化结果，失败时只重试对应片段，已完成片段可以复用。
- **分片上传与断点续传**：Redis Set 记录已落入 MinIO 的分片编号，前端恢复时只补传缺失分片，完成后由 MinIO 服务端合并最终对象。
- **可恢复资源清理**：任务删除先持久化 cleanup intent，再通过 lease 与后台扫描恢复 pgvector、MinIO 和 PostgreSQL 的幂等清理；共享 asset 只由最后一个引用的 owner 删除。
- **视频 RAG 问答**：以 ASR 转写为知识源，默认 pgvector 向量检索并返回引用片段；可选 query rewrite / model rerank。BM25+RRF hybrid 在代码中保留，生产默认关闭（见 `cmd/server/wiring.go`）。
- **AI 服务配置**：支持按用户配置 ASR、LLM、Embedding 服务，密钥加密保存。
- **访问与调用治理**：Redis Lua 令牌桶限制高成本接口，并记录 AI 调用与任务处理指标。
- **可观测性**：输出结构化日志，提供 Prometheus 指标和 Grafana 看板，便于定位任务阶段、重试和外部服务错误。

## 🏗️ 技术架构

![映知后端架构图](docs/images/vidlens-architecture.png)

典型处理流程：

```text
视频上传 → Kafka 任务 → 分段 ASR → PostgreSQL 持久化转写
                              ├→ LLM 摘要
                              └→ Embedding → pgvector（可选 rewrite/rerank）→ 引用式问答
```

## 🛠️ 技术栈

| 类别 | 技术 |
|---|---|
| 后端 | Go、Gin、GORM |
| 数据与中间件 | PostgreSQL + pgvector、Redis、Kafka |
| 存储与检索 | MinIO、pgvector（Milvus 适配暂留作向量回滚；BM25/RRF 代码保留，生产默认关） |
| AI 接入 | OpenAI-compatible API、用户级 ASR / LLM / Embedding 配置 |
| 前端 | Vue 3、Vite |
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

PostgreSQL + pgvector 是默认启动的正式业务数据库和向量后端。MySQL 不参与运行时，只在显式增加 `--profile legacy-mysql` 时作为迁移观察期的离线回滚源启动；Milvus/etcd 只在显式增加 `--profile milvus` 时启动，作为向量迁移回滚选项；Prometheus 和 Grafana 只在增加 `--profile observability` 时启动。容器数据会写入项目下的 `data/` 目录。

### 3. 配置密钥与本地参数

不要把真实 API Key 提交到 Git。启动后端前设置用于加密用户 AI 配置的密钥：

```powershell
$env:VIDLENS_API_KEY_SECRET="change-this-secret"
```

根据本机环境修改 `config.yaml` 中的 PostgreSQL、Redis、MinIO、Kafka 和 FFmpeg 配置，并确认 `rag.store: pgvector`。正式数据库连接统一使用 `database.*`，pgvector 表名使用 `rag.vector_table`；`legacy_mysql.*` 只供离线迁移/审计工具使用，server 不读取。回滚用的 Milvus collection 使用 `milvus.collection`，不要再写旧的 `rag.collection`。配置加载会拒绝未知字段，拼写错误会直接导致启动失败。Milvus 配置暂时保留用于迁移观察期回滚，不是当前正式后端。登录后可在“模型配置”页面填写自己的 ASR、LLM、Embedding 服务。

### 4. 启动后端

```bash
go run ./cmd/server
```

存活检查：`http://localhost:8080/healthz`（`/health` 保留兼容）；依赖就绪检查：`http://localhost:8080/readyz`。

### 5. 启动前端（可选）

```bash
cd web
npm install
npm run dev
```

开发页面：`http://127.0.0.1:5173`

## 📁 项目结构

```text
vid-lens/
├── cmd/server/       # 服务入口与运行时组装
├── internal/ai/      # AI 客户端、Provider 和调用治理
├── internal/handler/ # HTTP 接口层
├── internal/mq/      # Kafka 生产者、消费者、重试与租约
├── internal/service/ # 媒体、任务、RAG、聊天等业务服务
├── internal/ragtool/ # 离线评测、投影审计、重建（非请求主路径）
├── internal/repository/ # 数据访问层
├── internal/storage/ # MinIO 对象存储
├── internal/vector/  # 向量存储接口、pgvector / Milvus 适配与后端工厂
├── internal/eval/    # 严格 RAG 评测 harness（配合 cmd/rag-eval）
├── web/              # 展示界面
├── deploy/           # 受测部署脚本及 Prometheus / Grafana 配置
├── docs/             # 维护文档；面试材料在 docs/archive/
├── docker-compose.yml
└── config.yaml
```

## 📚 维护与迁移文档

- [文档入口](docs/README.md)：事实源层级、默认应读/不应读的路径。
- [后端维护地图](docs/backend-maintenance-map.md)：主链路、文件职责、不变量和常见修改入口。
- [pgvector 迁移说明](docs/pgvector-migration.md)：向量重建流程、校验结果和回滚条件。
- [PostgreSQL 单库迁移说明](docs/postgresql-single-database-migration.md)：业务表迁移证据、运行时边界和 MySQL 观察期退出计划。
- [归档：排障与面试记录](docs/archive/interview/troubleshooting-and-interview-notes.md)：历史问题与面试材料（默认开发不必读）。

## 📄 License

MIT License
