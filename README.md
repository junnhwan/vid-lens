# VidLens - 智能视频内容理解平台

> Go 后端复刻版，基于原 Java 项目 DOVideo-AI 的设计思路，用 Go 从零实现并完善。

## 项目简介

VidLens 是一个集成用户鉴权、视频上传、音频提取及 AI 自动总结的全链路视频内容理解平台。

针对视频处理场景中常见的**「长耗时阻塞」**、**「高并发资源冲突」**以及**「大文件传输不稳定」**等痛点，本项目基于 **Asynq + Redis 分布式锁 + 分片续传** 构建了完整的异步架构。

## 技术栈

| 层级 | 技术选型 |
|------|---------|
| HTTP 框架 | Gin |
| ORM | GORM |
| 任务队列 | Asynq (基于 Redis) |
| 缓存/锁 | go-redis |
| 对象存储 | MinIO (私有桶 + Pre-signed URL) |
| 数据库 | MySQL 8.0 |
| AI | 硅基流动 (ASR + DeepSeek) |
| 音视频处理 | FFmpeg |

## 核心架构

```
客户端
  │
  ├── 分片上传 ──→ Redis(分片状态) ──→ MinIO(合并)
  │
  ├── AI分析请求 ──→ 令牌桶限流 ──→ Asynq队列 ──→ 即时返回(50ms)
  │                                              │
  │                                     ┌────────┴────────┐
  │                                  消费者拉取任务    死信队列兜底
  │                                     │
  │                              分布式锁(MD5)
  │                                     │
  │                            FFmpeg → ASR → LLM
  │                                     │
  │                            MySQL落盘(状态机)
  │                                     │
  └── 轮询任务状态 ←──────────────────┘
```

## 快速开始

### 1. 启动中间件

```bash
docker-compose up -d
```

### 2. 修改配置

编辑 `config.yaml`，填入你的 AI API Key：

```yaml
ai:
  siliconflow_api_key: "sk-your-key-here"
```

### 3. 启动服务

```bash
go run cmd/server/main.go
```

## 项目结构

```
vid-lens/
├── cmd/server/          # 入口
├── internal/
│   ├── ai/              # AI 策略（ASR + LLM）
│   ├── config/          # 配置加载
│   ├── handler/         # HTTP 处理层
│   ├── middleware/       # 中间件（CORS/JWT/限流）
│   ├── model/           # 数据模型
│   ├── mq/              # 消息队列（Asynq 生产者+消费者）
│   ├── pkg/             # 内部工具包
│   │   ├── ffmpeg/      # FFmpeg 封装
│   │   ├── jwt/         # JWT 工具
│   │   ├── lock/        # 分布式锁（看门狗续期）
│   │   └── response/    # 统一响应
│   ├── repository/      # 数据访问层
│   ├── service/         # 业务逻辑层
│   └── storage/         # MinIO 存储
├── config.yaml
├── docker-compose.yml
└── README.md
```

## 面试亮点

1. **Asynq 异步削峰**：投递即返回，接口 RT < 50ms；原生指数退避重试 + 死信队列
2. **自研 Redis 分布式锁**：Lua 脚本 + WatchDog 自动续期，适配长耗时视频处理
3. **Lua 令牌桶限流**：惰性计算 + 原子扣减，遏制恶意请求
4. **分片上传 + 断点续传**：Redis Set 记录状态，先落盘后记账
5. **MD5 内容级去重**：秒传 + 唯一索引物理兜底
6. **Pre-signed URL**：MinIO 私有桶，5 分钟签名有效期
7. **垂直拆分**：转录文本和 AI 总结独立建表，保护主表查询性能
