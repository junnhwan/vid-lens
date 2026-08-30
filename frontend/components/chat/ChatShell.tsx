'use client'

import { useEffect, useRef, useState, type ReactNode, type Ref } from 'react'
import Link from 'next/link'
import { BookOpen, ChevronDown, MessageCircle, Sparkles } from 'lucide-react'
import type { VideoChatMode } from '@/lib/types'
import ChatSplitLayout from '@/components/chat/ChatSplitLayout'

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
    <header className="shrink-0 bg-paper-0/80 border-b border-ink-0/8 px-6 h-14 flex items-center gap-4">
      <Link href={backHref} className="flex items-center gap-2 text-ink-4 hover:text-ink-1 transition-colors text-[12px] shrink-0">
        <span className="sr-only">返回</span>
        ← {backLabel}
      </Link>
      <div className="h-5 w-px bg-ink-0/10 shrink-0" />
      <div className="min-w-0 flex-1">
        {kicker && <div className="text-[10px] text-ink-4">{kicker}</div>}
        <div className={`text-[15px] font-medium text-ink-0 truncate ${kicker ? 'ui-serif' : ''}`}>
          {title}
        </div>
      </div>
      {actions}
    </header>
  )
}

/** 提问方式：放在输入框旁，而不是顶栏技术开关 */
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
        className={`h-8 pl-2.5 pr-2 rounded-lg text-[12px] flex items-center gap-1.5 ui-btn-lift ${
          mode === 'agent'
            ? 'bg-sienna-500/10 text-sienna-800'
            : 'text-ink-2 hover:bg-ink-0/4'
        } disabled:opacity-50`}
      >
        <Icon className="w-3.5 h-3.5 shrink-0" />
        {current.label}
        <ChevronDown className={`w-3 h-3 text-ink-4 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div
          role="listbox"
          className="absolute bottom-full left-0 mb-2 w-[240px] rounded-xl border border-ink-0/10 bg-paper-0 shadow-lg py-1 z-30"
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
    <aside className="w-52 shrink-0 border-r border-ink-0/8 bg-paper-0/70 p-4 hidden md:block overflow-y-auto">
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
    <div>
      <div className="text-[11px] text-ink-4 mb-2 flex items-center justify-between">
        <span>{title}</span>
        {action}
      </div>
      {children}
    </div>
  )
}

export function ChatFooter({ sidebarWidth = 'w-52', children, hint, footerAction }: {
  sidebarWidth?: string
  children: ReactNode
  hint?: ReactNode
  footerAction?: ReactNode
}) {
  return (
    <footer className="shrink-0 bg-paper-0/80 border-t border-ink-0/8">
      <div className="flex">
        <div className={`${sidebarWidth} shrink-0 hidden md:block`} />
        <div className="flex-1 px-6 py-4 max-w-2xl">
          {children}
          {(hint || footerAction) && (
            <div className="flex items-center justify-between mt-2 text-[10px] text-ink-4">
              <span>{hint}</span>
              {footerAction}
            </div>
          )}
        </div>
      </div>
    </footer>
  )
}

export default function ChatShell({ header, sidebar, children, footer, scrollRef, tracePanel, overlay }: {
  header: ReactNode
  sidebar?: ReactNode
  children: ReactNode
  footer: ReactNode
  scrollRef?: Ref<HTMLDivElement>
  tracePanel?: ReactNode
  overlay?: ReactNode
}) {
  return (
    <div className="h-screen flex flex-col bg-paper-1 text-ink-0 overflow-hidden ui-root">
      {header}
      <div className="flex-1 flex min-h-0">
        {sidebar}
        {tracePanel ? (
          <ChatSplitLayout scrollRef={scrollRef} tracePanel={tracePanel}>
            {children}
          </ChatSplitLayout>
        ) : (
          <div className="flex-1 flex flex-col min-h-0 min-w-0 relative">
            <div ref={scrollRef} role="main" className="flex-1 overflow-y-auto scroll-thin px-6 py-6">
              <div className="max-w-2xl mx-auto space-y-6">{children}</div>
            </div>
            {overlay}
          </div>
        )}
      </div>
      {footer}
    </div>
  )
}
