'use client'

import { useState } from 'react'
import { BookOpen, ChevronDown, Copy, RefreshCw, Brain } from 'lucide-react'
import { CitationCards, renderAnswerWithCites } from '@/components/Citation'
import type { ChatMsg } from '@/components/chat/chatUtils'
import type { ChatTraceStep } from '@/components/chat/traceTypes'
import AgentChatBubble from '@/components/chat/AgentChatBubble'

const TRACE_SOURCE_LABEL = {
  agent: 'Agent 执行步骤',
  legacy: '历史执行摘要',
  inferred: '检索与生成',
} as const

function TraceCollapse({
  steps,
  source,
}: {
  steps: ChatTraceStep[]
  source?: keyof typeof TRACE_SOURCE_LABEL
}) {
  const [open, setOpen] = useState(false)
  const done = steps.filter(s => s.status === 'done').length
  if (steps.length === 0) return null

  const title = source ? TRACE_SOURCE_LABEL[source] : '思考与检索轨迹'

  return (
    <div className="rounded-xl border border-ink-0/8 bg-paper-1/80 overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        className="w-full flex items-center justify-between px-3 py-2 text-[11px] text-ink-2 hover:bg-paper-1"
      >
        <span className="flex items-center gap-1.5">
          <Brain className="w-3 h-3" />
          {title} ({done}/{steps.length})
        </span>
        <ChevronDown className={`w-3.5 h-3.5 ui-chevron transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div className="px-3 pb-3 border-t border-ink-0/6 space-y-2">
          {steps.map(s => (
            <div key={s.id} className="flex items-start gap-2 text-[11px] text-ink-3 py-0.5">
              <span className={`mt-1 w-1.5 h-1.5 rounded-full shrink-0 ${
                s.status === 'done' ? 'bg-moss' : s.status === 'running' ? 'bg-sienna-500 ui-agent-pulse-opacity' : s.status === 'error' ? 'bg-rust' : 'bg-paper-3'
              }`} />
              <div className="min-w-0 space-y-0.5">
                <div>
                  <span className="font-medium text-ink-1">{s.label}</span>
                  {s.tool && <span className="text-ink-4 font-mono text-[10px]"> · {s.tool}</span>}
                  {s.detail && s.status !== 'error' && <span className="text-ink-3"> · {s.detail}</span>}
                </div>
                {s.toolInput && (
                  <p className="text-[10px] text-stone-500 font-mono truncate" title={s.toolInput}>
                    输入 {s.toolInput}
                  </p>
                )}
                {s.toolOutput && s.status !== 'error' && (
                  <p className="text-[10px] text-stone-500 font-mono truncate" title={s.toolOutput}>
                    输出 {s.toolOutput}
                  </p>
                )}
                {s.error && (
                  <p className="text-[10px] text-red-600">{s.error}</p>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default function ChatMessageRow({ msg, idx, onToggleCite, modeLabel, topK, onCopy, onRetry }: {
  msg: ChatMsg
  idx: number
  onToggleCite: (msgIdx: number, id: string) => void
  modeLabel: string
  topK?: number
  onCopy?: (content: string) => void
  onRetry?: (msgIdx: number) => void
}) {
  if (msg.role === 'user') {
    return (
      <div className="flex justify-end ui-fade-in">
        <div className="bg-ink-0 text-paper-0 text-[14px] leading-relaxed px-4 py-2.5 rounded-2xl rounded-br-md max-w-[85%]">
          {msg.content}
        </div>
      </div>
    )
  }

  if (msg.agentRun) {
    return (
      <AgentChatBubble
        msg={msg}
        idx={idx}
        modeLabel={modeLabel}
        onToggleCite={onToggleCite}
        onCopy={onCopy}
        onRetry={onRetry}
      />
    )
  }

  const citeIds = (msg.cites || []).map(c => c.id)
  const toggle = (id: string) => onToggleCite(idx, id)
  const showTrace = msg.trace && msg.trace.length > 0 && !msg.streaming

  return (
    <div className="space-y-2 ui-fade-in">
      <div className="flex items-center gap-2 text-[10px] text-ink-4">
        <BookOpen className="w-3 h-3" />
        映知 · {modeLabel}{topK != null ? ` · top_k ${topK}` : ''}
        {msg.streaming && (
          <span className="text-sienna-700 flex items-center gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 ui-agent-pulse-opacity" />生成中
          </span>
        )}
        {msg.degraded && <span>（降级）</span>}
      </div>

      {showTrace && <TraceCollapse steps={msg.trace!} source={msg.traceSource} />}

      {msg.error ? (
        <div className="p-4 rounded-xl bg-red-50 border border-red-200 text-[13px] text-red-700">{msg.error}</div>
      ) : (
        <div className={`rounded-2xl border p-4 transition-all duration-300 ${
          msg.streaming ? 'border-sienna-500/25 bg-sienna-500/5' : 'border-transparent bg-transparent px-0 py-0'
        }`}>
          <div className="text-[15px] leading-[1.8] text-ink-0">
            {renderAnswerWithCites(msg.content, citeIds, toggle, msg.openCiteIds || [])}
            {msg.streaming && <span className="ui-typewriter-cursor" aria-hidden />}
          </div>
        </div>
      )}

      {!msg.streaming && !msg.error && msg.content && (onCopy || onRetry) && (
        <div className="flex items-center gap-3 text-[10px] text-ink-4">
          {onCopy && (
            <button onClick={() => onCopy(msg.content)} className="hover:text-ink-2 flex items-center gap-1 ui-btn-lift">
              <Copy className="w-3 h-3" />复制
            </button>
          )}
          {onRetry && (
            <button onClick={() => onRetry(idx)} className="hover:text-ink-2 flex items-center gap-1 ui-btn-lift">
              <RefreshCw className="w-3 h-3" />重试
            </button>
          )}
        </div>
      )}

      {msg.cites && msg.cites.length > 0 && (
        <div className={msg.streaming ? 'opacity-60' : 'ui-typewriter-cites-in'}>
          <CitationCards refs={msg.cites} openIds={msg.openCiteIds || []} />
        </div>
      )}
      {!msg.streaming && !msg.error && msg.content && (!msg.cites || msg.cites.length === 0) && (
        <p className="text-[10px] text-ink-4 italic">本条回答未附带引用片段。</p>
      )}
    </div>
  )
}
