# Agent 原型动效修复计划

针对 `VariantM`（透镜浮层）及共享 `prototype.css` 的抖动 / 布局跳动问题。

**Commit 基准**: `e097e33`  
**审计范围**: `frontend/components/prototype/agent/VariantM.tsx`、`frontend/app/prototype/prototype.css`、共享 `shared.tsx`

## 计划列表

| # | 标题 | 严重度 | 状态 | 依赖 |
|---|------|--------|------|------|
| [001](001-lens-card-stable-layout.md) | 固定透镜卡片布局，消除逐步长高 | HIGH | DONE | — |
| [002](002-lens-mount-only-entrance.md) | 入场动画仅挂载一次，禁止逐步重播 | HIGH | DONE | 001 |
| [003](003-lens-transform-conflicts.md) | 修复 scale+pulse 冲突与 transition-all | HIGH | DONE | — |
| [004](004-lens-status-crossfade.md) | 状态文案 opacity 交叉淡入，禁止整块重排 | MEDIUM | DONE | 001 |
| [005](005-lens-auto-reopen-guard.md) | 用户收起后不再被 useEffect 强行弹开 | MEDIUM | DONE | — |

## 推荐执行顺序

1. **001** — 解决「窗口一直变高」的主因（证据区 / 底栏 / 脉络横向撑开）
2. **003** — 并行可做；消除圆点脉冲抖动
3. **002** — 去掉逐步更新时的入场动画错觉
4. **004** — 主气泡低语文案平滑切换
5. **005** — 交互尊重用户收起意图

执行完毕后用 `npm run dev` 打开 `?variant=M`，点「重播 Agent 流程」，以 10% 慢速观察右下角透镜是否仍跳动。
