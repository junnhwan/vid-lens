# Product UI

## React code map / 从哪里开始读

这个前端是 Vite + React 单页应用。建议按这条调用链阅读，而不是按文件名猜页面行为：

1. `index.html` 提供 `#root`；`main.tsx` 加载全局样式，并把路由挂到 React 根节点。
2. `app/router.tsx` 集中声明 URL 与页面的对应关系。`RootLayout` 提供主题和提示消息；`MainLayout` 提供登录后的工作台框架。按需加载的页面用 `React.lazy` 和 `Suspense`。
3. `app/(main)/`、`app/login/`、`app/docs/` 是页面组件。`(main)` 和 `[id]` 是迁移期间保留的目录名；URL 由 `app/router.tsx` 决定，目录名本身不生成路由。带 `params`、`searchParams` 的旧页面由路由中的小适配组件传参。
4. `components/` 放可复用的界面和交互。`components/shell/AppShell.tsx` 处理登录后的用户与全局操作；`components/shell/useLeaveGuard.ts` 保护未保存的表单和笔记。普通页面内跳转使用 `Link` 或导航函数。`lib/router.tsx` 是迁移期的小型兼容层，方便逐页改造旧的 `href`、`router.push` 调用。
5. `lib/api.ts` 是现有业务 API 的统一入口；`lib/artifacts/api.ts` 和 `schema.ts` 负责成果接口及响应校验。组件中的临时表单输入留在 `useState`；需跨组件共享的用户和 Toast 用 Context；成果的服务端缓存和刷新用 TanStack Query。不要把服务端响应复制成另一份长期本地状态。
6. `styles/tokens.css` 是颜色、字体等设计变量；页面布局和组件外观见下方样式索引。组件使用明确的加载、空数据、失败和只读状态。

新增页面时，在 `app/router.tsx` 注册路由，页面通过 `lib/api.ts` 或具体领域 API 获取数据，复用 `components/ui/` 的状态与弹窗组件。保存表单时要处理失败和离开页面的未保存内容；需要共享或轮询的服务端数据优先使用现有 Query Provider。代码里不再使用 Next 的 `use client`、文件路由或服务端组件语义。

The production workspace retains the existing routes and adds `/artifacts`,
`/artifacts/[id]`, and `/tasks`. The API authority is
`../docs/architecture/artifact-api-contract.md`.

## Where to change the design

- `styles/studio.css`: Studio light canvas, evergreen rail, hero and evidence layout.
- `styles/tokens.css`: shared colors, typefaces, spacing, paper, evidence panel,
  map palette, and theme overrides. Change these first for visual adjustments.
- `styles/product.css`: shell, page headings, dashboard hero and metrics.
- `styles/artifacts.css`: note paper, editor, map, evidence and task layouts.
- `components/shell/ShellFrame.tsx`: shared navigation and responsive frame.
- `components/product/`: common page headings and dashboard hero.
- `components/artifacts/`: reusable workspace, creation dialog, evidence panel,
  task status, cards and version-aware editing.

Notes and the mind map render the same structured body. Markmap is loaded on
demand; labels are escaped and no model-provided code or assets are executed.
Large trees initially collapse chapters; the text outline exposes all nodes.

## Data and writes

`lib/artifacts/schema.ts` validates wire responses with Zod. `api.ts` uses the
existing authenticated transport; production has no fixture fallback.
TanStack Query is scoped to the signed-in owner. Active runs use GET polling;
the existing conversation stream is unchanged.

Generation uses a stable idempotency key for retries of the same submission.
Editing uses the head version captured with the draft. A 409 preserves that
draft and offers comparison and download. Historical versions are read-only.
Unknown evidence timestamps never produce a seek; outdated snapshots retain
their text but disable seeking into a changed source.

## Development preview

Run `npm run dev`, then open `/dev/product?view=notes`. Other views are `home`,
`artifacts`, and `tasks`. The state selector exposes errors, conflicts, read-only
access, source deletion, unknown timestamps and a 200-node map.

`dev/productFixtures.ts` contains explicit synthetic examples. Saving in this
preview uses tab-scoped session storage only. Preview components are shared with
production, but the sample data is never a production API response or fallback.
The production Node server rejects this route with HTTP 404; Vite excludes the preview route from production.

## Runtime

`npm run dev` starts Vite with an `/api` proxy. `npm run build` writes `dist/`;
`npm start` serves the SPA and streams `/api` through to the Go server.
`VIDLENS_API_BASE` sets the backend target, and deployment writes the same
value to a local `.api-base` file for the systemd service.

## Checks

```powershell
npm test
npm run lint
npm run typecheck
npm run build
npm run test:stream
node --experimental-strip-types scripts/check-artifact-contract.mjs <local-response-file.json>
```

The last command validates private response specimens emitted by the backend
handler integration test. It does not substitute for authenticated browser
acceptance against a running API, worker, model provider and video media.
