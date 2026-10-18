<div align="center">

# VidLens · 映知

**让视频变得可读：阅读 · 回看 · 追问**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./docs/images/readme-banner-dark.svg" />
  <source media="(prefers-color-scheme: light)" srcset="./docs/images/readme-banner-light.svg" />
  <img alt="VidLens — Your video, made readable. Read. Revisit. Ask." src="./docs/images/readme-banner-light.svg" width="1440" />
</picture>

**简体中文** · [English](README.en.md)

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev) [![React 18 · Vite 5](https://img.shields.io/badge/React-18%20%2B%20Vite%205-61DAFB?logo=react&logoColor=black)](https://vite.dev) [![PostgreSQL 17](https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org) [![Redis 7 · Asynq](https://img.shields.io/badge/Redis-7%20%2B%20Asynq-DC382D?logo=redis&logoColor=white)](https://github.com/hibiken/asynq) [![FFmpeg](https://img.shields.io/badge/FFmpeg-media%20processing-007808?logo=ffmpeg&logoColor=white)](https://ffmpeg.org)

[在线体验](https://vidlens.wanjune.qzz.io/) · [真实示例](https://vidlens.wanjune.qzz.io/example) · [Linux.do 交流](https://linux.do/) · [本地开发](docs/backend/startup.md)

</div>

VidLens 面向桌面浏览器，把 B 站或 YouTube 视频整理成带章节、真实画面、时间出处和内容导图的图文文章。你可以选择解读深度与关注点，阅读时回看来源，也可以继续提问。

## 先看真实示例

| 示例 | 可以体验 |
| --- | --- |
| [从 Grill 到 Retro · B 站 / 09:04](https://vidlens.wanjune.qzz.io/example?video=workflow) | 图文文章、内容导图、处理回放和问答 |
| [Skills v1.3，三个新技能 · YouTube / 14:34](https://vidlens.wanjune.qzz.io/example?video=skills) | 取证过程、时间引用和画面追问 |

这些是已保存的真实结果；浏览示例不会发起新的模型调用。

## 实际处理与阅读界面

以下处理、文章和问答截图来自一条 10:08 的 YouTube 实测视频。流程进度和用时只代表截图捕获时的界面状态，不构成时长承诺。

### 提交视频

![VidLens 首页：粘贴视频链接并选择模型与解读深度。](docs/images/readme-demo-home.png)

### 查看处理阶段与计划

![VidLens 处理页面：查看当前阶段、执行计划和全片概览画面。](docs/images/readme-process-plan.png)

### 准备文字来源与画面证据

![VidLens 来源准备记录：检查平台字幕、获取视频并生成覆盖全片的概览帧。](docs/images/readme-process-source-detail.png)

### 分批撰写文章

![VidLens 处理进度：12 章计划中已保存 6 章，Agent 正在继续撰写。](docs/images/readme-process-writing-progress.png)

### 阅读带时间出处的文章

![VidLens 图文文章：正文包含时间引用，并配有真实视频画面。](docs/images/readme-article-detail.png)

### 浏览章节内容导图

![VidLens 已生成的图文文章页面：显示章节内容导图和导出、回看处理、视频追问入口。](docs/images/readme-article-map.png)

### 选中内容继续追问

![VidLens 视频问答：选中文章内容后，在侧栏查看带时间出处的回答和对应视频画面。](docs/images/readme-frame-qa.png)

## 核心能力

- **从视频开始**：支持 B 站单条视频、单个分 P 和 YouTube 单条视频；优先使用字幕，缺失时进行语音转写，不需要个人平台 Cookie。
- **带出处阅读**：文章按章节组织，包含真实画面、时间引用和内容导图；可导出 Markdown 或图文 ZIP。
- **围绕证据提问**：Agent 根据视频文字和画面查找依据；可追问文章、段落或选中画面，并标出来源和未确认之处。
- **查看实际进度**：页面展示来源准备、取证、写作和校验过程，已保存的步骤可以回看。服务启用时，也可对单轮问答选择补充公开网页资料。
- **分享单条记录**：创建可取消的只读链接，让其他人查看这一次的图文与处理过程；私人追问保持私有。

## 架构概览

这张图概括了从视频链接到图文阅读的流程，以及当前应用服务、Agent、队列和持久化存储之间的关系。

![VidLens 架构图：视频来源准备、Agent 证据调查、文章发布与当前服务组件。](docs/images/readme-architecture.svg)

## 适合什么视频

技术讲解、科普、课程和操作教程等讲述清晰的视频更适合整理成图文。音乐、风景或纯剪辑视频可供整理的信息较少；静态画面也无法完整表达连续动作、节奏或剧情。结果会受字幕、转写、画面清晰度和模型服务影响。详见[选片建议](docs/site/start.md)。

## 本地开发

需要 Go 1.26+、Node.js 22+、PostgreSQL 17、Redis 7 和 FFmpeg；Windows 上运行后端需使用 Linux/Docker 环境。完整步骤见[启动与配置](docs/backend/startup.md)。

## 文档

[使用指南](docs/site/index.md) · [隐私与保留](docs/site/privacy.md) · [Agent 执行与恢复](docs/backend/execution.md)

## 社区与反馈

VidLens 的发布与讨论在 [Linux.do](https://linux.do/)；欢迎分享体验、提问和交流建议。技术问题与可复现的缺陷也可以提交到 [GitHub Issues](https://github.com/junnhwan/vid-lens/issues)。

网站面向桌面浏览器，访客无需注册；文章和问答由当前匿名会话持有。主动分享单条记录后，获得链接的人可只读查看该条图文与处理过程，私人追问不随链接公开。详情见[隐私与保留](docs/site/privacy.md)。

## Star History

[![VidLens Star History](https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&legend=top-left)](https://www.star-history.com/?repos=junnhwan%2Fvid-lens&type=date&legend=top-left)
