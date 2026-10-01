<div align="center">

# 映知 · VidLens

**观之以映，释之以知**

让长视频既能被追问，也能被学成笔记。

**简体中文** · [English](README.en.md)

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React + Vite](https://img.shields.io/badge/React%20%2B%20Vite-SPA-315e48?logo=react)](https://vite.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL%20%2B%20pgvector-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org)

</div>

映知是一个基于 Go 与 Vite + React 的长视频理解与学习平台。上传视频或导入链接后，系统异步产出语音转写、画面 OCR 与画面描述三类证据并建立索引；用户可以直接提问，也可以让 Agent 检索片段、按需抽帧复核画面，再把内容整理成版本化的学习笔记，并沿对话持续修订。

## 核心能力

- **分片上传与断点续传**：Redis 记录分片状态、MinIO 服务端合并，大文件中断后只补传缺失分片；MD5 命中已完成对象时直接复用。
- **RabbitMQ 异步调度**：转写、摘要生成、画面分析与索引构建按阶段调度，配合手动确认、退避重试、租约与幂等控制，复用已完成阶段和 ASR 分片。
- **分段摘要与逐层合并**：长音频按重叠窗口切分、有界并发转写并对齐去重；超出上下文时逐层合并分段摘要，中间结果持久化，失败后复用已完成分段而不整段重做。
- **混合检索**：pgvector 向量与 Go 内实现的 BM25 关键词并行召回，经查询改写与 RRF 融合后重排，为问答筛选相关片段并返回引用证据。
- **Agent 工具调用循环**：按 ReAct 思路规划下一步，调用转写检索、相邻窗口读取与视觉证据工具，逐步收敛后生成带引用的回答；未被工具实际观测到的引用一律拒绝。
- **按需抽帧与视觉分析**：在已定位的时间窗内抽帧交给视觉模型补充画面证据，分析结果按帧与调查目标缓存，重复调查直接复用而不重复调用模型。
- **学习笔记与 Agent 修订**：内容整理成结构化笔记，Agent 以区块级补丁完成纠错与结构调整；事务 Outbox、操作幂等与检查点恢复保证进程崩溃后不产生重复版本。摘要同样支持带锚点的改名与术语纠正，不重新生成全文。
- **执行预算与恢复**：按 Profile 配置调用、时间、Token 与视觉预算并为最终回答预留额度；执行记录持久化，断流后查询状态与已保存结果，不自动重复执行。
- **跨会话偏好记忆**：用户显式授权后保存可撤回的回答偏好（语言、详略、格式），按对话中的显式表述识别、经 Outbox 异步落库并在后续会话注入；默认关闭，需在配置中启用。
- **调用治理与限流**：按用户配置 ASR / LLM / Embedding / Vision，密钥以 AES-256-GCM 加密保存；Redis + Lua 单脚本原子校验用户、操作、供应商与模型四个维度的速率上限，并区分实际用量与估算。
- **数据边界与可观测**：PostgreSQL 保存业务状态与执行记录，pgvector 作为可重建检索投影，MinIO 保存媒体；检索、工具调用和结果发布均校验用户与知识库成员范围；结构化日志、Prometheus 指标与 Grafana 面板之外，另有 `cmd/rag-eval`、`cmd/rag-audit`、`cmd/rag-reindex` 三条评测与审计命令。

## 系统架构

![映知 · 系统架构](docs/images/readme-architecture.svg)

## 界面预览

**Agent 规划与执行：每一步先给出规划理由，再调用工具，并可展开工具调用详情。**

![Agent 逐步规划与工具调用](docs/images/readme-agent-plan.png)

**回答与引用：引用卡保留时间范围与证据模态，可直接回放对应片段。**

![回答中的时间引用与回放](docs/images/readme-chat-citation.png)

**知识画布：节点组织概念，右侧列出每个结论的关联依据并可回到原视频。**

![学习笔记的知识画布与关联依据](docs/images/readme-artifact-canvas.png)

<details>
<summary>执行用量与回答形态</summary>

**运行详情：累计耗时、工具调用与上限、模型与检索调用、输入输出 Token 和逐步耗时。**

![Agent 运行详情与预算用量](docs/images/readme-agent-budget.png)

**深入分析的回答：按要点组织，结论来自检索到的转写证据。**

![深入分析的回答正文](docs/images/readme-agent-answer.png)

</details>

<details>
<summary>视频详情与多模态证据</summary>

**转写时间轴：按解说转写、画面 OCR、画面描述着色，点击跳到对应时刻。**

![视频详情的转写时间轴](docs/images/readme-video-transcript.png)

**画面证据：关键帧与对应的画面描述、屏幕文字。**

![视频详情的画面证据](docs/images/readme-video-visual.png)

**任务：视频处理按入库、转写、画面分析、检索索引四阶段推进，学习笔记生成同列。**

![处理任务的四阶段进度](docs/images/readme-tasks.png)

</details>

<details>
<summary>学习笔记、思维导图与画布</summary>

**学习笔记：章节化正文，每段可查看依据、围绕这段提问或让 Agent 修改这段；页面自带「待核对」与抽样声明。**

![学习笔记正文与依据入口](docs/images/readme-artifact-note.png)

**思维导图：同一份笔记内容渲染成图，不是另一份文档。**

![学习笔记的思维导图视图](docs/images/readme-artifact-map.png)

</details>

<details>
<summary>工作台、问答范围与设置</summary>

**工作台：继续上次学习的视频，以及资料、会话与待处理统计。**

![首页工作台](docs/images/readme-dashboard.png)

**工作台：最近会话与最近成果。**

![最近会话与最近成果](docs/images/readme-dashboard-recent.png)

**问答范围：视频库问答、知识库问答与单视频问答三种入口。**

![问答范围选择](docs/images/readme-chat-scope.png)

**知识库：按主题限定问答与检索的范围。**

![知识库列表](docs/images/readme-knowledge-bases.png)

**设置：外观主题，以及一份 AI 配置中对话、视觉、语音识别、向量检索与重排序各自的模型。**

![AI 服务设置](docs/images/readme-settings-ai.png)

</details>

## 技术栈与启动

**Go · Gin · GORM · PostgreSQL / pgvector · Redis · RabbitMQ · MinIO · FFmpeg / yt-dlp · Vite + React**

准备 Go 1.24+、Node.js 20+、Docker Compose、FFmpeg 和 yt-dlp。复制 `.env.example` 为 `.env`，按环境填写连接信息与密钥；登录后可在「设置 → AI 服务」配置模型。缺少 OCR 二进制时，画面 OCR 阶段自动跳过，仅保留转写与画面描述。

```bash
# 仓库根目录：启动依赖与后端
cp .env.example .env   # 首次配置；已有 .env 时跳过
# 编辑 .env，配置独立稳定的 VIDLENS_JWT_SECRET / VIDLENS_API_KEY_SECRET 后继续
docker compose up -d
go run ./cmd/server
```

另开终端启动前端：

```bash
cd frontend
npm ci
npm run dev -- -p 5173
```

访问 `http://127.0.0.1:5173`；后端健康检查为 `http://127.0.0.1:8080/healthz`。Windows 配好环境后也可使用 `make start` / `make status`，启动脚本会检查本地数据目录。

## Star History

<a href="https://www.star-history.com/?repos=junnhwan%2Fvid-lens&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&legend=top-left" />
 </picture>
</a>

## 社区
本项目的发布与讨论都在 [linux.do](https://linux.do/) —— 使用问题、踩坑经验、改进建议都欢迎到那里聊。提 issue 也可以，但在社区里通常回得更快。

## 工程文档

[架构总览](docs/architecture/overview.md) · [检索链路](docs/architecture/retrieval.md) · [执行与恢复](docs/architecture/agent-streaming-contract.md) · [证据与引用](docs/architecture/agent-evidence.md) · [笔记编辑契约](docs/architecture/artifact-editing-contract.md) · [笔记生成契约](docs/architecture/study-generation-v3-contract.md) · [媒体理解流水线](docs/architecture/media-understanding-pipeline.md) · [可靠性与幂等](docs/architecture/reliability.md) · [偏好记忆](docs/architecture/agent-memory.md) · [反馈与产品回归](docs/eval/product-feedback.md) · [文档导航](docs/README.md)
