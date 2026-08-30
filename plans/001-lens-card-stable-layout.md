# 001 — 固定透镜卡片布局，消除逐步长高

- **Status**: TODO
- **Commit**: e097e33
- **Severity**: HIGH
- **Category**: Purpose & frequency / Performance
- **Estimated scope**: 1 file, ~40 lines

## Problem

`LensCard` 在 Agent 演示的每一步 `setSteps` 后都会改变高度，导致右下角浮层持续上下跳动——用户描述的「窗口一直变化」主要来自这里，而非对话区。

证据（条件挂载区块）：

```tsx
// frontend/components/prototype/agent/VariantM.tsx:88-109
{evidence.length > 0 && (
  <div className="px-3 py-2">...</div>
)}

{running && (
  <div className="px-3 py-2 bg-stone-50/80 border-t ...">
    {stepToWhisper(running)}
  </div>
)}
```

- 证据区从 0 条变为 1 条时整块 DOM 插入 → 卡片突然变高
- `running` 底栏在每步 `done` → 下一步 `running` 之间会卸载/重装 → 高度来回跳
- `MiniGraph` 随步骤增加横向加点（`:11-36`），宽度微变

`max-h-[100px]` 只约束流水线区，无法稳定整体卡片。

## Target

透镜卡片在整段演示中保持**固定外框尺寸**；内容更新只改变内部文字/圆点颜色，不改变卡片高度。新增证据用横向滚动，不撑高；底栏占位始终保留。

在 `LensCard` 根节点增加固定高度壳：

```tsx
<div className="... w-[min(300px,calc(100vw-3rem))] h-[248px] flex flex-col">
```

分区目标：

| 区块 | 高度 | 行为 |
|------|------|------|
| Header | auto shrink-0 | 不变 |
| 脉络 MiniGraph | `h-8 shrink-0` | 横向 `overflow-x-auto`，不换行 |
| 流水线 | `h-[72px] shrink-0 overflow-hidden` | 固定 3 行槽位，不足用空行占位 |
| 证据 | `h-[68px] shrink-0` | **始终渲染**；无证据时显示灰色占位「暂无证据」 |
| 底栏状态 | `h-8 shrink-0` | **始终渲染**；无 running 时显示最后一步摘要或 `&nbsp;` |

底栏示例：

```tsx
<div className="h-8 shrink-0 px-3 flex items-center border-t border-stone-100 bg-stone-50/80 text-[10px] text-stone-500 truncate">
  {running ? stepToWhisper(running) : (trace.at(-1)?.label ?? '\u00a0')}
</div>
```

证据区示例（始终存在）：

```tsx
<div className="h-[68px] shrink-0 px-3 py-2 border-t border-stone-50">
  ...
  {evidence.length === 0 ? (
    <p className="text-[9px] text-stone-300 italic">暂无证据</p>
  ) : (
    <div className="flex gap-1.5 overflow-x-auto">...</div>
  )}
</div>
```

流水线固定 3 槽：

```tsx
const slots = [...trace.slice(-3)]
while (slots.length < 3) slots.unshift(null)
// map slots; null → 空行 div h-[18px]
```

## Repo conventions to follow

- 原型动效类前缀 `proto-*`，定义在 `frontend/app/prototype/prototype.css`
- 浮层定位保持在 `VariantM.tsx` 的 `absolute right-4 bottom-[5.5rem]`
- 参照 `VariantD.tsx` 侧栏用 `min-h-0 overflow-y-auto` 处理溢出，但本计划优先**禁止溢出撑高**

## Steps

1. 打开 `frontend/components/prototype/agent/VariantM.tsx`，给 `LensCard` 根 `div`（当前 `:52`）加 `h-[248px] flex flex-col`，移除根上的 `proto-lens-in`（交给 plan 002）。
2. 将 `MiniGraph` 外包 `div` 设为 `h-8 shrink-0 overflow-x-auto overflow-y-hidden`；图谱行 `flex-nowrap`。
3. 流水线区改为固定 `h-[72px] shrink-0`，实现 3 槽位逻辑（见 Target）。
4. 证据区改为**无条件渲染**，固定 `h-[68px] shrink-0`。
5. 底栏改为**无条件渲染**，固定 `h-8 shrink-0`；`running` 时用 `stepToWhisper`，否则显示 `trace` 最后一项 label。
6. 删除 `{evidence.length > 0 &&` 与 `{running &&` 条件包裹。

## Boundaries

- 勿改 `LensPill`、对话主区、其他 Variant 文件。
- 勿引入新依赖。
- 若 `h-[248px]` 在实机裁切内容，只允许微调 ±8px，不得恢复条件挂载撑高模式。

## Verification

- **Mechanical**: `cd frontend && npm run typecheck` 通过。
- **Feel check**:
  - 打开 `http://localhost:3000/prototype/agent-chat?variant=M`，点「重播 Agent 流程」
  - 右下角透镜外框高度在整个 8 步演示中**不变**（可用 DevTools 选中根节点看 computed height）
  - 证据从 0→3 时卡片不跳，仅横向卡片增加
  - 步骤切换时底栏文案变化但底栏高度不变
- **Done when**: 透镜卡片 `getBoundingClientRect().height` 在演示全程波动 ≤ 1px。
