'use client'

import Link from 'next/link'
import type { ReactNode, Ref } from 'react'
import type { VideoChatMode } from '@/lib/types'
import ChatSplitLayout from '@/components/chat/ChatSplitLayout'

export function ChatHeader({ backHref, backLabel, kicker, title, actions }: {
  backHref: string
  backLabel: string
  kicker: string
  title: string
  actions?: ReactNode
}) {
  return (
    <header className="shrink-0 bg-[#faf8f5] border-b border-stone-200 px-6 h-14 flex items-center gap-4">
      <Link href={backHref} className="flex items-center gap-2 text-stone-500 hover:text-stone-800 transition-colors text-[12px]">
        <span className="sr-only">返回</span>
        ← {backLabel}
      </Link>
      <div className="h-5 w-px bg-stone-200" />
      <div className="min-w-0 flex-1">
        <div className="text-[10px] text-stone-400">{kicker}</div>
        <div className="text-[15px] font-medium text-stone-900 truncate ui-serif">{title}</div>
      </div>
      {actions}
    </header>
  )
}

export function ModeToggle({ mode, onChange }: {
  mode: 'video_assistant' | 'strict_rag'
  onChange: (m: 'video_assistant' | 'strict_rag') => void
}) {
  return (
    <div className="flex rounded-lg border border-stone-200 overflow-hidden text-[11px]">
      <button
        onClick={() => onChange('video_assistant')}
        className={`px-3 py-1.5 transition-colors ${mode === 'video_assistant' ? 'bg-stone-900 text-white' : 'text-stone-500 hover:bg-stone-50'}`}
      >
        普通
      </button>
      <button
        onClick={() => onChange('strict_rag')}
        className={`px-3 py-1.5 transition-colors ${mode === 'strict_rag' ? 'bg-stone-900 text-white' : 'text-stone-500 hover:bg-stone-50'}`}
      >
        严格 RAG
      </button>
    </div>
  )
}

/** 单视频聊天：普通 / 严格 RAG / Agent SSE */
export function VideoModeToggle({ mode, onChange, disabled }: {
  mode: VideoChatMode
  onChange: (m: VideoChatMode) => void
  disabled?: boolean
}) {
  const items: { key: VideoChatMode; label: string; title: string }[] = [
    { key: 'strict_rag', label: '严格 RAG', title: '标准检索增强问答' },
    { key: 'video_assistant', label: '普通', title: '普通视频助手' },
    { key: 'agent', label: 'Agent', title: '多步工具 Agent（单视频）' },
  ]
  return (
    <div className={`flex flex-wrap rounded-lg border border-stone-200 overflow-hidden text-[11px] max-w-full ${disabled ? 'opacity-50 pointer-events-none' : ''}`}>
      {items.map(item => (
        <button
          key={item.key}
          type="button"
          title={item.title}
          onClick={() => onChange(item.key)}
          className={`px-2.5 sm:px-3 py-1.5 transition-colors whitespace-nowrap ${
            mode === item.key ? 'bg-stone-900 text-white' : 'text-stone-500 hover:bg-stone-50'
          }`}
        >
          {item.label}
        </button>
      ))}
    </div>
  )
}

export function ChatSidebar({ children }: { children: ReactNode }) {
  return (
    <aside className="w-56 shrink-0 border-r border-stone-200 bg-[#faf8f5] p-5 hidden md:block overflow-y-auto">
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
      <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-2 flex items-center justify-between">
        <span>{title}</span>
        {action}
      </div>
      {children}
    </div>
  )
}

export function ChatFooter({ sidebarWidth = 'w-56', children, hint, footerAction }: {
  sidebarWidth?: string
  children: ReactNode
  hint?: ReactNode
  footerAction?: ReactNode
}) {
  return (
    <footer className="shrink-0 bg-[#faf8f5] border-t border-stone-200">
      <div className="flex">
        <div className={`${sidebarWidth} shrink-0 hidden md:block`} />
        <div className="flex-1 px-6 py-4 max-w-2xl">
          {children}
          {(hint || footerAction) && (
            <div className="flex items-center justify-between mt-2 text-[10px] text-stone-400">
              <span>{hint}</span>
              {footerAction}
            </div>
          )}
        </div>
      </div>
    </footer>
  )
}

export default function ChatShell({ header, sidebar, children, footer, scrollRef, tracePanel }: {
  header: ReactNode
  sidebar?: ReactNode
  children: ReactNode
  footer: ReactNode
  scrollRef?: Ref<HTMLDivElement>
  tracePanel?: ReactNode
}) {
  return (
    <div className="h-screen flex flex-col bg-[#f7f4ef] text-stone-800 overflow-hidden ui-root">
      {header}
      <div className="flex-1 flex min-h-0">
        {sidebar}
        {tracePanel ? (
          <ChatSplitLayout scrollRef={scrollRef} tracePanel={tracePanel}>
            {children}
          </ChatSplitLayout>
        ) : (
          <div ref={scrollRef} role="main" className="flex-1 overflow-y-auto scroll-thin px-6 py-6">
            <div className="max-w-2xl mx-auto space-y-6">{children}</div>
          </div>
        )}
      </div>
      {footer}
    </div>
  )
}
