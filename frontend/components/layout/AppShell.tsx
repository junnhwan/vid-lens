'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { Video, Library, Settings, Upload, MessageCircle } from 'lucide-react'
import ThemeSwitch from '@/components/ThemeSwitch'
import UserMenu from '@/components/UserMenu'
import QaRecentShortcuts from '@/components/layout/QaRecentShortcuts'
import { useRole } from '@/lib/useRole'

const NAV = [
  { key: 'library', href: '/', icon: Video, label: '我的视频', desc: '管理与处理' },
  { key: 'qa', href: '/qa', icon: MessageCircle, label: '问答', desc: '单视频 / 知识库' },
  { key: 'kb', href: '/kb', icon: Library, label: '知识库', desc: '跨视频问答' },
  { key: 'settings', href: '/settings', icon: Settings, label: 'AI 配置', desc: 'BYOK 密钥' },
] as const

export function PageHero({ kicker, title, desc, actions }: {
  kicker?: string; title: string; desc?: string; actions?: React.ReactNode
}) {
  return (
    <header className="shrink-0 px-8 pt-7 pb-5 ui-fade-in">
      {kicker && <div className="text-[12px] text-ink-4 mb-1.5">{kicker}</div>}
      <div className="flex items-start justify-between gap-6">
        <div>
          <h1 className="text-[28px] font-semibold text-ink-0 leading-tight tracking-tight text-balance ui-serif">{title}</h1>
          {desc && <p className="text-[14px] text-ink-3 mt-2 max-w-[48ch] leading-relaxed">{desc}</p>}
        </div>
        {actions}
      </div>
    </header>
  )
}

export function MiniStat({ value, label, accent, warn }: { value: number | string; label: string; accent?: boolean; warn?: boolean }) {
  return (
    <div className="px-2 py-1.5 text-center">
      <div className={`text-[18px] font-semibold tabular-nums tracking-tight ${warn ? 'text-sienna-600' : accent ? 'text-moss' : 'text-ink-0'}`}>{value}</div>
      <div className="text-[10px] text-ink-5 mt-0.5">{label}</div>
    </div>
  )
}

export default function AppShell({ children, onUpload }: { children: React.ReactNode; onUpload?: () => void }) {
  const pathname = usePathname()
  const { isDemo } = useRole()
  const active = pathname.startsWith('/qa') || pathname.startsWith('/chat')
    ? 'qa'
    : pathname.startsWith('/kb')
      ? 'kb'
      : pathname.startsWith('/settings')
        ? 'settings'
        : 'library'

  return (
    <div className="h-screen flex bg-paper-1 text-ink-0 overflow-hidden ui-root">
      <aside className="w-[248px] shrink-0 bg-paper-0/70 border-r border-ink-0/8 flex flex-col">
        <div className="px-5 py-6 border-b border-ink-0/8">
          <Link href="/" className="block group">
            <div className="flex items-center gap-2.5">
              <span className="w-7 h-7 rounded-[7px] bg-ink-0 text-paper-0 text-[11px] font-semibold flex items-center justify-center tracking-tight">
                映
              </span>
              <span className="text-[18px] font-semibold tracking-tight text-ink-0 ui-serif group-hover:text-sienna-700 transition-colors duration-200">
                映知
              </span>
            </div>
            <p className="text-[12px] text-ink-4 mt-2.5 leading-relaxed">观之以映，释之以知</p>
          </Link>
        </div>
        <nav className="p-3 space-y-0.5">
          {NAV.map(({ key, href, icon: Icon, label, desc }) => {
            const on = active === key
            return (
              <Link
                key={key}
                href={href}
                className={`relative w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors duration-200 ${
                  on ? 'bg-sienna-500/8 text-ink-0' : 'hover:bg-ink-0/4'
                }`}
              >
                {on && (
                  <span className="absolute left-0 top-2.5 bottom-2.5 w-0.5 rounded-full bg-sienna-500" />
                )}
                <div className={`w-8 h-8 rounded-md flex items-center justify-center ${on ? 'text-sienna-700' : 'text-ink-4'}`}>
                  <Icon className="w-4 h-4" />
                </div>
                <div>
                  <div className={`text-[13px] ${on ? 'font-medium text-ink-0' : 'text-ink-2'}`}>{label}</div>
                  <div className="text-[10px] text-ink-5">{desc}</div>
                </div>
              </Link>
            )
          })}
        </nav>
        <QaRecentShortcuts />
        <div className="flex-1" />
        <div className="p-4 border-t border-ink-0/8 space-y-3">
          {onUpload && !isDemo && (
            <button
              onClick={onUpload}
              className="w-full h-10 rounded-lg bg-ink-0 text-paper-0 text-[13px] font-medium flex items-center justify-center gap-2 hover:bg-ink-1 ui-btn-lift"
            >
              <Upload className="w-4 h-4" />上传视频
            </button>
          )}
          <div className="flex items-center justify-between px-1">
            <ThemeSwitch />
            <UserMenu />
          </div>
        </div>
      </aside>
      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">{children}</div>
    </div>
  )
}
