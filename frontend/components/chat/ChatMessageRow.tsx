'use client'

import { useState } from 'react'
import { BookOpen, ChevronDown, Copy, RefreshCw, Brain } from 'lucide-react'
import { CitationCards, renderAnswerWithCites } from '@/components/Citation'
import type { ChatMsg } from '@/components/chat/chatUtils'
import type { ChatTraceStep } from '@/components/chat/traceTypes'

function TraceCollapse({ steps }: { steps: ChatTraceStep[] }) {
  const [open, setOpen] = useState(false)
  const done = steps.filter(s => s.status === 'done').length
  if (steps.length === 0) return null

  return (
    <div className="rounded-xl border border-stone-200 bg-stone-50/80 overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        className="w-full flex items-center justify-between px-3 py-2 text-[11px] text-stone-600 hover:bg-stone-50"
      >
        <span className="flex items-center gap-1.5">
          <Brain className="w-3 h-3" />
          思考与检索轨迹 ({done}/{steps.length})
        </span>
        <ChevronDown className={`w-3.5 h-3.5 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div className="px-3 pb-3 border-t border-stone-100 space-y-1.5">
          {steps.map(s => (
            <div key={s.id} className="flex items-start gap-2 text-[11px] text-stone-600 py-0.5">
              <span className={`mt-1 w-1.5 h-1.5 rounded-full shrink-0 ${
                s.status === 'done' ? 'bg-emerald-500' : s.status === 'running' ? 'bg-amber-500 ui-pulse' : s.status === 'error' ? 'bg-red-500' : 'bg-stone-300'
              }`} />
              <div>
                <span className="font-medium text-stone-700">{s.label}</span>
                {s.detail && <span className="text-stone-500"> · {s.detail}</span>}
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
        <div className="bg-stone-900 text-white text-[14px] leading-relaxed px-4 py-2.5 rounded-2xl rounded-br-md max-w-[85%]">
          {msg.content}
        </div>
      </div>
    )
  }

  const citeIds = (msg.cites || []).map(c => c.id)
  const toggle = (id: string) => onToggleCite(idx, id)
  const showTrace = msg.trace && msg.trace.length > 0 && !msg.streaming

  return (
    <div className="space-y-2 ui-fade-in">
      <div className="flex items-center gap-2 text-[10px] text-stone-400">
        <BookOpen className="w-3 h-3" />
        映知 · {modeLabel}{topK != null ? ` · top_k ${topK}` : ''}
        {msg.streaming && (
          <span className="text-amber-700 flex items-center gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-amber-500 ui-pulse" />生成中
          </span>
        )}
        {msg.degraded && <span>（降级）</span>}
      </div>

      {showTrace && <TraceCollapse steps={msg.trace!} />}

      {msg.error ? (
        <div className="p-4 rounded-xl bg-red-50 border border-red-200 text-[13px] text-red-700">{msg.error}</div>
      ) : (
        <div className={`rounded-2xl border p-4 transition-all duration-300 ${
          msg.streaming ? 'border-amber-200 bg-amber-50/30' : 'border-transparent bg-transparent px-0 py-0'
        }`}>
          <div className={`text-[15px] leading-[1.8] text-stone-900 ${msg.streaming ? '' : ''}`}>
            {renderAnswerWithCites(msg.content, citeIds, toggle, msg.openCiteIds || [])}
            {msg.streaming && <span className="ui-typewriter-cursor" aria-hidden />}
          </div>
        </div>
      )}

      {!msg.streaming && !msg.error && msg.content && (onCopy || onRetry) && (
        <div className="flex items-center gap-3 text-[10px] text-stone-400">
          {onCopy && (
            <button onClick={() => onCopy(msg.content)} className="hover:text-stone-700 flex items-center gap-1 ui-btn-lift">
              <Copy className="w-3 h-3" />复制
            </button>
          )}
          {onRetry && (
            <button onClick={() => onRetry(idx)} className="hover:text-stone-700 flex items-center gap-1 ui-btn-lift">
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
    </div>
  )
}
