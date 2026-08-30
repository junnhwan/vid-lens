# 005 — 用户收起透镜后不再被强行弹开

- **Status**: TODO
- **Commit**: e097e33
- **Severity**: MEDIUM
- **Category**: Purpose & frequency
- **Estimated scope**: 1 file, ~10 lines

## Problem

```tsx
// VariantM.tsx:156-158
useEffect(() => {
  if (isActive) setLensOpen(true)
}, [isActive])
```

用户点击收起（`setLensOpen(false)`）后，下一步 `isActive` 仍为 true → effect 再次 `setLensOpen(true)` → 卡片弹回，像「窗口一直在抖、关不掉」。

这是高频交互（演示中每步都可能触发），违反 AUDIT §1：用户明确选择收起后不应被系统覆盖。

## Target

仅在**本轮演示首次出现活动**时自动展开；用户手动收起后，本轮不再自动打开，直到 `reset` / 重播。

```tsx
const [lensOpen, setLensOpen] = useState(true)
const [userCollapsed, setUserCollapsed] = useState(false)

// 重播时由父级 reset 回调清零，或在 steps 变空时：
useEffect(() => {
  if (steps.every(s => s.status === 'pending')) {
    setUserCollapsed(false)
    setLensOpen(true)
  }
}, [steps])

useEffect(() => {
  if (isActive && !userCollapsed) setLensOpen(true)
}, [isActive, userCollapsed])

// onMinimize:
onMinimize={() => { setLensOpen(false); setUserCollapsed(true) }}
```

`DemoControls` 的 `onReset` 需在 `VariantM` 或通过 wrapper 重置 `userCollapsed`——在 `AgentChatPrototypeView` 传 `reset` 时一并 `setUserCollapsed(false)`。若 `reset` 在 page 层，可在 `VariantM` 内监听 `steps.length === 0` 或 `!steps.some(s => s.status !== 'pending')` 来重置。

## Repo conventions to follow

- 状态局部留在 `VariantM.tsx`，不提升到 page 除非必要
- 与 `LensPill` 手动 `onExpand` 配合：`setLensOpen(true); setUserCollapsed(false)`

## Steps

1. 添加 `userCollapsed` state。
2. 修改 `useEffect` 为 `isActive && !userCollapsed` 条件。
3. `onMinimize` 设置 `userCollapsed(true)`。
4. `LensPill` `onExpand` 设置 `userCollapsed(false)`。
5. 当所有 steps 回到 pending（重播开始）时重置 `userCollapsed`。

## Boundaries

- 勿改 `useAgentDemo` hook。
- 勿在收起时卸载 `LensCard` 数据（Pill 仍应显示进度）。

## Verification

- **Feel check**:
  - 重播 → 透镜自动展开 ✓
  - 点收起 → 保持 Pill，后续步骤**不**自动弹开大卡
  - 点 Pill 展开 → 恢复大卡
  - 点重播 → 再次自动展开
- **Done when**: 收起后连续 3 步更新，透镜保持 Pill 状态。
