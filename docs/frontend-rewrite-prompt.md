# 前端重写提示词 — VidLens（映知）AI 视频理解平台

> 交给前端工程师 AI 的输入。本提示词基于一个**已实现且稳定的 Go 后端**，前端做完整重写。后端 API 已就绪，前端只需消费。

---

你是一个资深前端工程师。请为一个已有后端的 AI 视频理解平台 **VidLens（映知）** 重写整个前端。这是一个**多页面应用**，不是单页面 demo。请分两步交付：**第一步先出 mockup（线框图/静态 HTML）让我确认布局，确认后再进入第二步实现**。

## 1. 项目目标

- **我要做的是**：一个 AI 长视频理解与可追溯问答平台的前端。用户上传视频 → 后端异步做 ASR 转写 / LLM 摘要 / RAG 向量索引 → 用户基于转写内容做带引用的问答。前端是验证与使用界面。
- **目标用户是**：需要把长视频（讲座/教程/会议）变成可检索知识库的人。重视信息密度与可追溯性，不喜欢花哨但空洞的 UI。
- **使用场景是**：上传一个视频后等它处理完，然后在聊天里问视频相关问题，回答带 [Cx] 引用可点开看转写原文片段；或把多个视频组成知识库跨片问答。
- **最小可用版本 MVP 只需要包含**：
  1. 视频库页：任务列表 + 上传入口（直传文件 / 粘贴 URL）+ 任务详情面板（展示 ASR/摘要/RAG 索引三阶段进度状态）
  2. RAG 问答页：选单个视频任务 → 流式打字回答 + [Cx] 引用卡片可展开看转写片段
  3. 知识库问答页：跨视频会话（KB scope，自动走严格 RAG 路径检索集合内多视频）
  4. AI Profile 配置页：BYOK——填写 ASR/LLM/Embedding 三类 profile 的 endpoint/key/model，点测试连通
- **暂时不要实现**：
  - 视频播放器、字幕同步、时间轴跳转（不做时间码可视化，citation 只显示文本 + chunk_index）
  - Agentic 工具循环模式（`/messages/agent` 端点存在但实验性，不在 MVP）
  - 分片上传的断点续传 UI（直传 + URL 上传够了；分片上传后端支持但 MVP 不做 UI）
  - 多语言/i18n（先中文）
  - 暗黑/亮色双主题切换（先做一个主题，另一个后续）

## 2. 技术栈与项目约束

- **框架选择**：React 18（用 Next.js 14 App Router，因为它内置路由 + SSR 适配后端 serve）
- **构建工具**：Next.js 内置
- **语言**：TypeScript
- **样式方案**：Tailwind CSS（让 AI 自己定设计系统 token：色板/间距/字号/圆角/阴影，集中在一处）
- **输出形式**：完整可跑的 Next.js 项目目录（`app/` `components/` `lib/` 分层），能 `npm install && npm run dev` 启动
- **代码复杂度**：更可维护的分层写法——`lib/api.ts` 封装所有后端调用，`lib/types.ts` 放后端契约类型，`components/` 放可复用组件，`app/` 放页面路由
- **API 代理**：dev 时 Next 代理 `/api → http://localhost:8080`（后端跑在 8080）。在 `next.config.js` 配 rewrites。

## 3. 页面结构

四个主路由：

- **`/`（视频库 Library）**
  - Header：logo + 用户头像/登出
  - Main：任务卡片网格/列表，每张卡片显示视频标题、状态徽标（Pending/Queued/Running/Completed/Failed/Dead）、三阶段子状态（ASR / 摘要 / RAG 索引 各自的进度）、创建时间
  - 顶部操作栏：上传按钮（弹 Modal：直传文件 / 粘贴 URL 两个 tab）
  - 点击卡片 → 右侧或弹层显示任务详情面板（轮询更新进度，完成态显示摘要 + 触发 RAG 索引按钮 + "去问答"入口）
  - Sidebar/Filter：按状态筛选、按来源（上传/URL）筛选、搜索标题
- **`/chat/:taskId`（单视频 RAG 问答）**
  - Header：返回库 + 视频标题 + 模式切换（普通 / strict_rag）
  - Main：消息流，用户气泡 + AI 回答（流式打字效果，SSE），回答里的 [Cx] 引用渲染成可点击小徽标，点击展开下方引用卡片（转写片段原文 + chunk_index + 检索分数 + 来源 source）
  - 底部：输入框 + 发送 + TopK 调节
- **`/kb/:kbId`（知识库跨视频问答）**
  - Header：知识库名 + 管理入口（添加/移除视频）
  - Main：同问答页，但回答来自多视频检索，引用卡片标注来自哪个视频
  - 侧栏：知识库内视频列表
- **`/settings`（AI Profile 配置）**
  - Main：ASR / LLM / Embedding 三类 profile 的 CRUD 列表，每条可编辑（provider / base_url / api_key / model）+ "测试连通"按钮 + "探测 embedding 维度"按钮

## 4. 数据与状态（后端 API 契约，已实现，prefix `/api/v1`）

所有需鉴权的请求带 `Authorization: Bearer <jwt>`。未授权跳登录。

**认证**
- `POST /user/register` body `{username,password}` → `{token,user}`
- `POST /user/login` 同上
- `GET /user/profile` → 当前用户

**AI Profile（BYOK）**
- `GET /ai/profiles` → 列表
- `POST /ai/profiles` body `{provider,base_url,api_key,model,type}` → 创建
- `PUT /ai/profiles/:id` / `DELETE /ai/profiles/:id`
- `POST /ai/profiles/test` body profile → 测试连通结果
- `POST /ai/profiles/models` → 列出该 provider 可用模型
- `POST /ai/profiles/embedding-dim` → 探测维度

**媒体任务**
- `POST /media/upload` multipart file → `{task_id}`
- `POST /media/upload-url` body `{url}` → `{task_id}`
- `GET /media/list?...` → 任务列表
- `GET /media/task/:id` → **任务详情，含三阶段子状态**（前端轮询此端点更新进度，建议 2-3s 间隔，运行态才轮询，完成态停）
- `DELETE /media/task/:id`
- `POST /media/transcribe/:id` → 触发 ASR
- `POST /media/analyze/:id` → 触发摘要
- `GET /media/task/:id/rag-index` → RAG 索引状态
- `POST /media/task/:id/rag-index` → 触发 RAG 索引

**任务状态机**：`Pending → Queued → Running → Completed | Failed | Dead`。子阶段 ASR/摘要/RAG 各自独立走这个状态机。

**Chat（核心）**

**两个正交维度，别混**：
- **ScopeType**（会话属性，创建 session 时定）：`video`（单视频，带 task_id）/ `knowledge_base`（跨视频，带 kb_id）。KB scope 自动走严格 RAG。
- **ChatMode**（请求参数，每条 message 可带）：`video_assistant`（默认宽松问答）/ `strict_rag`（强制 RAG，无索引/无上下文 fail closed）。
- 有意义组合：① 单视频 + video_assistant（默认）② 单视频 + strict_rag（强制检索）③ 知识库（KB scope，自带严格 RAG 语义，mode 无所谓）。

- `POST /chat/sessions` body `{task_id?, mode, kb_id?, scope_type?}` → `{session_id}`（scope_type 不传时：有 task_id 默认 video，有 kb_id 默认 knowledge_base）
- `GET /chat/sessions` → 会话列表
- `GET /chat/sessions/:id/messages` → 历史
- `POST /chat/sessions/:id/messages` body `{question, top_k?, mode?}` → 同步回答 + citations
- **`POST /chat/sessions/:id/messages/stream`** → **SSE 流式**。Content-Type `text/event-stream`，事件格式 `event: <type>\ndata: <json>\n\n`。事件 type 包括：`token`（增量 token）、`citation`（引用片段，结构见下）、`done`（完成）、`error`。前端按 type 分发渲染。
- `DELETE /chat/sessions/:id`

**Citation 结构（`/messages` 同步返回的 `citations` 数组，SSE 的 `citation` 事件 data 同形）**：
```ts
type Citation = {
  task_id: number
  video_title?: string
  evidence_id: string
  chunk_id: number
  chunk_index: number      // 用于显示"片段 #N"
  score: number
  content: string           // 转写原文片段
  anchor_content?: string   // 锚点片段
  source?: string           // vector | hybrid | keyword
  final_rank?: number
  rerank_score?: number
}
```
注意：**citation 没有时间码**（start/end second）。不要做时间跳转，只显示文本 + chunk_index + score + source 徽标。

**知识库**
- `POST /knowledge-bases` / `GET /knowledge-bases` / `GET /knowledge-bases/:id` / `DELETE /knowledge-bases/:id`
- `POST /knowledge-bases/:id/videos` body `{task_id}` / `DELETE /knowledge-bases/:id/videos/:task_id`

**页面状态需处理**：
- 加载中（骨架屏，不要转圈）
- 空结果（任务库空 / 搜索无结果 / 问答无引用）
- 错误（网络失败 / 401 跳登录 / 5xx 显示重试）
- SSE 断连重连
- 移动端窄屏（列表变单列，详情面板变全屏抽屉）

## 5. 交互需求

用户可以：
1. 登录/注册 → 进入视频库
2. 上传视频（文件或 URL）→ 看到任务卡片出现并自动轮询进度直到 Completed
3. 点任务卡片 → 看详情 + 摘要；Completed 后点"去问答"
4. 在问答页输入问题 → 看到流式回答逐字出现 + [Cx] 引用徽标；点引用展开原文片段
5. 切换 strict_rag 模式（无索引时 fail closed，UI 要友好提示"该视频尚未建立索引"）
6. 创建知识库 → 添加多个视频 → 跨视频问答，引用标注来源视频
7. 在设置页配置 AI profile → 测试连通 → **设为默认**（无独立端点，通过 `PUT /ai/profiles/:id` 带 `is_default: true` 实现，后端会自动把同类其他 profile 的 is_default 置 false；Create 时若该类无默认 profile 会自动设为默认）

交互完成后，页面应该：状态徽标实时更新、流式回答流畅不打架、引用展开有过渡动效、错误有明确文案 + 重试入口。

## 6. 视觉与体验

- **整体风格**：**不指定具体风格，请你作为前端工程师自行定**。但给约束：这是一个"内容密集型工具"（任务列表 + 转写文本 + 引用卡片），不是营销页。请优先**信息密度、可读性、状态可追溯**，不要为大留白牺牲信息量。气质偏"专业工具"而非"消费级 App"。
- **布局要求**：桌面端列表/详情可并列；移动端变单列 + 抽屉。问答页桌面端消息流居中限宽（~720px），移动端全宽。
- **颜色/字体偏好**：你定。但要求一个清晰的语义色系统（success/warning/error/neutral + 一个主色），集中在 Tailwind config token。
- **可访问性**：按钮可键盘聚焦、文字对比度达 AA、input 有 label、SSE 流式回答有 `aria-live="polite"`。
- **动效**：克制。流式打字、引用展开、状态徽标变化这些用微动效；不要全页进场动画。

## 7. 输出与验收

### 第一步：Mockup（先交付这个，等我确认）
- 输出四个页面的**静态 HTML 线框图**（可以是单个 HTML 文件用 Tailwind CDN，或 Next.js 静态页），用真实占位数据渲染，展示布局、信息层次、状态变体（空/加载/有数据/错误）。
- 不要写 API 调用逻辑，先把"长什么样"定下来。
- 说明：你为每个页面做了哪些布局决策、为什么。

### 第二步：实现（mockup 确认后）
- 输出完整 Next.js 项目目录（`app/` `components/` `lib/api.ts` `lib/types.ts` `next.config.js` `tailwind.config.ts` `package.json`）
- 说明：如何 `npm install`、`npm run dev` 启动、如何配 `/api → :8080` 代理
- 验收标准：
  1. `npm run dev` 能起，无控制台报错
  2. 视频库能列任务、能上传、能轮询进度到完成
  3. 问答页能流式打字、能展开引用卡片
  4. 知识库页能跨视频问答
  5. 设置页能配 profile + 测试连通
  6. 移动端布局不溢出
  7. 关键部分有简短注释，`lib/api.ts` 是唯一后端调用出口

---

## 附录：后端技术事实（前端需知）

- 后端 Go + Gin，跑在 `:8080`，健康检查 `/healthz` `/readyz`
- 前端 dev 时代理 `/api/v1 → :8080`；生产时后端会 serve 前端 build 产物（路径后续约定）
- SSE 端点返回标准 `text/event-stream`，用 EventSource API 接
- 任务进度靠轮询 `GET /media/task/:id`，非 WebSocket
- citation 无时间码，别做时间跳转
- strict_rag 模式无索引会 fail closed（4xx），UI 要友好处理
