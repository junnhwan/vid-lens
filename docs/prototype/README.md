# 映知 · 产品 UI 高保真原型

> 状态：交互原型，仅用于评审前端 UI 形态，不参与生产构建。
> 打开方式：直接双击 `index.html`，无需构建和依赖；演示视频为本地资产，离线可完整演示。

## 原型覆盖的页面

| 路由 | 页面 | 演示重点 |
|------|------|----------|
| `#/dashboard` | 工作台 | 提问式首页、处理中任务、最近视频与会话 |
| `#/library` | 视频库 | 视频过滤、转写进度、失败重试状态 |
| `#/kb` | 知识库 | 跨视频问答入口、成员视频概览 |
| `#/video/12` | 视频工作台 | 播放器、多模态时间轴、画面证据帧和索引状态 |
| `#/chat/v/12` | 单视频问答 | Chat / Agent、流式回答、引用回放和执行步骤 |
| `#/chat/kb/3` | 知识库问答 | 授权成员范围内的 Chat / Agent 与跨视频引用 |
| `#/settings` | 设置 | BYOK AI 服务配置和记忆治理 |

## 当前交互重点

1. Chat 以问题、检索命中、回答引用和视频时间跳转为核心路径。
2. Agent 展示公开规划摘要、工具步骤、回答、citations 和运行状态。
3. 视频工作台将解说转写、画面 OCR、画面描述放在同一条可回放时间线上。
4. 引用详情展示模态、毫秒范围、anchor quote 和 source refs，并支持跳转播放器。
5. 知识库页面展示授权成员视频、跨视频命中和按视频聚合的证据。
6. 设置页面展示 AI provider、模型、限流和长期记忆策略。

## 原型与后端对应关系

| 原型交互 | 后端事实 |
|---|---|
| Chat | `POST .../messages/stream`，按当前视频或知识库作用域执行标准检索和回答 |
| Agent | `POST .../messages/agent` 或 `/agent/stream`，使用有界 Planner/Tool/Observe 循环 |
| Agent 视觉工具 | `search_visual_evidence` / `inspect_visual_window` 读取视觉索引；`investigate_visual` 在服务端确认的 seed windows 内按预算抽帧并调用 VLM |
| 引用回放 | 引用携带 modality、毫秒范围、anchor quote 和 source refs；`GET /media/task/:id/playback` 返回站内流地址，`/timeline` 支撑时间轴 |
| 视频流定位 | `GET /media/task/:id/stream` 输出支持 Range 的视频字节，播放器按毫秒参数定位 |
| 转写状态 | 分片进度、worker 并发、重试预算和已完成分片复用由转写 consumer 与任务状态接口提供 |
| 知识库范围 | Chat 与 Agent 均使用服务端确认的知识库成员任务集合 |

## 原型与真实实现的差异

- 原型使用本地演示数据；生产页面使用 API 返回的任务、会话、引用和运行状态。
- 原型以模拟节奏展示步骤；生产 Agent 流式执行使用真实 SSE 事件。
- 原型中的检索命中卡会补充视觉化时间列；生产时间和模态字段以 citation/source refs 为准。
- 原型播放器使用本地视频；生产播放器使用对象存储流地址和 Range 请求。
- 原型用于表达交互结构，生产执行仍受服务端作用域、工具白名单、预算、租约和状态机约束。
