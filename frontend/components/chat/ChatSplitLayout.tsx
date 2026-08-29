'use client'

import { useCallback, useEffect, useRef, useState, type ReactNode, type Ref } from 'react'
import { GripVertical, PanelRightClose, PanelRightOpen } from 'lucide-react'

const CHAT_DEFAULT = 72
const CHAT_MIN = 58
const CHAT_MAX = 88
const SIDE_MIN_PX = 240
const SIDE_MAX_PCT = 42

export default function ChatSplitLayout({
  children,
  tracePanel,
  scrollRef,
}: {
  children: ReactNode
  tracePanel: ReactNode
  scrollRef?: Ref<HTMLDivElement>
}) {
  const [chatPct, setChatPct] = useState(CHAT_DEFAULT)
  const [collapsed, setCollapsed] = useState(false)
  const [dragging, setDragging] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  // 窄屏默认收起流水线，避免挤压对话区
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 767px)')
    const sync = () => { if (mq.matches) setCollapsed(true) }
    sync()
    mq.addEventListener('change', sync)
    return () => mq.removeEventListener('change', sync)
  }, [])

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
    <div
      ref={containerRef}
      className={`flex-1 flex min-h-0 min-w-0 relative ${dragging ? 'select-none cursor-col-resize' : ''}`}
    >
      <div
        ref={scrollRef}
        role="main"
        className="overflow-y-auto scroll-thin px-6 py-6 min-h-0 min-w-0 transition-[width] duration-200 ease-out"
        style={{ width: collapsed ? '100%' : `${chatPct}%` }}
      >
        <div className="max-w-2xl mx-auto space-y-6">{children}</div>
      </div>

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
            className="absolute top-3 -right-3 w-6 h-6 rounded-full bg-white border border-stone-200 shadow-sm flex items-center justify-center text-stone-500 hover:text-stone-800 hover:border-amber-300 ui-btn-lift"
            title="收起流水线"
          >
            <PanelRightClose className="w-3 h-3" />
          </button>
        </div>
      )}

      {!collapsed && (
        <aside
          className="bg-[#faf8f5] overflow-y-auto min-h-0 transition-[width] duration-200 ease-out"
          style={{ width: `${100 - chatPct}%`, minWidth: SIDE_MIN_PX }}
        >
          {tracePanel}
        </aside>
      )}

      {collapsed && (
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          className="absolute right-0 top-1/2 -translate-y-1/2 z-20 flex flex-col items-center gap-1 py-4 px-1.5 rounded-l-lg bg-[#faf8f5] border border-r-0 border-stone-200 shadow-md text-stone-500 hover:text-amber-800 hover:border-amber-300 ui-btn-lift"
          title="展开流水线"
        >
          <PanelRightOpen className="w-4 h-4" />
          <span className="text-[9px] [writing-mode:vertical-rl] tracking-wider">流水线</span>
        </button>
      )}
    </div>
  )
}
