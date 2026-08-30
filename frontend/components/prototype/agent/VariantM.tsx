'use client'

import { useEffect, useRef, useState } from 'react'
import { ChevronDown, Minimize2 } from 'lucide-react'
import type { AgentStep } from '@/components/prototype/agent/types'
import { DEMO_RESEARCH_QUESTION } from '@/components/prototype/agent/types'
import { collectEvidence } from '@/components/prototype/agent/useAgentDemo'
import { stepToWhisper } from '@/components/prototype/agent/stepNarrative'
import { StepIcon, StatusDot, UserBubble, AnswerTypewriter, AgentMark } from '@/components/prototype/agent/shared'

/** 文案切换：opacity 交叉淡入，避免步骤推进时整块闪切 */
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
    <span
      className={`proto-status-text ${className ?? ''}`}
      data-changing={changing ? 'true' : 'false'}
    >
      {display}
    </span>
  )
}

function MiniGraph({ steps }: { steps: AgentStep[] }) {
  const exec = steps.filter(s => s.kind !== 'answer')
  if (exec.length === 0) {
    return <div className="h-4" />
  }
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
                active ? 'bg-sienna-500 proto-agent-pulse-opacity' :
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

function EvidenceStack({ steps }: { steps: AgentStep[] }) {
  const evidence = collectEvidence(steps)
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
  }, [evidence.length])

  return (
    <div className="flex-1 min-h-0 flex flex-col px-4 py-2 overflow-hidden">
      <div className="text-[11px] text-ink-4 mb-1.5 shrink-0">
        找到的片段{evidence.length > 0 ? ` · ${evidence.length}` : ''}
      </div>
      {evidence.length === 0 ? (
        <p className="text-[11px] text-ink-5">检索到的转写片段会出现在这里</p>
      ) : (
        <div
          className="proto-ev-stack flex-1 min-h-0"
          data-overflow={overflow ? 'true' : 'false'}
        >
          <div ref={listRef} className="h-full overflow-y-auto space-y-1.5 pr-0.5">
            {evidence.map(ev => (
              <article
                key={ev.id}
                className="proto-item-in rounded-lg bg-sienna-500/6 ring-1 ring-sienna-500/15 px-2.5 py-1.5"
              >
                {ev.video && (
                  <div className="text-[9px] text-sienna-700 truncate">{ev.video}</div>
                )}
                <p className="text-[10px] text-ink-2 leading-snug mt-0.5">{ev.text}</p>
              </article>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function LensCard({
  steps, onMinimize,
}: {
  steps: AgentStep[]
  onMinimize: () => void
}) {
  const trace = steps.filter(s => s.kind !== 'answer' && s.status !== 'pending')
  const running = steps.find(s => s.status === 'running')
  const doneCount = trace.filter(s => s.status === 'done').length
  const total = steps.filter(s => s.kind !== 'answer').length
  const finished = doneCount === total && !running

  const slots: (AgentStep | null)[] = [...trace.slice(-2)]
  while (slots.length < 2) slots.unshift(null)

  const footerText = running
    ? stepToWhisper(running)
    : (trace.at(-1)?.label ?? '\u00a0')

  const headerTitle = running ? '研究中' : finished ? '研究完成' : '研究进展'

  return (
    <div className="proto-glass rounded-2xl overflow-hidden w-[min(360px,calc(100vw-2rem))] h-[min(420px,58vh)] min-h-[380px] flex flex-col">
      <div className="shrink-0 flex items-center justify-between px-4 py-2.5 border-b border-ink-0/8">
        <div className="flex items-center gap-2 text-[13px] font-medium text-ink-1">
          {running && <span className="w-2 h-2 rounded-full bg-sienna-500 proto-agent-pulse-opacity shrink-0" />}
          {headerTitle}
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[11px] text-ink-4 tabular-nums">{doneCount}/{total}</span>
          <button type="button" onClick={onMinimize} className="p-1.5 rounded-lg text-ink-4 hover:text-ink-1 hover:bg-ink-0/5 transition-colors proto-btn-lift" title="收起">
            <Minimize2 className="w-4 h-4" />
          </button>
        </div>
      </div>

      <div className="shrink-0 px-4 py-2 border-b border-ink-0/6">
        <div className="text-[11px] text-ink-4 mb-1">进度</div>
        <div className="h-6 overflow-x-auto overflow-y-hidden">
          <MiniGraph steps={steps} />
        </div>
      </div>

      <div className="shrink-0 px-4 py-2 border-b border-ink-0/6">
        <div className="text-[11px] text-ink-4 mb-1">最近步骤</div>
        <div className="space-y-1.5 min-h-[36px]">
          {slots.map((step, i) => (
            step ? (
              <div
                key={step.id}
                className={`flex items-center gap-2 text-[11px] leading-tight ${step.status === 'running' ? 'text-sienna-700' : 'text-ink-2'}`}
              >
                <StatusDot status={step.status} />
                <StepIcon kind={step.kind} className="w-3.5 h-3.5 opacity-70 shrink-0" />
                <span className="truncate flex-1">{step.label}</span>
                {step.kind === 'plan' && step.replan && (
                  <span className="text-[9px] text-sienna-700 shrink-0">重规划</span>
                )}
              </div>
            ) : (
              <div key={`empty-${i}`} className="h-[18px]" />
            )
          ))}
        </div>
      </div>

      <EvidenceStack steps={steps} />

      <div className="shrink-0 h-10 px-4 flex items-center border-t border-ink-0/8 bg-paper-1/70 text-[11px] text-ink-3 min-w-0">
        <CrossfadeText text={footerText} className="truncate block w-full" />
      </div>
    </div>
  )
}

function LensPill({ steps, onExpand }: { steps: AgentStep[]; onExpand: () => void }) {
  const running = steps.some(s => s.status === 'running')
  const done = steps.filter(s => s.kind !== 'answer' && s.status === 'done').length
  const total = steps.filter(s => s.kind !== 'answer').length
  const evidence = collectEvidence(steps).length

  return (
    <button
      type="button"
      onClick={onExpand}
      className={`flex items-center gap-2.5 px-4 py-2.5 rounded-full proto-glass proto-btn-lift ${
        running
          ? 'text-sienna-700 proto-agent-glow'
          : 'text-ink-2'
      }`}
    >
      {running && <span className="w-2 h-2 rounded-full bg-sienna-500 proto-agent-pulse-opacity shrink-0" />}
      <span className="text-[12px] font-medium">
        {running ? '研究中' : '研究完成'} · {done}/{total}
      </span>
      {evidence > 0 && <span className="text-[11px] text-ink-4">· {evidence} 片段</span>}
    </button>
  )
}

/**
 * M：透镜浮层 — 对话顶对齐，过程区定高，避免输出时整列重新居中。
 */
export function AgentVariantM({ steps }: { steps: AgentStep[] }) {
  const [openCites, setOpenCites] = useState<string[]>([])
  const [showProcess, setShowProcess] = useState(true)
  const [lensOpen, setLensOpen] = useState(true)
  const [userCollapsed, setUserCollapsed] = useState(false)
  const [lensMounted, setLensMounted] = useState(false)
  const processBoxRef = useRef<HTMLDivElement>(null)

  const running = steps.find(s => s.status === 'running')
  const answer = steps.find(s => s.kind === 'answer')
  const processSteps = steps.filter(s => s.kind !== 'answer')
  const visibleProcess = processSteps.filter(s => s.status !== 'pending')
  const doneProcessCount = processSteps.filter(s => s.status === 'done').length
  const isThinking = Boolean(running && running.kind !== 'answer')
  const hasAnswer = answer?.kind === 'answer' && answer.status !== 'pending'
  const isActive = steps.some(s => s.status === 'running')
  const lensVisible = doneProcessCount > 0 || isActive

  const allPending = steps.every(s => s.status === 'pending')

  useEffect(() => {
    if (allPending) {
      setUserCollapsed(false)
      setLensOpen(true)
      setLensMounted(false)
    }
  }, [allPending])

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

  useEffect(() => {
    const el = processBoxRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [doneProcessCount, running?.id])

  const handleMinimize = () => {
    setLensOpen(false)
    setUserCollapsed(true)
  }

  const handleExpand = () => {
    setLensOpen(true)
    setUserCollapsed(false)
  }

  return (
    <div className="flex-1 flex flex-col min-h-0 relative">
      <div className="flex-1 overflow-y-auto">
        <div className="px-6 pt-10 pb-28">
          <div className="w-full max-w-2xl mx-auto flex flex-col gap-6">
            <UserBubble text={DEMO_RESEARCH_QUESTION} />

            <div className="w-full">
              <div className="flex items-center gap-2 mb-2 text-[10px] text-ink-4">
                <AgentMark />
                映知 Agent
              </div>

              <div
                className={`proto-chat-bubble rounded-2xl rounded-tl-md px-5 py-4 min-h-[248px] transition-colors duration-200 ${
                  isThinking ? 'ring-1 ring-sienna-500/25 bg-sienna-500/5' :
                  'ring-1 ring-ink-0/8 bg-paper-0'
                }`}
              >
                  <div className="mb-3 pb-3 border-b border-ink-0/8">
                    <button
                      type="button"
                      onClick={() => setShowProcess(v => !v)}
                      className="text-[10px] text-ink-4 hover:text-ink-2 flex items-center gap-1 transition-colors w-full proto-btn-lift"
                    >
                      <ChevronDown className={`w-3 h-3 proto-chevron ${showProcess ? 'rotate-180' : ''}`} />
                      {showProcess ? '收起过程' : `研究过程（${doneProcessCount} 步）`}
                      {isThinking && running && (
                        <span className="ml-auto text-sienna-700 flex items-center gap-1">
                          <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 proto-agent-pulse-opacity" />
                          进行中
                        </span>
                      )}
                    </button>
                    <div className="proto-acc" data-open={showProcess ? 'true' : 'false'}>
                      <div className="proto-acc-inner">
                        <div ref={processBoxRef} className="h-[88px] overflow-y-auto space-y-1.5 pt-2">
                          {visibleProcess.map(step => (
                            <div
                              key={step.id}
                              className={`proto-item-in text-[10px] flex items-center gap-1.5 ${
                                step.status === 'running' ? 'text-sienna-700' : 'text-ink-3'
                              }`}
                            >
                              <StepIcon kind={step.kind} className="w-3 h-3 shrink-0" />
                              <span className="truncate">{step.label}</span>
                              {step.status === 'running' && (
                                <span className="text-[9px] text-sienna-600 shrink-0">…</span>
                              )}
                            </div>
                          ))}
                        </div>
                      </div>
                    </div>
                  </div>

                <div className="relative min-h-[7.2em]">
                  {isThinking && running && (
                    <p className="absolute inset-x-0 top-0 text-[14px] text-ink-3 flex items-center gap-2 h-[22px] z-[1]">
                      <span className="w-1.5 h-1.5 rounded-full bg-ink-4 shrink-0 proto-agent-pulse-opacity" />
                      <CrossfadeText text={stepToWhisper(running)} />
                    </p>
                  )}

                  {hasAnswer && answer.kind === 'answer' && (
                    <AnswerTypewriter
                      content={answer.content}
                      active={answer.status === 'running' || answer.status === 'done'}
                      cites={answer.cites}
                      openCites={openCites}
                      onToggleCite={id => setOpenCites(o => o.includes(id) ? o.filter(x => x !== id) : [...o, id])}
                      className="text-[16px] leading-[1.8] text-ink-0"
                    />
                  )}

                  {!isThinking && !hasAnswer && (
                    <p className="text-[14px] text-ink-5">…</p>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="absolute right-4 bottom-[5.5rem] z-20 pointer-events-none">
        {lensVisible && (
          <div
            className="proto-lens-shell pointer-events-auto"
            data-mounted={lensMounted ? 'true' : 'false'}
          >
            {lensOpen && doneProcessCount > 0 ? (
              <div key="card" className="proto-lens-swap">
                <LensCard steps={steps} onMinimize={handleMinimize} />
              </div>
            ) : (
              <div key="pill" className="proto-lens-swap">
                <LensPill steps={steps} onExpand={handleExpand} />
              </div>
            )}
          </div>
        )}
      </div>

      <div className="shrink-0 border-t border-ink-0/8 bg-paper-0/80 px-6 py-4 relative z-10">
        <div className="max-w-2xl mx-auto">
          <div className="rounded-xl border border-ink-0/10 bg-paper-0 px-4 py-3 text-[13px] text-ink-5">
            继续追问…（原型演示，输入不可用）
          </div>
          <p className="text-[10px] text-ink-5 mt-2 text-center">
            对话全宽 · 右下角可展开研究进展与片段
          </p>
        </div>
      </div>
    </div>
  )
}
