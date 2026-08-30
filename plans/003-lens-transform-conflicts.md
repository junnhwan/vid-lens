# 003 — 修复透镜内 transform 冲突与 transition-all

- **Status**: TODO
- **Commit**: e097e33
- **Severity**: HIGH
- **Category**: Easing & duration / Performance
- **Estimated scope**: 2 files, ~20 lines

## Problem

**1. 同一元素叠加 Tailwind `scale-125` 与 `proto-agent-pulse` keyframes transform**

```tsx
// VariantM.tsx:26-30
className={`w-2 h-2 rounded-full transition-all duration-300 ${
  active ? 'bg-amber-500 scale-125 proto-agent-pulse' :
```

```css
/* prototype.css:101-104 */
@keyframes proto-agent-pulse {
  50% { opacity: 0.4; transform: scale(0.85); }
}
```

浏览器在 class `scale-125`（transform: scale(1.25)）与 keyframes `scale(0.85)` 之间打架，活跃圆点逐步更新时 visibly 抖动。

**2. `transition-all duration-300` 在逐步更新的列表项上**

同上 `:26`，`transition-all` 会动画化所有属性（AUDIT §5 明确为 finding），步骤 `pending→running→done` 时颜色/scale/box-shadow 全部过渡 300ms，7 个圆点同时过渡产生波浪抖动。

**3. `proto-agent-scan` 动画 `width`（布局属性）**

```css
/* prototype.css:122-126 */
@keyframes proto-agent-scan {
  0% { width: 0; } ...
}
```

在 VariantM 未直接使用，但共享类若被 Lens 未来引用会引发布局抖动；本计划仅修 VariantM 内引用。

## Target

活跃圆点：**只脉冲 opacity**，不动 transform：

```tsx
className={`w-2 h-2 rounded-full transition-colors duration-150 ease-out ${
  active ? 'bg-amber-500 proto-agent-pulse-opacity' :
  done ? 'bg-emerald-500' :
  'bg-stone-200'
}`}
```

新增 CSS：

```css
.proto-agent-pulse-opacity {
  animation: proto-agent-pulse-opacity 1.5s ease-in-out infinite;
}
@keyframes proto-agent-pulse-opacity {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.45; }
}
```

`transition-all` → 显式属性：

```tsx
transition-colors duration-150 ease-out
```

缓动：`ease-out` 对应 `cubic-bezier(0.23, 1, 0.32, 1)` 若写内联；Tailwind `ease-out` 可接受用于 150ms 颜色变化。

`LensPill` 上 `proto-agent-glow` 静态 box-shadow 可保留；运行中改为仅 opacity 脉冲的小圆点，勿给整个 pill 加 transform。

## Repo conventions to follow

- 现有 `proto-agent-pulse` 保留给其他 Variant；透镜专用 `proto-agent-pulse-opacity`
- `prefers-reduced-motion` 块加入新类名
- 参照 `StatusDot`（`shared.tsx:17-21`）— 仅用 opacity/background，无 scale

## Steps

1. `prototype.css`：添加 `proto-agent-pulse-opacity` keyframes；在 `@media (prefers-reduced-motion: reduce)` 中禁用。
2. `VariantM.tsx` `MiniGraph`：移除 `scale-125` 与 `proto-agent-pulse`；改用 `proto-agent-pulse-opacity`；`transition-all duration-300` → `transition-colors duration-150 ease-out`。
3. `LensCard` header 与 `LensPill` 上的小圆点：改用 `proto-agent-pulse-opacity`，勿用 `proto-agent-pulse`。
4. 检查 `LensPill` 勿含 `scale-*` 与 pulse 叠用。

## Boundaries

- 勿全局删除 `proto-agent-pulse`（A–L 其他变体仍用）。
- 勿改 `proto-agent-bounce`（本计划范围外；见 plan 004 对话区）。

## Verification

- **Mechanical**: `npm run typecheck` 通过。
- **Feel check**:
  - 重播 M 变体，观察脉络区活跃圆点：应柔和明暗，无尺寸弹跳
  - DevTools Animations 面板：活跃点只有 `opacity` 变化轨道，无 `transform` 轨道
- **Done when**: `MiniGraph` 活跃 dot 的 computed `transform` 为 `none`（或 matrix 无缩放）。
