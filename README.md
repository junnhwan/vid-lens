<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/readme-banner-dark.svg" />
  <source media="(prefers-color-scheme: light)" srcset="docs/images/readme-banner-light.svg" />
  <img alt="VidLens — Your video, made readable. Read. Revisit. Ask." src="docs/images/readme-banner-light.svg" width="1440" />
</picture>

**简体中文** · [English](README.en.md)

[在线体验](https://vidlens.wanjune.qzz.io/) · [真实示例](https://vidlens.wanjune.qzz.io/example) · [快速开始](#快速开始) · [工程文档](#工程文档)

![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-18-149ECA?logo=react&logoColor=white)
![TypeScript](https://img.shields.io/badge/TypeScript-3178C6?logo=typescript&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white)
![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)

</div>

VidLens 是一个面向桌面 Web 的视频阅读工具。粘贴 B 站或 YouTube 的单条视频链接，选择模型与解读深度、写下关注点，它会结合字幕或语音转写与真实视频画面，生成带配图、时间引用和内容导图的文章。读完后，可以继续问这个视频，也可以选中一张画面追问。

从教程里梳理操作步骤、从分享里提炼思路，或回到某一帧看清一个细节——让理解不必止于一段摘要。

## 先看一次真实演示

不想先配环境，可以直接打开[真实示例](https://vidlens.wanjune.qzz.io/example)：

| 示例 | 可以体验 |
| --- | --- |
| [从 Grill 到 Retro · B 站 / 09:04](https://vidlens.wanjune.qzz.io/example?video=workflow) | 图文、内容导图、处理回放、文字与选图问答 |
| [Skills v1.3，三个新技能 · YouTube / 14:34](https://vidlens.wanjune.qzz.io/example?video=skills) | 图文、取证过程、带时间引用的追问，以及从真实 PR 画面继续提问 |

示例的文章、配图和答案来自已保存的真实生成结果。处理与问答过程是压缩等待时间后的回放，浏览不会发起新的模型调用。示例是专门整理的公开内容，普通访客的文章和问答仍受匿名会话权限保护。

## 核心能力

- **从链接开始阅读**：支持 B 站单条视频与单个分 P、YouTube 单条视频。优先采用可用平台字幕，不可用时回落语音转写；获取视频不需要提供个人平台 Cookie。
- **选择解读深度与关注点**：提交前选择精简、标准或详细档位（默认标准），选择可用模型、填写可选关注点。三档都以全片主要内容为范围，调整解释深度；文章与后续问答固定同一模型版本，保留当次配置。
- **图文与内容导图**：正文按章节组织，配上实际视频帧、图注与时间引用；内容导图帮助浏览主线并跳转章节。
- **真正看画面的 Agent**：模型接收覆盖整段视频的文字和稀疏概览帧，再按需检索原文、检查时间窗与局部画面，记录观察后整理结果。视觉调查会传入真实图片。
- **文字提问，也能选图追问**：围绕当前视频继续提问，回答可包含文字出处与画面证据；标出无法确认的部分，支持放大图片、回到原视频对应时刻。
- **读完可以带走**：导出 Markdown，或包含原始配图、来源清单和校验清单的 ZIP。导出不包含私有问答与完整转写。
- **看得见的处理过程**：通过 SSE 展示处理阶段与公开步骤；执行状态、工具结果和检查点持久化，刷新页面后可重新读取状态与已保存结果。
- **站长管理与可选联网**：独立后台管理供应商、模型与能力检查，同一型号可在符合条件的供应商间故障切换。可配置公开网页读取、Tavily 搜索及额度，访客每轮主动允许后才会使用外部资料，并区分视频与网页出处。

模型和平台可用性取决于部署环境与供应商。可选联网需要站长另行配置，默认关闭。

## 界面预览

以下截图于 2026-10-16 从桌面 Web 界面实拍；图文与问答使用上述保存的真实示例，界面语言为中文。截图早于三档解读选择的新增；顶部横幅沿用项目现有标识与字标。

### 贴一个链接，带上你的关注点

在首页选择模型、添加关注点，再开始解读。支持浅色、深色及跟随系统主题。

![VidLens 首页：视频链接、模型选择与关注点入口](docs/images/readme-home.jpg)

### 文字有出处，画面能细看

正文配图来自原视频，图注和引用保留时间点；点击画面可以放大，点击时间可以回到视频。

![真实文章：Grill 章节、原视频画面与时间引用](docs/images/readme-evidence.jpg)

### 从一张画面，再多问一句

选中视频里的 PR 截图，追问 Evidence、Door 和 Blast Radius。回答结合实际画面，区分直接观察与解释。

![真实选图问答：选中的 PR 画面、问题与带引用的回答](docs/images/readme-qa.jpg)

<details>
<summary>展开查看：内容导图与处理回放</summary>

**内容导图与章节导航**：先看主线，再跳到想读的章节。

![文章的内容导图与章节导航](docs/images/readme-article.jpg)

**处理回放**：查看已经保存的真实步骤和取证画面；这里展示的是回放结果。

![真实处理回放：公开执行步骤与视频取证画面](docs/images/readme-process.jpg)

</details>

## 从视频到图文，背后发生了什么

![VidLens 系统架构：来源准备、证据调查、图文与问答；Go、PostgreSQL、Redis 与持久化文件](docs/images/readme-architecture.svg)

1. **准备来源**：解析链接，匿名获取视频与可用字幕；必要时转写音频，冻结来源快照与真实时长。
2. **围绕证据调查**：Go / Eino Agent 接收完整可用文字和全视频概览帧，调用原文检索、时间窗阅读、取帧与观察记录工具。
3. **校验后发布**：服务端校验文章结构、引用归属、原文片段、帧时间和图片文件，再保存正式文章。已发布文章保持不可变，后续问答单独保存。
4. **继续阅读与追问**：浏览器读取已保存结果，通过 SSE 接收公开执行步骤；问答限定当前视频和当前匿名会话。

PostgreSQL 保存业务状态、来源、证据与结果；Redis / Asynq 负责投递、资源协调与小型索引缓存；视频缓存和正式配图由应用持久化目录保存。API 与分队列 Worker 运行在一个 Go 进程中。

当前检索使用时间范围、关键词与全视频覆盖，向量检索尚未启用。校验保证的是结构与证据引用约束，模型解释本身仍可能有误。详见[执行与恢复](docs/backend/execution.md)。

## 快速开始

### 环境要求

| 组件 | 要求 |
| --- | --- |
| Go | 1.26+；仅容器运行后端时无需在宿主安装 |
| Node.js | 22+，用于前端开发，也可供原生 yt-dlp 执行 YouTube JavaScript |
| 数据服务 | PostgreSQL 17、Redis 7；仓库提供 Docker Compose |
| 媒体工具 | FFmpeg / ffprobe、`yt-dlp[default]==2026.8.19`；YouTube 还需要 Deno 2.3+ 或 Node 22+ |
| 模型服务 | Chat Completions 兼容接口，支持图片与流式工具调用 |
| ASR 服务 | 同步 OpenAI 兼容 multipart 音频转写接口 |

Windows 请使用 Linux / Docker 运行后端，当前资源管理代码使用 Unix 系统调用。后端镜像已包含媒体工具、匹配的 yt-dlp / EJS 及 Deno。

### 1. 启动依赖，填写配置

以下命令面向新环境；已有配置时保留原文件，端口已占用时先调整 Compose 与配置。

```sh
git clone https://github.com/junnhwan/vid-lens.git
cd vid-lens

docker compose -p vidlens-dev -f compose.backend.yaml up -d postgres redis
mkdir -p .local
cp config.example.yaml .local/backend.yaml
chmod 600 .local/backend.yaml
```

编辑 `.local/backend.yaml`：填写模型和 ASR 地址、型号与 API Key；为 `admin_token` 和 `secret_key` 分别生成独立随机值（至少 32 个字符，例如分别运行 `openssl rand -hex 32`）。原生运行时，把 `data_dir` 改为仓库外已创建、当前用户可写的绝对路径。

YAML 中的产品配置与凭据只初始化新数据库一次，之后通过站长后台修改。请保管好原 `secret_key`，数据库中的加密凭据依赖它。

### 2. 启动后端

原生运行（已安装环境要求中的媒体工具）：

```sh
go run ./cmd/vidlens --config .local/backend.yaml
```

或使用容器，二选一：

```sh
docker compose -p vidlens-dev -f compose.backend.yaml --profile app up -d --build
```

Compose 的默认数据库密码仅供本地开发；其他环境请设置独立凭据，并使用独立数据卷。

### 3. 启动前端

另开终端：

```sh
cd frontend
npm ci
npm run dev -- --host 127.0.0.1 --port 5173 --strictPort
```

打开 <http://127.0.0.1:5173>。Vite 默认代理至后端 `127.0.0.1:8086`。更改端口、设置站长密码、配置模型目录及生产代理，见[启动与配置](docs/backend/startup.md)、[产品能力与后台](docs/backend/product-features.md)和[前端集成](docs/backend/frontend-integration.md)。

## 使用边界

- 面向当前单条视频的解读与问答；不提供本地文件上传、多视频知识库或任意长度视频的处理承诺。长视频受输入、调用、时长与磁盘预算约束，超出范围会明确失败。
- 匿名获取仍可能被上游平台拒绝或要求验证。私有、付费、会员专属与年龄受限内容不保证可用，B 站画质取决于公开接口实际返回的轨道与资源限制。
- 访客使用匿名会话，普通文章与问答不提供公开分享。会话默认自创建起保留 30 天，丢失 Cookie 或会话到期后没有账号找回入口；重要内容建议导出。
- 平台字幕不可用时使用 ASR。若供应商只返回文本，引用按真实音频窗口标注，不伪造逐句时间。已发布正文不会随追问被改写。
- SSE 展示公开步骤与最终结果，不逐 token 展示模型回答；自动化检查不代表每个模型、视频和部署环境均已验证。

## 开发与验证

```sh
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build -o .local/bin/vidlens ./cmd/vidlens

cd frontend
npm test
npm run build
```

前端构建包含 TypeScript 检查。后端集成测试另需设置 `VIDLENS_TEST_DATABASE_URL` 和 `VIDLENS_TEST_REDIS_URL`，指向隔离测试库与 Redis 逻辑 DB；PostgreSQL 测试创建并清理随机 schema。媒体测试依赖 FFmpeg / ffprobe，真实模型与 ASR 验证会产生供应商调用。

生产需要同源静态服务与 API 代理、无缓冲 SSE、私有配置和持久化数据，参考 [nginx 示例](config-examples/nginx.conf.example)。当前仓库没有 GitHub Actions 部署工作流，推送源码不会自动部署。

## 工程文档

| 入口 | 内容 |
| --- | --- |
| [启动与站长配置](docs/backend/startup.md) | 环境、配置初始化、平台获取、密码与权限 |
| [模型管理与导出](docs/backend/product-features.md) | 模型版本、供应商切换、Markdown / ZIP、可选联网 |
| [执行与恢复](docs/backend/execution.md) | 证据工具、发布校验、检查点、资源与文件生命周期 |
| [前端集成](docs/backend/frontend-integration.md) | API、SSE、匿名会话与交互约定 |
| [使用文档](docs/site/index.md) | 阅读、问答、引用、导出与隐私说明 |
| [README 视觉素材](docs/readme-assets.md) | 截图来源、示例归属与视觉素材维护 |

源码入口：`cmd/vidlens/` 为后端命令，`internal/` 为 Go 业务模块，`frontend/` 为 React / TypeScript / Vite 应用。`docs/prototype/` 仅保留静态设计参考。

## 交流与反馈

欢迎通过 [GitHub Issues](https://github.com/junnhwan/vid-lens/issues) 反馈问题与建议，也可以在 [linux.do](https://linux.do/) 交流使用体验。请附上复现步骤与可公开的错误信息，避免提交 API Key、Cookie 或私有问答。

## Star History

<a href="https://www.star-history.com/?repos=junnhwan%2Fvid-lens&type=date&legend=top-left">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&theme=dark&legend=top-left" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&legend=top-left" />
    <img alt="VidLens Star History" src="https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&legend=top-left" />
  </picture>
</a>
