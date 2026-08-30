'use client'

import { useEffect, useRef, useState } from 'react'
import { Brain, Database, Minimize2, Sparkles, Wrench } from 'lucide-react'
import type { CiteRef } from '@/components/Citation'
import type { ChatTraceStep } from '@/components/chat/traceTypes'
import { traceStepToWhisper } from '@/components/chat/agentNarrative'

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
    <span className={`ui-status-text ${className ?? ''}`} data-changing={changing ? 'true' : 'false'}>
      {display}
    </span>
  )
}

function StepIcon({ kind, className = 'w-3 h-3' }: { kind: ChatTraceStep['kind']; className?: string }) {
  if (kind === 'think') return <Brain className={className} />
  if (kind === 'retrieve') return <Database className={className} />
  if (kind === 'tool' || kind === 'plan' || kind === 'observe') return <Wrench className={className} />
  return <Sparkles className={className} />
}

function MiniGraph({ steps }: { steps: ChatTraceStep[] }) {
  const exec = steps.filter(s => s.kind !== 'answer')
  if (exec.length === 0) return <div className="h-4" />

  return (
    <div className="flex items-center gap-0.5 py-1 flex-nowrap min-w-0">
      {exec.map((step, i) => {
        const active = step.status === 'running'
        const done = step.status === 'done'
        return (
          <div key={step.id} className="flex items-center gap-0.5 shrink-0">
            {i > 0 && (
              <div className={`w-3 h-px shrink-0 transition-colors duration-150 ${done || active ? 'bg-sienna-500' : 'bg-paper-3'}`} />
            )}
            <div
              title={step.label}
              className={`w-2 h-2 rounded-full shrink-0 transition-colors duration-150 ${
                active ? 'bg-sienna-500 ui-agent-pulse-opacity' :
                done ? 'bg-moss' :
                'bg-paper-3'
              }`}
            />
          </div>
        )
      })}
    </div>
  )
}

function EvidenceQuote({ cite }: { cite: CiteRef }) {
  return (
    <article className="ui-quote-in rounded-xl ring-1 ring-ink-0/10 bg-paper-1 px-3.5 py-3 shrink-0 w-[min(240px,70vw)]">
      <div className="flex items-baseline gap-2 min-w-0 mb-1.5">
        <span className="font-mono text-[10px] text-sienna-700 shrink-0">{cite.id}</span>
        {(cite.videoTitle || cite.source) && (
          <span className="text-[11px] text-sienna-700 truncate">{cite.videoTitle || cite.source}</span>
        )}
      </div>
      <p className="text-[12px] leading-relaxed text-ink-1 line-clamp-3">{cite.content}</p>
    </article>
  )
}

function EvidenceStack({ cites }: { cites: CiteRef[] }) {
  const listRef = useRef<HTMLDivElement>(null)
  const [overflow, setOverflow] = useState(false)

  useEffect(() => {
    const el = listRef.current
    if (!el) {
      setOverflow(false)
      return
    }
    const check = () => setOverflow(el.scrollHeight > el.clientHeight + 1)
    check()
    const ro = new ResizeObserver(check)
    ro.observe(el)
    return () => ro.disconnect()
  }, [cites.length])

  return (
    <div className="flex-1 min-h-0 flex flex-col px-4 py-3 overflow-hidden">
      <div className="text-[11px] text-ink-4 mb-1.5 shrink-0">
        找到的片段{cites.length > 0 ? ` · ${cites.length}` : ''}
      </div>
      {cites.length === 0 ? (
        <p className="text-[11px] text-ink-5">检索到的转写片段会出现在这里</p>
      ) : (
        <div className="ui-ev-stack flex-1 min-h-0" data-overflow={overflow ? 'true' : 'false'}>
          <div ref={listRef} className="h-full overflow-y-auto space-y-2 pr-2">
            {cites.map(cite => (
              <EvidenceQuote key={cite.id} cite={cite} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function LensCard({ steps, cites, onMinimize }: {
  steps: ChatTraceStep[]
  cites: CiteRef[]
  onMinimize: () => void
}) {
  const trace = steps.filter(s => s.kind !== 'answer' && s.status !== 'pending')
  const running = steps.find(s => s.status === 'running')
  const doneCount = trace.filter(s => s.status === 'done').length
  const total = steps.filter(s => s.kind !== 'answer').length
  const finished = doneCount === total && total > 0 && !running

  const footerText = running
    ? traceStepToWhisper(running)
    : (trace.at(-1)?.label ?? '\u00a0')

  const headerTitle = running ? '研究中' : finished ? '研究完成' : '研究进展'

  return (
    <div className="ui-glass rounded-2xl overflow-hidden w-[min(400px,calc(100vw-2rem))] h-[min(460px,62vh)] min-h-[420px] flex flex-col">
      <div className="shrink-0 flex items-center justify-between gap-2 px-4 py-2.5 border-b border-ink-0/8">
        <div className="flex items-center gap-2 text-[13px] font-medium text-ink-1 min-w-0">
          {running && <span className="w-2 h-2 rounded-full bg-sienna-500 ui-agent-pulse-opacity shrink-0" />}
          {headerTitle}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span className="text-[11px] text-ink-4 tabular-nums">{doneCount}/{total || '—'}</span>
          <button type="button" onClick={onMinimize} className="p-1.5 rounded-lg text-ink-4 hover:text-ink-1 hover:bg-ink-0/5 transition-colors" title="收起">
            <Minimize2 className="w-4 h-4" />
          </button>
        </div>
      </div>

      <div className="shrink-0 px-4 py-2 border-b border-ink-0/6">
        <MiniGraph steps={steps} />
      </div>

      <EvidenceStack cites={cites} />

      <div className="shrink-0 h-10 px-4 flex items-center border-t border-ink-0/8 bg-paper-1/70 text-[11px] text-ink-3 min-w-0">
        <CrossfadeText text={footerText} className="truncate block w-full" />
      </div>
    </div>
  )
}

function LensPill({ steps, citeCount, onExpand }: {
  steps: ChatTraceStep[]
  citeCount: number
  onExpand: () => void
}) {
  const running = steps.some(s => s.status === 'running')
  const done = steps.filter(s => s.kind !== 'answer' && s.status === 'done').length
  const total = steps.filter(s => s.kind !== 'answer').length

  return (
    <button
      type="button"
      onClick={onExpand}
      className={`flex items-center gap-2.5 px-4 py-2.5 rounded-full ui-glass transition-colors ${
        running ? 'text-sienna-700 ui-agent-glow-lens' : 'text-ink-2 hover:text-ink-1'
      }`}
    >
      {running && <span className="w-2 h-2 rounded-full bg-sienna-500 ui-agent-pulse-opacity shrink-0" />}
      <span className="text-[12px] font-medium">
        {running ? '研究中' : '研究完成'} · {done}/{total || '—'}
      </span>
      {citeCount > 0 && <span className="text-[11px] text-ink-4">· {citeCount} 片段</span>}
    </button>
  )
}

/** Agent 模式右下角透镜浮层（融合版） */
export default function AgentLensOverlay({ steps, cites }: { steps: ChatTraceStep[]; cites: CiteRef[] }) {
  const [lensOpen, setLensOpen] = useState(true)
  const [userCollapsed, setUserCollapsed] = useState(false)
  const [lensMounted, setLensMounted] = useState(false)

  const processSteps = steps.filter(s => s.kind !== 'answer')
  const doneProcessCount = processSteps.filter(s => s.status === 'done').length
  const isActive = steps.some(s => s.status === 'running')
  const lensVisible = doneProcessCount > 0 || isActive

  const allIdle = steps.length === 0 || steps.every(s => s.status === 'pending')

  useEffect(() => {
    if (allIdle) {
      setUserCollapsed(false)
      setLensOpen(true)
      setLensMounted(false)
    }
  }, [allIdle])

  useEffect(() => {
    if (lensVisible) {
      requestAnimationFrame(() => setLensMounted(true))
    } else {
      setLensMounted(false)
    }
  }, [lensVisible])

  useEffect(() => {
    if (isActive && !userCollapsed) setLensOpen(true)
  }, [isActive, userCollapsed])

  if (!lensVisible) return null

  return (
    <div className="absolute right-4 bottom-4 z-20 pointer-events-none">
      <div className="ui-lens-shell pointer-events-auto" data-mounted={lensMounted ? 'true' : 'false'}>
        {lensOpen && doneProcessCount > 0 ? (
          <div key="card" className="ui-lens-swap">
            <LensCard
              steps={steps}
              cites={cites}
              onMinimize={() => { setLensOpen(false); setUserCollapsed(true) }}
            />
          </div>
        ) : (
          <div key="pill" className="ui-lens-swap">
            <LensPill
              steps={steps}
              citeCount={cites.length}
              onExpand={() => { setLensOpen(true); setUserCollapsed(false) }}
            />
          </div>
        )}
      </div>
    </div>
  )
}
