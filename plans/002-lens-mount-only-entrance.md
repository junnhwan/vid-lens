# 002 — 透镜入场动画仅挂载一次

- **Status**: TODO
- **Commit**: e097e33
- **Severity**: HIGH
- **Category**: Interruptibility
- **Estimated scope**: 2 files, ~25 lines

## Problem

`proto-lens-in` 是 CSS `@keyframes` 入场动画，挂在会随 `steps` 高频更新的组件上：

```tsx
// VariantM.tsx:52 — LensCard 根
<div className="... proto-lens-in ...">

// VariantM.tsx:124 — LensPill
<button className="... proto-lens-in ...">
```

`LensCard` 与 `LensPill` 在 `lensOpen` / `doneProcessCount` 切换时会**卸载并重新挂载**（`:249-253`），每次挂载都重播 `translateY(12px) scale(0.96)` 入场，叠加布局变化产生「抖一下」感。

```css
/* prototype.css:267-273 */
@keyframes proto-lens-in {
  from { opacity: 0; transform: translateY(12px) scale(0.96); }
  to { opacity: 1; transform: none; }
}
```

Keyframes 在快速切换时从 0 重启（AUDIT §4），不适合逐步更新的 live UI。

## Target

- 整场演示只有一次入场：用户首次看到透镜时播放 200ms ease-out
- `steps` 更新、底栏文案变化、证据增加：**零入场动画**
- `LensCard` ↔ `LensPill` 切换用 **opacity transition**（可中断），不用 keyframes

在 `prototype.css` 新增：

```css
.proto-lens-shell {
  transform-origin: bottom right;
  transition: opacity 200ms cubic-bezier(0.23, 1, 0.32, 1),
              transform 200ms cubic-bezier(0.23, 1, 0.32, 1);
}
.proto-lens-shell[data-mounted="false"] {
  opacity: 0;
  transform: translateY(8px) scale(0.97);
}
.proto-lens-shell[data-mounted="true"] {
  opacity: 1;
  transform: none;
}
```

`VariantM` 中：

```tsx
const [lensMounted, setLensMounted] = useState(false)
useEffect(() => {
  if (doneProcessCount > 0 || isActive) {
    requestAnimationFrame(() => setLensMounted(true))
  } else {
    setLensMounted(false)
  }
}, [doneProcessCount > 0 || isActive]) // 仅在首次出现/完全消失时切换
```

外层包裹：

```tsx
<div className="proto-lens-shell" data-mounted={lensMounted ? 'true' : 'false'}>
  {lensOpen && doneProcessCount > 0 ? <LensCard ... /> : <LensPill ... />}
</div>
```

从 `LensCard` / `LensPill` 上**移除** `proto-lens-in` class。

`prefers-reduced-motion` 块中增加 `.proto-lens-shell`。

## Repo conventions to follow

- 与 `prototype.css` 中 `proto-modal-in` 相同模式：`scale(0.96)` 起步，禁止 `scale(0)`
- 缓动使用 AUDIT 标准 `--ease-out` 数值：`cubic-bezier(0.23, 1, 0.32, 1)`
- `transform-origin: bottom right` — 透镜锚在右下角（AUDIT §3 触发器锚定）

## Steps

1. `prototype.css`：添加 `.proto-lens-shell` 规则与 reduced-motion 覆盖；保留旧 `proto-lens-in` 供其他变体或标记 deprecated。
2. `VariantM.tsx`：添加 `lensMounted` state + `useEffect`（见 Target）。
3. 浮层容器（`:247-254`）外包 `proto-lens-shell` + `data-mounted`。
4. 删除 `LensCard` `:52` 与 `LensPill` `:124` 上的 `proto-lens-in`。

## Boundaries

- 勿改 `proto-lens-in` 的 keyframes 定义（其他页面可能引用）。
- 勿为 Card/Pill 切换添加 keyframes 缩放弹跳。

## Verification

- **Mechanical**: `npm run typecheck` 通过。
- **Feel check**:
  - 重播演示：仅第一次出现透镜时有轻微上浮入场
  - 第 2–8 步更新时透镜**不再**整体 translate/scale 抖动
  - 收起为 Pill 再展开：200ms 淡入，无 keyframes 重启感
  - DevTools → Rendering → `prefers-reduced-motion: reduce`：透镜直接显示，无位移
- **Done when**: 步骤推进时 `proto-lens-in` 动画不再被触发（Animations 面板无重复 `proto-lens-in`）。
