# VidLens 前端实现交接文档

> 本文档交接给新会话实现 VidLens 前端。读本文档 + `docs/frontend-mockup/*.html` 即可开干，不需要读原对话历史。
> 后端已实现稳定，只做前端。Mockup 已确认，进入实现阶段。

## 1. 任务概述

为 **VidLens（映知）**——一个 AI 长视频理解与可追溯问答平台的 Go 后端——重写整个前端。用户上传视频 → 后端异步做 ASR 转写 / LLM 摘要 / RAG 向量索引 → 用户基于转写内容做带引用的问答。前端是验证与使用界面。

**输出**：完整可跑的 Next.js 14 (App Router) + TypeScript + Tailwind 项目，能 `npm install && npm run dev` 启动，dev 时代理 `/api → http://localhost:8080`。

## 2. 已定决策（不要再推翻）

### 2.1 视觉语言（四页统一，已定稿）

四页 mockup 在 `docs/frontend-mockup/`，**以此为准**：

| 页面 | mockup 文件 | 路由 |
|---|---|---|
| 视频库 | `library-b-expandable.html` | `/` |
| 单视频问答 | `chat.html` | `/chat/:taskId` |
| 知识库问答 | `kb.html` | `/kb/:kbId` |
| AI Profile 设置 | `settings.html` | `/settings` |

`index.html` 是索引页，`library.html` / `library-a-detail-page.html` 是旧版/废弃方案，**不要参考**。以 `library-b-expandable.html` 为准。

### 2.2 视觉系统（反 AI 味，已定稿）

- **美学方向**：现代无衬线工具，不花哨，信息密度优先。**不是**衬线/古籍/暖纸（早期版本犯过这个错，已纠正）。气质偏 Linear / Vercel dashboard / Arc 那种"克制但有识别度"。
- **字体**：标题+正文用 **Geist**（几何无衬线），数字/英文技术标签用 **Geist Mono**。中文 fallback 用 **Noto Sans SC**。**禁用** Inter / Roboto / Arial / 系统默认。**禁用** Fraunces / Noto Serif SC（这是早期"书生气"的根源，已删除）。
- **图标**：全程 **Lucide React**，统一 1.5 stroke。**禁止任何 emoji 或 ✓ 字符当图标**。
- **圆角**：直角为主，仅徽章/小标签 2px，按钮 `rounded-md`（6px）。**禁用** `rounded-2xl` 满屏。
- **配色机制**：5 套主题，用 **CSS 变量** 实现。Tailwind config 的颜色 token 引用 `var(--xxx)`，切换只改 `data-theme` 属性。每套是**不同的配色策略**（不是换色）：
  - `sienna`（默认）：冷白 `#FAFAFB` 底 + 墨黑 `#18181B` 文字 + 深琥珀 accent `#9A4A1A` + 苔绿 done `#3F7A4A` + 朱红 fail `#C0392B`
  - `indigo`：冷白 + 深靛 accent + 冷青 done + 朱砂红 fail（冷暖撞）
  - `verdigris`：灰青底 + 暖金 accent（金/绿撞）
  - `rust`：米黄底（更暗更暖）+ 锈红 accent + 深橄榄
  - `ink`（暗色）：暖黑 `#0E0E11` + 米白文字 + 暖金 accent + 冷青 done
  - 5 套主题的**完整 CSS 变量值**见每个 mockup 文件的 `<style>` 块，逐字抄过来即可。
- **主题切换器**：右上角 5 个色块 swatch，点击实时切。用 `data-theme` 属性写在 `<html>` 上，CSS 变量自动级联。无需 JS 拆分颜色（早期噪点方案已删）。
- **质感**：纯色面 + 1px 发丝分隔线（`border-ink-2/20`）。**禁用** 紫色渐变、玻璃拟态 `backdrop-blur`、`ring-1` 双阴影满屏、纸张噪点 SVG。
- **微交互**：行 hover 左侧 3px accent 色指示条滑入（150ms）；按钮 hover 150ms 过渡；Running 进度条流光扫过；脉冲点用 `animation` 不是闪烁。**零进场炫技动画**（不要 fade-in-up 全场）。

### 2.3 布局决策（已确认）

- **视频库（方案 B，已选定）**：点任务卡片 → 右侧 380px 窄面板（看进度）；面板顶部 ⤢ 展开 → 撑到 58% 屏宽，内部上下两块：摘要 + 转写全文。**不新增独立详情页路由**（A 方案已否决）。展开态可把 `?task=:id` 写进地址栏（不跳路由）支持刷新还原。
- **问答页**：消息流居中限宽 820px；左侧 lg 屏 sticky 栏（本视频元信息 / 引用汇总 / 历史会话 / 检索元信息），窄屏隐藏。引用做成**脚注**而非弹层——`[C1]` 是上标小徽标，点击在回答正下方**内联展开**引用卡片（`max-height` 过渡 350ms），不要浮层。
- **用户气泡**：深底浅字，右对齐。**AI 回答**：无气泡底，纯正文左对齐（工具感）。
- **移动端**：列表单列 + 横排筛选 chip；详情面板变全屏抽屉；问答页左栏隐藏。
- **对比度**：AI 回答正文用 `text-ink-0`（全墨）+ `15.5px` + `font-medium`，是页面最重墨色。

### 2.4 状态机

任务：`Pending → Queued → Running → Completed | Failed | Dead`。子阶段 ASR/摘要/RAG 各自独立走这个状态机。中文标签：处理中/排队/已完成/失败/已废弃。

## 3. 后端 API 契约（已实现，prefix `/api/v1`）

所有需鉴权请求带 `Authorization: Bearer <jwt>`。未授权跳登录。后端跑在 `:8080`，健康检查 `/healthz` `/readyz`。

### 3.1 认证
- `POST /user/register` body `{username,password}` → `{token,user}`
- `POST /user/login` 同上
- `GET /user/profile` → 当前用户

### 3.2 AI Profile（BYOK）
- `GET /ai/profiles` → 列表
- `POST /ai/profiles` body `{provider,base_url,api_key,model,type}` → 创建
- `PUT /ai/profiles/:id` / `DELETE /ai/profiles/:id`
- `POST /ai/profiles/test` body profile → 测试连通结果
- `POST /ai/profiles/models` → 列出该 provider 可用模型
- `POST /ai/profiles/embedding-dim` → 探测维度
- **设为默认**：无独立端点，通过 `PUT /ai/profiles/:id` 带 `is_default: true`；后端自动把同类其他 profile 的 is_default 置 false。Create 时若该类无默认 profile 会自动设为默认。

### 3.3 媒体任务
- `POST /media/upload` multipart file → `{task_id}`
- `POST /media/upload-url` body `{url}` → `{task_id}`
- `GET /media/list?...` → 任务列表
- `GET /media/task/:id` → **任务详情，含三阶段子状态**（前端轮询此端点，2-3s 间隔，**仅 Running 态轮询，完成态停**）
- `DELETE /media/task/:id`
- `POST /media/transcribe/:id` → 触发 ASR
- `POST /media/analyze/:id` → 触发摘要
- `GET /media/task/:id/rag-index` → RAG 索引状态
- `POST /media/task/:id/rag-index` → 触发 RAG 索引

### 3.4 Chat（核心）—— 两个正交维度，别混
- **ScopeType**（会话属性，创建 session 时定）：`video`（单视频，带 task_id）/ `knowledge_base`（跨视频，带 kb_id）。KB scope 自动走严格 RAG。
- **ChatMode**（请求参数，每条 message 可带）：`video_assistant`（默认宽松问答）/ `strict_rag`（强制 RAG，无索引/无上下文 fail closed）。
- 有意义组合：① 单视频 + video_assistant ② 单视频 + strict_rag ③ 知识库（KB scope，自带严格 RAG 语义，mode 无所谓）。
- 端点：
  - `POST /chat/sessions` body `{task_id?, mode, kb_id?, scope_type?}` → `{session_id}`（scope_type 不传时：有 task_id 默认 video，有 kb_id 默认 knowledge_base）
  - `GET /chat/sessions` → 会话列表
  - `GET /chat/sessions/:id/messages` → 历史
  - `POST /chat/sessions/:id/messages` body `{question, top_k?, mode?}` → 同步回答 + citations
  - **`POST /chat/sessions/:id/messages/stream`** → **SSE 流式**。Content-Type `text/event-stream`，事件格式 `event: <type>\ndata: <json>\n\n`。事件 type：`token`（增量 token）/ `citation`（引用片段）/ `done` / `error`。前端按 type 分发渲染。用 EventSource API 接。
  - `DELETE /chat/sessions/:id`

### 3.5 Citation 结构（`/messages` 同步返回的 `citations` 数组，SSE 的 `citation` 事件 data 同形）
```ts
type Citation = {
  task_id: number
  video_title?: string
  evidence_id: string
  chunk_id: number
  chunk_index: number      // 用于显示"片段 #N"
  score: number
  content: string           // 转写原文片段
  anchor_content?: string
  source?: string           // vector | hybrid | keyword
  final_rank?: number
  rerank_score?: number
}
```
**注意：citation 没有时间码**（无 start/end second）。**不要做时间跳转**，只显示文本 + chunk_index + score + source 徽标。

### 3.6 知识库
- `POST /knowledge-bases` / `GET /knowledge-bases` / `GET /knowledge-bases/:id` / `DELETE /knowledge-bases/:id`
- `POST /knowledge-bases/:id/videos` body `{task_id}` / `DELETE /knowledge-bases/:id/videos/:task_id`

## 4. 实现约束（PRD 原文要求）

- **框架**：React 18 + Next.js 14 App Router + TypeScript + Tailwind CSS。
- **分层**：`lib/api.ts` 封装所有后端调用（**唯一出口**）；`lib/types.ts` 放后端契约类型；`components/` 放可复用组件；`app/` 放页面路由。
- **API 代理**：dev 时 Next 代理 `/api → http://localhost:8080`。在 `next.config.js` 配 rewrites。
- **MVP 四页**（同 §2.1 表）。
- **暂时不实现**：视频播放器/字幕同步/时间轴跳转；Agentic 工具循环（`/messages/agent` 端点实验性，不做）；分片上传断点续传 UI；多语言/i18n（先中文）；暗黑/亮色双主题切换（已有 5 套主题切换器，但**不**做系统明暗自动跟随，先手动切）。

## 5. 页面状态需处理
- 加载中：骨架屏（`docs/frontend-mockup` 里有 `.sk` 类的实现，shimmer 动画），**不要转圈 spinner**。
- 空结果：任务库空 / 搜索无结果 / 问答无引用——mockup 里有空态样式。
- 错误：网络失败 / 401 跳登录 / 5xx 显示重试。
- SSE 断连重连。
- 移动端窄屏：列表单列，详情面板全屏抽屉。

## 6. 验收标准
1. `npm run dev` 能起，无控制台报错。
2. 视频库能列任务、能上传（文件 + URL）、能轮询进度到完成。
3. 问答页能流式打字、能展开引用卡片。
4. 知识库页能跨视频问答。
5. 设置页能配 profile + 测试连通 + 探测维度 + 设为默认。
6. 移动端布局不溢出。
7. 关键部分有简短注释，`lib/api.ts` 是唯一后端调用出口。
8. 5 套主题切换器可用。
9. 全程 Lucide 图标，零 emoji。

## 7. 交互需求（用户可完成的流程）
1. 登录/注册 → 进入视频库。
2. 上传视频（文件或 URL）→ 看到任务卡片出现并自动轮询进度直到 Completed。
3. 点任务卡片 → 看详情 + 摘要；Completed 后点"去问答"或展开读转写。
4. 问答页输入问题 → 流式回答逐字出现 + [Cx] 引用徽标；点引用展开原文片段。
5. 切换 strict_rag 模式（无索引时 fail closed，UI 友好提示"该视频尚未建立索引" + 触发索引入口）。
6. 创建知识库 → 添加多个视频 → 跨视频问答，引用标注来源视频。
7. 设置页配 AI profile → 测试连通 → 设为默认。

## 8. 视觉细节备忘（从 mockup 抄）

- **进度条**：2-3px 细条 + 实色填充；Running 态用 `.flow` 类（`linear-gradient` 流光 `animation`）；完成用苔绿；排队用灰。
- **Running 脉冲点**：`.live` 类（`opacity` 2s 往返），不要用闪烁。
- **行 hover 指示条**：`.entry::before` 3px accent 色，`height` 从 0 滑到 `calc(100% - 28px)`（180ms）。
- **引用上标徽标**：`.cite` 类，`vertical-align:super`，accent 描边，hover 反色。
- **引用卡片展开**：`.cite-card` 类，`max-height:0 → 240px/280px` 过渡（350ms ease）。
- **流式光标**：`.caret::after` accent 色方块，`steps(2) blink`。
- **状态徽标**：圆角 2px + accent 色 10% 透明底 + 同色文字 + 1px 边框。中文标签 sans 字体（不要 mono）。
- **hit 高亮**（转写段定位）：`.hit` 类，accent 色 6% 透明底 + `inset 2px 0 0 accent` 左边框。
- **主题色块切换器**：`.swatch` 16x16 直角色块，选中态 `box-shadow: 0 0 0 2px paper-0, 0 0 0 3px ink-0`（双层 ring）。

## 9. package.json 草稿（直接用，省得想依赖）

```json
{
  "name": "vidlens-frontend",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "next lint",
    "typecheck": "tsc --noEmit"
  },
  "dependencies": {
    "next": "14.2.5",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "lucide-react": "^0.408.0"
  },
  "devDependencies": {
    "@types/node": "^20.14.10",
    "@types/react": "^18.3.3",
    "@types/react-dom": "^18.3.0",
    "typescript": "^5.5.3",
    "tailwindcss": "^3.4.6",
    "postcss": "^8.4.39",
    "autoprefixer": "^10.4.19",
    "eslint": "^8.57.0",
    "eslint-config-next": "14.2.5"
  }
}
```

**配套文件草稿**：

`tailwind.config.ts`（颜色 token 引用 CSS 变量，5 套主题抄 mockup 的 `:root` 值）：
```ts
import type { Config } from 'tailwindcss'
const config: Config = {
  content: ['./app/**/*.{ts,tsx}', './components/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        paper: ['var(--paper-0)', 'var(--paper-1)', 'var(--paper-2)', 'var(--paper-3)'],
        ink:   ['var(--ink-0)', 'var(--ink-1)', 'var(--ink-2)', 'var(--ink-3)', 'var(--ink-4)', 'var(--ink-5)'],
        accent: { 400: 'var(--accent-400)', 500: 'var(--accent-500)', 600: 'var(--accent-600)', 700: 'var(--accent-700)' },
        done: 'var(--done)',
        fail: 'var(--fail)',
      },
      fontFamily: {
        sans: ['Geist', '"Noto Sans SC"', 'system-ui', 'sans-serif'],
        mono: ['"Geist Mono"', '"JetBrains Mono"', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [],
}
export default config
```

`next.config.js`（dev 代理 `/api → :8080`）：
```js
/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    return [
      { source: '/api/:path*', destination: 'http://localhost:8080/api/:path*' },
    ]
  },
}
module.exports = nextConfig
```

`app/globals.css`（5 套主题 CSS 变量，从 mockup 的 `<style>` 块逐字抄过来；body 用 `font-sans`；不放 `grain` 类）：
```css
@tailwind base;
@tailwind components;
@tailwind utilities;

:root, [data-theme="sienna"] {
  --paper-0:#FFFFFF; --paper-1:#FAFAFB; --paper-2:#F1F1F3; --paper-3:#E5E5E8;
  --ink-0:#18181B; --ink-1:#27272A; --ink-2:#3F3F46; --ink-3:#52525B; --ink-4:#71717A; --ink-5:#A1A1AA;
  --accent-400:#C2622E; --accent-500:#9A4A1A; --accent-600:#7E3A12; --accent-700:#5C2A0D;
  --done:#3F7A4A; --fail:#C0392B;
  --skel-from:#F1F1F3; --skel-to:#E5E5E8;
}
[data-theme="indigo"] {
  --paper-0:#FFFFFF; --paper-1:#F7F8FB; --paper-2:#EDF0F7; --paper-3:#DCE1ED;
  --ink-0:#0E1530; --ink-1:#1E2748; --ink-2:#34406A; --ink-3:#5A6791; --ink-4:#7E89AD; --ink-5:#A2ABC7;
  --accent-400:#3F62C2; --accent-500:#2B4A8C; --accent-600:#1D3468; --accent-700:#122450;
  --done:#2A6B5E; --fail:#C0392B;
  --skel-from:#EDF0F7; --skel-to:#DCE1ED;
}
[data-theme="verdigris"] {
  --paper-0:#FFFFFF; --paper-1:#F7F9F6; --paper-2:#ECEFEC; --paper-3:#DEE3DD;
  --ink-0:#0F1A16; --ink-1:#1F2B26; --ink-2:#33423C; --ink-3:#54635C; --ink-4:#7A8882; --ink-5:#9CA8A2;
  --accent-400:#D9A44A; --accent-500:#B8842E; --accent-600:#8E631E; --accent-700:#6B4A14;
  --done:#2F6B5E; --fail:#9B3A2A;
  --skel-from:#ECEFEC; --skel-to:#DEE3DD;
}
[data-theme="rust"] {
  --paper-0:#FFFFFF; --paper-1:#FBF8F2; --paper-2:#F2EBDD; --paper-3:#E4D8C0;
  --ink-0:#2B1A0E; --ink-1:#3D2A1A; --ink-2:#573D29; --ink-3:#7A5940; --ink-4:#9C7A5C; --ink-5:#BB9A7C;
  --accent-400:#C75A2E; --accent-500:#A03A18; --accent-600:#7A2A10; --accent-700:#561D0A;
  --done:#6B7A2A; --fail:#7A2A1F;
  --skel-from:#F2EBDD; --skel-to:#E4D8C0;
}
[data-theme="ink"] {
  --paper-0:#0E0E11; --paper-1:#16161B; --paper-2:#1F1F26; --paper-3:#2A2A33;
  --ink-0:#F4F4F6; --ink-1:#D4D4D9; --ink-2:#A8A8B0; --ink-3:#85858D; --ink-4:#5E5E66; --ink-5:#3F3F47;
  --accent-400:#E0BE60; --accent-500:#C9A24A; --accent-600:#A88438; --accent-700:#856A2C;
  --done:#5FA8A0; --fail:#D9623A;
  --skel-from:#1F1F26; --skel-to:#2A2A33;
}

body { -webkit-font-smoothing: antialiased; }
```

**字体加载**：在 `app/layout.tsx` 的 `<head>` 里加 Google Fonts 链接（Geist + Geist Mono + Noto Sans SC），或用 `next/font/google` 包 Geist。

**主题切换器组件**：一个 `<ThemeSwitch>` 客户端组件，5 个 `.swatch` 按钮，点击 `document.documentElement.setAttribute('data-theme', name)`。初始读 `localStorage` 或默认 `sienna`。

## 10. 给新会话的开场建议

> 读 `docs/frontend-mockup/handoff-to-impl.md`（本文档）和 `docs/frontend-mockup/library-b-expandable.html`、`chat.html`、`kb.html`、`settings.html` 四个 mockup 文件。mockup 是静态 HTML 用 Tailwind CDN，实现时把它们拆成 Next.js App Router 的 React 组件 + Tailwind config（颜色 token 引用 CSS 变量，5 套主题抄 mockup 的 `:root` 变量值）。先搭项目骨架（package.json / next.config.js rewrites / tailwind.config.ts / app/layout.tsx 主题 provider / lib/types.ts / lib/api.ts），再逐页实现。每写完一页让我确认再继续下一页。

## 10. 项目根

`D:\dev\my_proj\go\vid-lens`。前端项目建议放 `web-next/`（与现有旧 Vue `web/` 区分），或直接在仓库根建 `frontend/`——实现时与用户确认。后端 `config.yaml` 在根目录，跑 `go run ./cmd/server` 启动后端 :8080。
