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
    <header className="shrink-0 px-8 pt-8 pb-6 bg-gradient-to-b from-[#faf8f5] to-transparent ui-fade-in">
      {kicker && <div className="text-[11px] text-stone-500 uppercase tracking-wider mb-1">{kicker}</div>}
      <div className="flex items-start justify-between gap-6">
        <div>
          <h1 className="text-[32px] font-semibold text-stone-900 leading-tight ui-serif">{title}</h1>
          {desc && <p className="text-[14px] text-stone-500 mt-2 max-w-lg leading-relaxed">{desc}</p>}
        </div>
        {actions}
      </div>
    </header>
  )
}

export function MiniStat({ value, label, accent, warn }: { value: number | string; label: string; accent?: boolean; warn?: boolean }) {
  return (
    <div className="p-2 rounded-lg bg-white border border-stone-200 text-center ui-card-hover">
      <div className={`text-[18px] font-semibold tabular-nums ${warn ? 'text-amber-600' : accent ? 'text-emerald-700' : 'text-stone-800'}`}>{value}</div>
      <div className="text-[9px] text-stone-400 mt-0.5">{label}</div>
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
    <div className="h-screen flex bg-[#f7f4ef] text-stone-800 overflow-hidden ui-root">
      <aside className="w-[260px] shrink-0 bg-[#faf8f5] border-r border-stone-200 flex flex-col">
        <div className="p-6 border-b border-stone-200">
          <Link href="/" className="block group">
            <div className="text-[22px] font-semibold tracking-tight text-stone-900 ui-serif group-hover:text-amber-900 transition-colors">映知</div>
            <p className="text-[12px] text-stone-500 mt-1 italic leading-relaxed">观之以映，释之以知</p>
          </Link>
        </div>
        <nav className="p-4 space-y-1">
          {NAV.map(({ key, href, icon: Icon, label, desc }) => {
            const on = active === key
            return (
              <Link
                key={key}
                href={href}
                className={`flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all duration-200 ${
                  on ? 'bg-amber-50/80 shadow-sm' : 'hover:bg-stone-100/80'
                }`}
              >
                <div className={`w-8 h-8 rounded-lg flex items-center justify-center ${on ? 'bg-amber-100 text-amber-800' : 'bg-stone-100 text-stone-500'}`}>
                  <Icon className="w-4 h-4" />
                </div>
                <div>
                  <div className={`text-[13px] ${on ? 'font-medium text-stone-900' : 'text-stone-700'}`}>{label}</div>
                  <div className="text-[10px] text-stone-400">{desc}</div>
                </div>
              </Link>
            )
          })}
        </nav>
        <QaRecentShortcuts />
        <div className="flex-1" />
        <div className="p-4 border-t border-stone-200 space-y-3">
          {onUpload && !isDemo && (
            <button
              onClick={onUpload}
              className="w-full h-10 rounded-lg bg-stone-900 text-[#faf8f5] text-[13px] font-medium flex items-center justify-center gap-2 hover:bg-stone-800 ui-btn-lift"
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
