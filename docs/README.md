# VidLens 工程文档

本目录保留项目的公开工程资料、产品规划与评审原型，不放简历、面试准备、个人信息或本地工作记录。规划和静态原型会明确区分已有实现与待接入能力。

## 文档导航

### 系统结构与边界

- [架构总览](architecture/overview.md)
- [数据模型与存储边界](architecture/data-model.md)
- [检索与回答链路](architecture/retrieval.md)
- [问答事实与时间边界](architecture/qa-grounding.md)
- [视频理解管线](architecture/media-understanding-pipeline.md)
- [知识库工作区](architecture/knowledge-workspace.md)
- [在线协议与执行边界](architecture/compatibility.md)
- [聊天前端与后端边界](architecture/frontend-chat-agent-gaps.md)
- [可靠性与幂等](architecture/reliability.md)

### Agent 执行与契约

- [Agent 流式契约（UI × 后端）](architecture/agent-streaming-contract.md)
- [Chat 与自主 Agent](architecture/agent-evolution.md)
- [长期记忆与回答偏好](architecture/agent-memory.md)
- [Agent 引用与视觉证据](architecture/agent-evidence.md)
- [会话执行计时契约](architecture/conversation-timing-contract.md)
- [视觉处理进度契约](architecture/visual-progress-contract.md)

### 笔记（Artifact）契约

- [Artifact API 首版契约](architecture/artifact-api-contract.md)
- [笔记编辑 R1 契约](architecture/artifact-editing-contract.md)
- [可编辑知识画布 R3 契约](architecture/artifact-canvas-contract.md)
- [学习笔记生成配方 v2](architecture/study-generation-v2-contract.md)
- [学习笔记生成配方 v3](architecture/study-generation-v3-contract.md)

### 部署与运维

- [GitHub 自动部署](automatic-deployment.md)
- [Free API 托管接入](hosted-ai.md)
- [运维资料](operations/README.md) · [压测与故障演练](operations/stress-testing.md)

### 评测

- [评测资料](eval/README.md)
- [严格评测标注指南](eval/annotation-guide.md)
- [数据集 schema](eval/dataset-schema.yaml)
- [回答反馈与产品回归候选](eval/product-feedback.md)

### 图示与原型

- [架构图](images/readme-architecture.svg) · [English](images/readme-architecture.en.svg)
- [架构图 HTML 源文件](images/readme-architecture.html) · [English](images/readme-architecture.en.html)
- [产品 UI 高保真原型](prototype/README.md)
- [视频摘要桌面原型：动态 Agent 活动](prototypes/summary-experience-desktop-v2/README.md)

### 视频摘要体验改造（规划）

- [产品规划](planning/2026-10-09-summary-product-plan.md)
- [实现与验收指南](planning/2026-10-09-summary-implementation-guide.md)
- [桌面原型设计与交互要求](design/2026-10-09-frontend-prototype-brief.md)

## 目录约定

- `architecture/`：当前系统结构、模块边界、数据流和可靠性约束
- `eval/`：可复现的评测规范、配置和示例数据
- `operations/`：部署、压测和故障处理资料
- `prototype/`：仅用于评审前端 UI 形态的高保真静态原型，不参与生产构建
- `prototypes/`：独立交互评审原型；使用本地模拟数据，不代表业务能力已实现
- `planning/`：待实施的产品方案、工作包与验收条件
- `design/`：前端设计、交互要求与后续实现参考
- `images/`：README 和架构文档使用的图片资源
- 根目录下的 `automatic-deployment.md`、`hosted-ai.md` 是单篇运维与功能说明

个人材料、未定稿内容和当前本地评测输入放在仓库根目录的 `docs-private/`，该目录不会提交到 Git。
