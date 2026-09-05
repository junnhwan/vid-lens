'use client'

import { useEffect, useRef, useState } from 'react'
import { ChevronDown, Copy, RefreshCw } from 'lucide-react'
import { CitationCards, renderAnswerWithCites } from '@/components/Citation'
import type { CiteRef } from '@/components/Citation'
import type { ChatMsg } from '@/components/chat/chatUtils'
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

function AgentMark() {
  return (
    <span className="w-5 h-5 text-[9px] rounded-[5px] bg-ink-0 text-paper-0 font-semibold flex items-center justify-center tracking-tight">
      映
    </span>
  )
}

function TraceStepIcon({ kind }: { kind: string }) {
  const cls = 'w-3 h-3 shrink-0'
  if (kind === 'retrieve') return <span className={cls}>⌕</span>
  if (kind === 'tool') return <span className={cls}>⚙</span>
  if (kind === 'think') return <span className={cls}>◎</span>
  return <span className={cls}>·</span>
}

export default function AgentChatBubble({
  msg,
  idx,
  onToggleCite,
  onCopy,
  onRetry,
  onPlayCitation,
}: {
  msg: ChatMsg
  idx: number
  modeLabel?: string
  onToggleCite: (msgIdx: number, id: string) => void
  onCopy?: (content: string) => void
  onRetry?: (msgIdx: number) => void
  onPlayCitation?: (citation: CiteRef) => void
}) {
  const [showProcess, setShowProcess] = useState(true)
  const processBoxRef = useRef<HTMLDivElement>(null)

  const steps = msg.trace || []
  const running = steps.find(s => s.status === 'running')
  const processSteps = steps.filter(s => s.kind !== 'answer')
  const visibleProcess = processSteps.filter(s => s.status !== 'pending')
  const doneProcessCount = processSteps.filter(s => s.status === 'done').length
  const isThinking = Boolean(running && running.kind !== 'answer')
  const hasAnswer = Boolean(msg.content) || msg.streaming
  const citeIds = (msg.cites || []).map(c => c.id)
  const toggle = (id: string) => onToggleCite(idx, id)

  useEffect(() => {
    const el = processBoxRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [doneProcessCount, running?.id])

  if (msg.error) {
    return (
      <div className="space-y-2 ui-fade-in">
        <div className="flex items-center gap-2 text-[10px] text-ink-4">
          <AgentMark />
          映知 Agent
        </div>
        <div className="p-4 rounded-xl bg-red-50 border border-red-200 text-[13px] text-red-700">{msg.error}</div>
      </div>
    )
  }

  return (
    <div className="space-y-2 ui-fade-in w-full">
      <div className="flex items-center gap-2 text-[10px] text-ink-4">
        <AgentMark />
        映知 Agent
        {msg.streaming && (
          <span className="text-sienna-700 flex items-center gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 ui-agent-pulse-opacity" />
            生成中
          </span>
        )}
      </div>

      <div
        className={`ui-chat-bubble rounded-2xl rounded-tl-md px-5 py-4 min-h-[248px] transition-colors duration-200 ${
          isThinking ? 'ring-1 ring-sienna-500/25 bg-sienna-500/5' :
          'ring-1 ring-ink-0/8 bg-paper-0'
        }`}
      >
        {visibleProcess.length > 0 && (
          <div className="mb-3 pb-3 border-b border-ink-0/8">
            <button
              type="button"
              onClick={() => setShowProcess(v => !v)}
              className="text-[10px] text-ink-4 hover:text-ink-2 flex items-center gap-1 transition-colors w-full"
            >
              <ChevronDown className={`w-3 h-3 ui-chevron ${showProcess ? 'rotate-180' : ''}`} />
              {showProcess ? '收起过程' : `研究过程（${doneProcessCount} 步）`}
              {isThinking && running && (
                <span className="ml-auto text-sienna-700 flex items-center gap-1">
                  <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 ui-agent-pulse-opacity" />
                  进行中
                </span>
              )}
            </button>
            <div className="ui-acc" data-open={showProcess ? 'true' : 'false'}>
              <div className="ui-acc-inner">
                <div ref={processBoxRef} className="h-[88px] overflow-y-auto space-y-1.5 pt-2">
                  {visibleProcess.map(step => (
                    <div
                      key={step.id}
                      className={`ui-item-in text-[10px] flex items-center gap-1.5 ${
                        step.status === 'running' ? 'text-sienna-700' : 'text-ink-3'
                      }`}
                    >
                      <TraceStepIcon kind={step.kind} />
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
        )}

        <div className="relative min-h-[7.2em]">
          {isThinking && running && (
            <p className="absolute inset-x-0 top-0 text-[14px] text-ink-3 flex items-center gap-2 h-[22px] z-[1]">
              <span className="w-1.5 h-1.5 rounded-full bg-ink-4 shrink-0 ui-agent-pulse-opacity" />
              <CrossfadeText text={traceStepToWhisper(running)} />
            </p>
          )}

          {hasAnswer && (
            <div className={`text-[16px] leading-[1.8] text-ink-0 ${isThinking ? 'pt-7' : ''}`}>
              {renderAnswerWithCites(msg.content, citeIds, toggle, msg.openCiteIds || [])}
              {msg.streaming && <span className="ui-typewriter-cursor" aria-hidden />}
            </div>
          )}

          {!isThinking && !hasAnswer && (
            <p className="text-[14px] text-ink-5">…</p>
          )}
        </div>
      </div>

      {!msg.streaming && msg.content && (onCopy || onRetry) && (
        <div className="flex items-center gap-3 text-[10px] text-ink-4">
          {onCopy && (
            <button onClick={() => onCopy(msg.content)} className="hover:text-ink-2 flex items-center gap-1">
              <Copy className="w-3 h-3" />复制
            </button>
          )}
          {onRetry && (
            <button onClick={() => onRetry(idx)} className="hover:text-ink-2 flex items-center gap-1">
              <RefreshCw className="w-3 h-3" />重试
            </button>
          )}
        </div>
      )}

      {msg.cites && msg.cites.length > 0 && (
        <div className={msg.streaming ? 'opacity-60' : 'ui-typewriter-cites-in'}>
          <CitationCards refs={msg.cites} openIds={msg.openCiteIds || []} onPlayCitation={onPlayCitation} />
        </div>
      )}
    </div>
  )
}
