'use client'

import { useEffect, useRef, useState, type ReactNode, type Ref } from 'react'
import Link from 'next/link'
import { BookOpen, ChevronDown, MessageCircle, Sparkles } from 'lucide-react'
import type { VideoChatMode } from '@/lib/types'

/** 对话与输入共用：同一宽度、同一左侧起点，贴着侧栏对齐 */
export const CHAT_COL = 'w-full max-w-2xl px-6'

const MODE_ITEMS: {
  key: VideoChatMode
  label: string
  desc: string
  icon: typeof BookOpen
}[] = [
  { key: 'strict_rag', label: '引用问答', desc: '只根据转写回答，带来源片段', icon: BookOpen },
  { key: 'agent', label: 'Agent', desc: '多步检索、对比、归纳', icon: Sparkles },
  { key: 'video_assistant', label: '助手', desc: '不强制检索，适合闲聊追问', icon: MessageCircle },
]

export function modeLabel(mode: VideoChatMode): string {
  return MODE_ITEMS.find(i => i.key === mode)?.label ?? '引用问答'
}

export function ChatHeader({ backHref, backLabel, kicker, title, actions }: {
  backHref: string
  backLabel: string
  kicker?: string
  title: string
  actions?: ReactNode
}) {
  return (
    <header className="shrink-0 h-14 px-6 flex items-center gap-3 border-b border-ink-0/8 bg-paper-0">
      <Link
        href={backHref}
        className="text-[13px] text-ink-4 hover:text-ink-1 transition-colors shrink-0"
      >
        ← {backLabel}
      </Link>
      <div className="h-4 w-px bg-ink-0/10 shrink-0" />
      <div className="min-w-0 flex-1">
        {kicker && <div className="text-[11px] text-ink-4 leading-none mb-0.5">{kicker}</div>}
        <div className="text-[14px] font-medium text-ink-0 truncate leading-tight">{title}</div>
      </div>
      {actions}
    </header>
  )
}

export function ChatModePicker({ mode, onChange, disabled }: {
  mode: VideoChatMode
  onChange: (m: VideoChatMode) => void
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)
  const current = MODE_ITEMS.find(i => i.key === mode) ?? MODE_ITEMS[0]
  const Icon = current.icon

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  useEffect(() => {
    if (disabled) setOpen(false)
  }, [disabled])

  return (
    <div ref={wrapRef} className="relative">
      <button
        type="button"
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen(v => !v)}
        className={`h-8 pl-2.5 pr-2 rounded-lg text-[12px] flex items-center gap-1.5 transition-colors disabled:opacity-50 ${
          mode === 'agent'
            ? 'bg-sienna-500/10 text-sienna-800'
            : 'text-ink-2 hover:bg-ink-0/5'
        }`}
      >
        <Icon className="w-3.5 h-3.5 shrink-0" />
        {current.label}
        <ChevronDown className={`w-3 h-3 text-ink-4 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div
          role="listbox"
          className="absolute bottom-full left-0 mb-2 w-[240px] rounded-xl border border-ink-0/10 bg-paper-0 shadow-[0_8px_30px_rgba(28,25,23,.08)] py-1 z-30"
        >
          {MODE_ITEMS.map(item => {
            const ItemIcon = item.icon
            const on = item.key === mode
            return (
              <button
                key={item.key}
                type="button"
                role="option"
                aria-selected={on}
                onClick={() => {
                  onChange(item.key)
                  setOpen(false)
                }}
                className={`w-full text-left px-3 py-2.5 flex items-start gap-2.5 ${
                  on ? 'bg-sienna-500/8' : 'hover:bg-ink-0/4'
                }`}
              >
                <ItemIcon className={`w-3.5 h-3.5 mt-0.5 shrink-0 ${on ? 'text-sienna-700' : 'text-ink-4'}`} />
                <span>
                  <span className={`block text-[12px] ${on ? 'font-medium text-ink-0' : 'text-ink-1'}`}>
                    {item.label}
                  </span>
                  <span className="block text-[11px] text-ink-4 mt-0.5 leading-snug">{item.desc}</span>
                </span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

export function ChatSidebar({ children }: { children: ReactNode }) {
  return (
    <aside className="w-[220px] shrink-0 border-r border-ink-0/8 bg-paper-0 hidden md:flex flex-col min-h-0 overflow-hidden">
      {children}
    </aside>
  )
}

export function SidebarSection({ title, action, children }: {
  title: string
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="px-3 py-4">
      <div className="text-[11px] text-ink-4 mb-2 px-2 flex items-center justify-between">
        <span>{title}</span>
        {action}
      </div>
      {children}
    </div>
  )
}

export function ChatFooter({ children, footerAction }: {
  children: ReactNode
  hint?: ReactNode
  footerAction?: ReactNode
}) {
  return (
    <div className="shrink-0 bg-paper-1">
      <div className={`${CHAT_COL} pt-3 pb-5`}>
        {children}
        {footerAction && (
          <div className="flex justify-end mt-2">
            {footerAction}
          </div>
        )}
      </div>
    </div>
  )
}

export default function ChatShell({ header, sidebar, children, footer, scrollRef, overlay }: {
  header: ReactNode
  sidebar?: ReactNode
  children: ReactNode
  footer: ReactNode
  scrollRef?: Ref<HTMLDivElement>
  overlay?: ReactNode
}) {
  return (
    <div className="h-dvh flex bg-paper-1 text-ink-0 overflow-hidden ui-root">
      {sidebar}
      <div className="flex-1 flex flex-col min-w-0 min-h-0">
        {header}
        <div className="flex-1 flex flex-col min-h-0 min-w-0">
          <div className="flex-1 min-h-0 relative">
            <div ref={scrollRef} role="main" className="h-full overflow-y-auto scroll-thin">
              <div className={`${CHAT_COL} py-8 space-y-6`}>{children}</div>
            </div>
            {overlay}
          </div>
          {footer}
        </div>
      </div>
    </div>
  )
}
