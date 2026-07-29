'use client'

import { useState, useRef, useEffect } from 'react'
import { Send, Trash2, Square } from 'lucide-react'

// 底部输入区：textarea + TopK ± + 发送/停止。
// Enter 发送（Shift+Enter 换行）。流式中显示停止按钮。
export default function ChatInput({
  onSend, onStop, streaming, placeholder, topK, onTopKChange, onClear,
}: {
  onSend: (q: string) => void
  onStop: () => void
  streaming: boolean
  placeholder: string
  topK: number
  onTopKChange: (n: number) => void
  onClear?: () => void
}) {
  const [text, setText] = useState('')
  const taRef = useRef<HTMLTextAreaElement>(null)

  // 自适应高度
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
    <div className="border border-ink-0/20 bg-paper-1 focus-within:border-sienna-500 transition-colors">
      <textarea
        ref={taRef}
        rows={2}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
        }}
        placeholder={placeholder}
        className="w-full bg-transparent px-4 pt-3 pb-1 font-sans text-[14px] leading-relaxed text-ink-0 placeholder:text-ink-4 focus:outline-none resize-none"
      />
      <div className="flex items-center gap-3 px-3 pb-2.5">
        {/* TopK */}
        <div className="flex items-center gap-2 font-mono text-[10px] text-ink-4">
          <span>TopK</span>
          <div className="flex items-center gap-1">
            <button onClick={() => onTopKChange(Math.max(1, topK - 1))} className="w-5 h-5 border border-ink-0/20 text-ink-3 hover:bg-ink-0/[.04]">−</button>
            <span className="w-5 text-center text-ink-2">{topK}</span>
            <button onClick={() => onTopKChange(Math.min(20, topK + 1))} className="w-5 h-5 border border-ink-0/20 text-ink-3 hover:bg-ink-0/[.04]">+</button>
          </div>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <span className="font-sans text-[10px] text-ink-4">Enter 发送</span>
          {streaming ? (
            <button onClick={onStop} className="btn-line h-8 px-3.5 font-mono text-[11px] flex items-center gap-1.5 text-rust border-rust/40">
              <Square className="w-3.5 h-3.5" /> 停止
            </button>
          ) : (
            <button onClick={send} className="btn-ink h-8 px-3.5 font-mono text-[11px] flex items-center gap-1.5">
              <Send className="w-3.5 h-3.5" /> Send
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
