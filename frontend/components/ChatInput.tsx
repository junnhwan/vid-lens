'use client'

import { useState, useRef, useEffect } from 'react'
import { Send, Square } from 'lucide-react'

export default function ChatInput({
  onSend, onStop, streaming, placeholder, topK, onTopKChange,
}: {
  onSend: (q: string) => void
  onStop: () => void
  streaming: boolean
  placeholder: string
  topK: number
  onTopKChange: (n: number) => void
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
    <div className="rounded-xl border border-stone-200 bg-white focus-within:ring-2 focus-within:ring-amber-600/20 focus-within:border-amber-400 transition-shadow">
      <textarea
        ref={taRef}
        rows={2}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
        }}
        placeholder={placeholder}
        className="w-full bg-transparent px-4 pt-3 pb-1 text-[14px] leading-relaxed text-stone-900 placeholder:text-stone-400 focus:outline-none resize-none"
      />
      <div className="flex items-center gap-3 px-3 pb-2.5">
        <div className="flex items-center gap-2 text-[10px] text-stone-400">
          <span>TopK</span>
          <div className="flex items-center gap-1">
            <button
              onClick={() => onTopKChange(Math.max(1, topK - 1))}
              className="w-5 h-5 rounded border border-stone-200 text-stone-500 hover:bg-stone-50"
            >
              −
            </button>
            <span className="w-5 text-center text-stone-700">{topK}</span>
            <button
              onClick={() => onTopKChange(Math.min(20, topK + 1))}
              className="w-5 h-5 rounded border border-stone-200 text-stone-500 hover:bg-stone-50"
            >
              +
            </button>
          </div>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-[10px] text-stone-400">Enter 发送</span>
          {streaming ? (
            <button
              onClick={onStop}
              className="h-8 px-3 rounded-lg border border-red-300 text-red-700 text-[11px] flex items-center gap-1.5 ui-btn-lift"
            >
              <Square className="w-3.5 h-3.5" />停止
            </button>
          ) : (
            <button
              onClick={send}
              className="h-8 px-3 rounded-lg bg-stone-900 text-white text-[11px] flex items-center gap-1.5 ui-btn-lift hover:bg-stone-800"
            >
              <Send className="w-3.5 h-3.5" />发送
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
