'use client'

import { useState, useRef, useEffect, type ReactNode } from 'react'
import { Send, Square } from 'lucide-react'

export default function ChatInput({
  onSend, onStop, streaming, placeholder, leading,
}: {
  onSend: (q: string) => void
  onStop: () => void
  streaming: boolean
  placeholder: string
  leading?: ReactNode
}) {
  const [text, setText] = useState('')
  const taRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    const ta = taRef.current
    if (!ta) return
    ta.style.height = 'auto'
    ta.style.height = `${Math.min(ta.scrollHeight, 120)}px`
  }, [text])

  const send = () => {
    const q = text.trim()
    if (!q || streaming) return
    onSend(q)
    setText('')
  }

  return (
    <div className="rounded-xl border border-ink-0/10 bg-paper-0 focus-within:ring-2 focus-within:ring-sienna-500/20 focus-within:border-sienna-500/40 transition-shadow">
      <textarea
        ref={taRef}
        rows={2}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
        }}
        placeholder={placeholder}
        className="w-full bg-transparent px-4 pt-3 pb-1 text-[14px] leading-relaxed text-ink-0 placeholder:text-ink-5 focus:outline-none resize-none"
      />
      <div className="flex items-center gap-2 px-2.5 pb-2.5">
        {leading}
        <div className="ml-auto flex items-center gap-2">
          {streaming ? (
            <button
              onClick={onStop}
              className="h-8 px-3 rounded-lg border border-rust/30 text-rust text-[11px] flex items-center gap-1.5 ui-btn-lift"
            >
              <Square className="w-3.5 h-3.5" />停止
            </button>
          ) : (
            <button
              onClick={send}
              className="h-8 px-3 rounded-lg bg-ink-0 text-paper-0 text-[11px] flex items-center gap-1.5 ui-btn-lift hover:bg-ink-1"
            >
              <Send className="w-3.5 h-3.5" />发送
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
