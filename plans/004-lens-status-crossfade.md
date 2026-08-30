# 004 — 主气泡与透镜底栏文案交叉淡入

- **Status**: TODO
- **Commit**: e097e33
- **Severity**: MEDIUM
- **Category**: Missed opportunities / Cohesion
- **Estimated scope**: 2 files, ~30 lines

## Problem

步骤切换时，低语文案整块替换且无过渡：

```tsx
// VariantM.tsx:182-194 — 思考低语
{isThinking && running && (
  <p className="text-[14px] text-stone-500 flex items-center gap-2">
    ...
    {stepToWhisper(running)}
  </p>
)}
```

每步 `running` 对象变化 → React 在同一 `<p>` 内直接换文本 → 视觉「闪切」。若曾用 `key={running.id}`（VariantI 模式）会更糟：触发 `proto-whisper-in` 整段重播。

透镜底栏（plan 001 改为常驻后）同样每步换字，需要平滑过渡而非跳动。

## Target

文案切换用 **opacity crossfade 150ms**，不用 keyframes 入场，不用 `translateY`。

`prototype.css`：

```css
.proto-status-text {
  transition: opacity 150ms cubic-bezier(0.23, 1, 0.32, 1);
}
.proto-status-text[data-changing="true"] {
  opacity: 0;
}
```

`VariantM.tsx` 提取小组件 `CrossfadeText`：

```tsx
function CrossfadeText({ text, className }: { text: string; className?: string }) {
  const [display, setDisplay] = useState(text)
  const [changing, setChanging] = useState(false)
  useEffect(() => {
    if (text === display) return
    setChanging(true)
    const t = setTimeout(() => {
      setDisplay(text)
      setChanging(false)
    }, 150)
    return () => clearTimeout(t)
  }, [text, display])
  return (
    <span className={`proto-status-text ${className ?? ''}`} data-changing={changing ? 'true' : 'false'}>
      {display}
    </span>
  )
}
```

用于：
- 主气泡 `stepToWhisper(running)`（`:193`）
- 透镜底栏状态（plan 001 常驻底栏）

**删除**主气泡思考区三个 `proto-agent-bounce` 点，改为单个静态 `proto-agent-pulse-opacity` 小圆点（减少垂直跳动）。

## Repo conventions to follow

- 150ms + `cubic-bezier(0.23, 1, 0.32, 1)` 来自 AUDIT §2
- 使用 transition 而非 keyframes（AUDIT §4 可中断性）
- `prefers-reduced-motion`：`.proto-status-text { transition: none; }`

## Steps

1. `prototype.css` 添加 `.proto-status-text` 规则 + reduced-motion。
2. `VariantM.tsx` 添加 `CrossfadeText` 组件（可放文件底部或 `shared.tsx` 若愿复用——本计划限定 VariantM）。
3. 主气泡思考行：用 `<CrossfadeText text={stepToWhisper(running)} />`；bounce 三点改单点 pulse-opacity。
4. 透镜底栏文案同样包 `CrossfadeText`。

## Boundaries

- 勿改 `AnswerTypewriter` 打字机逻辑。
- 勿为 crossfade 引入 Framer Motion。

## Verification

- **Feel check**:
  - 重播演示，留意「在读转写…」→「换个思路…」切换：应淡入淡出，无向上弹跳
  - 底栏同步平滑
  - `prefers-reduced-motion` 下文案仍即时切换，无 150ms 等待感（transition none + 可跳过 changing 状态）
- **Done when**: 步骤切换时无 `proto-whisper-in` / `proto-fade-in` 被触发。
