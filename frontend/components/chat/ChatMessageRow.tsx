'use client'

import { BookOpen, Copy, RefreshCw } from 'lucide-react'
import type { ChatMode } from '@/lib/types'
import { CitationCards, renderAnswerWithCites } from '@/components/Citation'
import type { ChatMsg } from '@/components/chat/chatUtils'

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
      {msg.error ? (
        <div className="p-4 rounded-xl bg-red-50 border border-red-200 text-[13px] text-red-700">{msg.error}</div>
      ) : (
        <div className={`text-[15px] leading-[1.8] text-stone-900 ${msg.streaming ? 'caret' : ''}`}>
          {renderAnswerWithCites(msg.content, citeIds, toggle, msg.openCiteIds || [])}
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
        <CitationCards refs={msg.cites} openIds={msg.openCiteIds || []} />
      )}
    </div>
  )
}
