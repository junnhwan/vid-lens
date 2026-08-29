'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { PanelRightClose, PanelRightOpen, GripVertical } from 'lucide-react'
import type { AgentStep } from '@/components/prototype/agent/types'
import AgentVariantB from '@/components/prototype/agent/VariantB'
import { StepIcon, StatusDot } from '@/components/prototype/agent/shared'

const CHAT_DEFAULT = 72   // 对话区默认占比 %
const CHAT_MIN = 58
const CHAT_MAX = 88
const SIDE_MIN_PX = 240
const SIDE_MAX_PCT = 42

function TimelineMini({ step }: { step: AgentStep }) {
  if (step.status === 'pending') return null
  const active = step.status === 'running'
  return (
    <div className={`relative pl-6 pb-4 proto-fade-in ${active ? 'proto-agent-glow rounded-lg -mx-1 px-1' : ''}`}>
      <div className={`absolute left-0 top-1 w-4 h-4 rounded-full border-2 flex items-center justify-center transition-all duration-300 ${
        active ? 'border-amber-500 bg-amber-50 scale-110' : step.status === 'done' ? 'border-emerald-500 bg-emerald-50' : 'border-stone-200'
      }`}>
        <StatusDot status={step.status} />
      </div>
      <div className="flex items-center gap-2 text-[11px] font-medium text-stone-700">
        <StepIcon kind={step.kind} className="w-3 h-3" />
        {step.label}
        {active && <span className="text-[10px] text-amber-600 proto-agent-shimmer">进行中</span>}
      </div>
      {step.kind === 'retrieve' && step.status === 'running' && (
        <div className="mt-1 h-1 rounded-full bg-stone-100 overflow-hidden"><div className="h-full bg-violet-500 proto-agent-scan" /></div>
      )}
      {step.kind === 'tool' && step.status === 'done' && (
        <div className="text-[10px] font-mono text-stone-500 mt-0.5">{step.tool}</div>
      )}
    </div>
  )
}

/** D 融合：对话为主（默认 ~72%）+ 可拖拽分栏 + 右侧流水线可折叠 */
export default function AgentVariantD({ steps }: { steps: AgentStep[] }) {
  const [chatPct, setChatPct] = useState(CHAT_DEFAULT)
  const [collapsed, setCollapsed] = useState(false)
  const [dragging, setDragging] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  const traceSteps = steps.filter(s => s.kind !== 'answer')
  const answer = steps.find(s => s.kind === 'answer')
  const running = steps.some(s => s.status === 'running')

  const onDragStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    setDragging(true)
  }, [])

  useEffect(() => {
    if (!dragging) return
    const onMove = (e: MouseEvent) => {
      const el = containerRef.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      const pct = ((e.clientX - rect.left) / rect.width) * 100
      const sidePct = 100 - pct
      if (sidePct < 100 - CHAT_MAX) setChatPct(CHAT_MAX)
      else if (pct < CHAT_MIN) setChatPct(CHAT_MIN)
      else if (sidePct > SIDE_MAX_PCT) setChatPct(100 - SIDE_MAX_PCT)
      else setChatPct(pct)
    }
    const onUp = () => setDragging(false)
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    return () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
  }, [dragging])

  return (
    <div ref={containerRef} className={`flex-1 flex min-h-0 relative ${dragging ? 'select-none cursor-col-resize' : ''}`}>
      {/* 对话主区 */}
      <div
        className="flex flex-col min-h-0 min-w-0 transition-[width] duration-200 ease-out"
        style={{ width: collapsed ? '100%' : `${chatPct}%` }}
      >
        <div className="flex-1 overflow-hidden min-h-0">
          <AgentVariantB steps={steps} />
        </div>
      </div>

      {/* 拖拽缝 + 折叠按钮 */}
      {!collapsed && (
        <div
          className="shrink-0 w-2 flex flex-col items-center justify-center bg-stone-100/80 border-x border-stone-200 hover:bg-amber-50/80 transition-colors group relative z-10"
          onMouseDown={onDragStart}
          role="separator"
          aria-orientation="vertical"
          aria-label="调整对话区与流水线宽度"
        >
          <GripVertical className="w-3.5 h-3.5 text-stone-400 group-hover:text-amber-700 pointer-events-none" />
          <button
            type="button"
            onClick={e => { e.stopPropagation(); setCollapsed(true) }}
            className="absolute top-3 -right-3 w-6 h-6 rounded-full bg-white border border-stone-200 shadow-sm flex items-center justify-center text-stone-500 hover:text-stone-800 hover:border-amber-300 proto-btn-lift"
            title="收起流水线"
          >
            <PanelRightClose className="w-3 h-3" />
          </button>
        </div>
      )}

      {/* 右侧流水线 */}
      {!collapsed && (
        <aside
          className="bg-[#faf8f5] overflow-y-auto p-4 min-h-0 transition-[width] duration-200 ease-out"
          style={{ width: `${100 - chatPct}%`, minWidth: SIDE_MIN_PX }}
        >
          <div className="flex items-center justify-between mb-3">
            <div className="text-[10px] uppercase tracking-wider text-stone-400">执行流水线</div>
            {running && (
              <span className="text-[10px] text-amber-700 flex items-center gap-1">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-500 proto-agent-pulse" />Live
              </span>
            )}
          </div>
          {traceSteps.length === 0 ? (
            <p className="text-[12px] text-stone-400 italic">等待 Agent 开始…</p>
          ) : traceSteps.map(s => <TimelineMini key={s.id} step={s} />)}
          {answer?.kind === 'answer' && answer.status === 'done' && (
            <div className="mt-2 p-3 rounded-xl border border-emerald-200 bg-emerald-50/50 text-[11px] text-emerald-800 proto-fade-in">
              回答已同步至左侧
            </div>
          )}
        </aside>
      )}

      {/* 收起后的展开把手 */}
      {collapsed && (
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          className="absolute right-0 top-1/2 -translate-y-1/2 z-20 flex flex-col items-center gap-1 py-4 px-1.5 rounded-l-lg bg-[#faf8f5] border border-r-0 border-stone-200 shadow-md text-stone-500 hover:text-amber-800 hover:border-amber-300 proto-btn-lift"
          title="展开流水线"
        >
          <PanelRightOpen className="w-4 h-4" />
          {running && <span className="w-2 h-2 rounded-full bg-amber-500 proto-agent-pulse" />}
          <span className="text-[9px] writing-mode-vertical [writing-mode:vertical-rl] tracking-wider">流水线</span>
        </button>
      )}
    </div>
  )
}
