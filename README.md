<div align="center">

# 镜知 · VidLens

**以镜观视，以知见意** — AI 驱动的智能视频内容理解平台

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev)
[![Vue](https://img.shields.io/badge/Vue-3-4FC08D?style=flat&logo=vue.js)](https://vuejs.org)
[![Kafka](https://img.shields.io/badge/Kafka-7.5-231F20?style=flat&logo=apache-kafka)](https://kafka.apache.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

---

## 📖 项目简介

镜知是一个完整的**视频 AI 分析平台**：上传视频 → 自动提取音频 → ASR 语音转录 → LLM 智能摘要，全链路异步处理，开箱即用。

针对视频处理场景中**「长耗时阻塞」**、**「高并发资源冲突」**、**「大文件传输不稳定」**三大痛点，项目基于 **Kafka + Redis 分布式锁 + Lua 令牌桶 + 分片上传** 构建了一套生产级的异步处理架构。

## ✨ 功能特性

- 🔐 **用户体系** — bcrypt 密码哈希 + JWT 鉴权
- 📤 **视频上传** — 普通上传 / 分片断点续传 / URL 远程下载（B站 / YouTube）
- 🎯 **内容去重** — MD5 秒传 + 唯一索引物理兜底
- 🤖 **AI 分析** — 语音转录（ASR）+ 智能摘要（LLM），策略模式可切换供应商
- 🛡️ **接口限流** — Redis Hash + Lua 令牌桶，惰性计算原子扣减
- 🔒 **并发安全** — 自研 Redis 分布式锁，Lua 脚本 + WatchDog 自动续期
- 💾 **私有存储** — MinIO 私有桶 + 5 分钟预签名 URL
- 📊 **垂直分表** — 任务 / 转录 / 摘要独立建表，保护主表查询性能

## 🏗️ 技术架构

```
┌─────────────────────────────────────────────────────────────┐
│                         客户端 (Vue3)                        │
└──────┬──────────┬──────────────┬──────────────┬──────────────┘
       │          │              │              │
   文件上传    分片上传      AI分析请求     轮询任务状态
       │          │              │              │
┌──────▼──────────▼──────────────▼──────────────▼──────────────┐
│                      Gin HTTP Server                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│  │ JWT Auth │  │   CORS   │  │RateLimit │  │ Handlers │    │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘    │
└──────────────────────┬──────────────────────────────────────┘
                       │ 投递消息 (同步发送, RequiredAcks=All)
                       ▼
              ┌─────────────────┐
              │  Kafka Cluster  │  ◄── MD5 做 Key, 同视频同分区
              │  ┌────────────┐ │
              │  │ 4 Partitions│ │
              │  └────────────┘ │
              └────────┬────────┘
                       │ 消费者组拉取 (手动 commit offset)
                       ▼
         ┌─────────────────────────────┐
         │     Kafka Consumer Worker    │
         │  ① 解析消息                  │
         │  ② 分布式锁 (Redis + Lua)    │
         │  ③ 幂等校验 (状态机检查)      │
         │  ④ 更新状态 → Running        │
         │  ⑤ FFmpeg → ASR → LLM       │
         │  ⑥ 更新状态 → Completed      │
         │  ⑦ 手动提交 offset           │
         └─────────────────────────────┘
                       │
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
   ┌─────────┐   ┌─────────┐   ┌─────────┐
   │  MySQL  │   │  Redis  │   │  MinIO  │
   │ (持久化) │   │ (缓存锁) │   │ (视频文件)│
   └─────────┘   └─────────┘   └─────────┘
```

## 🛠️ 技术栈

| 层级 | 选型 | 说明 |
|------|------|------|
| HTTP 框架 | Gin | 路由分组、中间件链 |
| ORM | GORM | 模型定义、AutoMigrate |
| 消息队列 | Kafka (segmentio/kafka-go) | 异步任务、削峰填谷、消费组负载均衡 |
| 缓存 | Redis (go-redis) | 分布式锁、令牌桶、分片状态追踪 |
| 对象存储 | MinIO | 私有桶 + Pre-signed URL |
| 数据库 | MySQL 8.0 | 4 表垂直分拆 |
| AI 服务 | 小米 MiMo / 硅基流动 | 策略模式切换供应商，支持 ASR + LLM 总结 |
| 音视频 | FFmpeg + yt-dlp | 音频提取、视频下载 |
| 前端 | Vue 3 + Axios | 登录、上传、任务管理、Markdown 渲染 |

## 🚀 快速开始

### 1. 启动中间件

```bash
docker-compose up -d
```

包含：MySQL 8.0、Redis、MinIO、Zookeeper、Kafka、Kafka UI。

### 2. 配置

编辑 `config.yaml`，选择 AI 供应商并配置 API Key。默认使用小米 MiMo Token Plan：

```powershell
$env:MIMO_API_KEY="tp-your-key-here"
```

如需改回硅基流动，将 `ai.provider` 改为 `siliconflow`，并设置 `SILICONFLOW_API_KEY`。

### 3. 启动后端

```bash
go run cmd/server/main.go
```

### 4. 启动前端（可选）

```bash
cd web
npm install
npm run dev
```

或直接构建前端静态文件，后端自动托管：

```bash
cd web && npm run build
```

访问 `http://localhost:8080` 即可使用。

## 📁 项目结构

```
vid-lens/
├── cmd/server/main.go         # 程序入口，初始化所有组件
├── internal/
│   ├── ai/                    # AI 策略模式 (Strategy Interface)
│   │   ├── strategy.go            # 策略接口定义
│   │   ├── siliconflow.go         # 硅基流动实现 + 指数退避重试
│   │   └── mimo.go                # 小米 MiMo 实现 + Token Plan 适配
│   ├── config/                # YAML 配置加载
│   ├── handler/               # HTTP 处理层 (Gin Handlers)
│   ├── middleware/             # 中间件
│   │   ├── auth.go                # JWT 认证
│   │   ├── cors.go                # 跨域
│   │   └── ratelimit.go           # Lua 令牌桶限流
│   ├── model/                 # 数据模型 (4 表垂直分拆)
│   │   ├── user.go                # 用户表 (bcrypt 密码)
│   │   ├── task.go                # 视频任务表 (5 状态机)
│   │   ├── transcription.go       # 转录文本表 (独立存储)
│   │   └── summary.go             # AI 摘要表 (独立存储)
│   ├── mq/                    # Kafka 消息队列
│   │   ├── producer.go            # 生产者 (同步发送, Key 路由)
│   │   └── consumer.go            # 消费者 (六步规范, 手动 commit)
│   ├── pkg/                   # 内部工具包
│   │   ├── ffmpeg/                # FFmpeg 音频提取
│   │   ├── jwt/                   # JWT 生成与解析
│   │   ├── lock/                  # Redis 分布式锁 (Lua + WatchDog)
│   │   ├── response/              # 统一响应封装
│   │   └── ytdlp/                 # yt-dlp 视频下载
│   ├── repository/            # 数据访问层
│   ├── service/               # 业务逻辑层
│   └── storage/               # MinIO 存储操作
├── web/                       # Vue3 前端
├── config.yaml                # 配置文件
├── docker-compose.yml         # 容器编排
└── README.md
```

## 🔑 核心设计

### Kafka 异步架构

- **生产端**：同步发送 + `RequiredAcks=All`，确保消息不丢失；MD5 作为 Key 保证同一视频进入同一分区
- **消费端**：消费者组分摊分区，天然负载均衡；手动 commit offset，只有业务成功才提交
- **六步消费规范**：解析 → 分布式锁 → 幂等校验 → 状态流转 → 业务处理 → 结果落盘

### 自研 Redis 分布式锁

基于 Lua 脚本实现原子操作，WatchDog 协程以 `TTL/3` 间隔自动续期，适配视频处理等长耗时场景，锁释放时校验 Owner 防止误删。

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

每次状态流转都有校验，防止非法跳转（如 Running → Queued），配合唯一索引保证幂等。

## 📄 License

MIT License
