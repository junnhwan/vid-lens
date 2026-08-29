'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import {
  Video, Library, Settings, LogIn, Upload, MessageCircle,
} from 'lucide-react'

export type ProtoPage = 'dashboard' | 'kb' | 'chat' | 'settings' | 'login'

const NAV: { page: ProtoPage; href: string; icon: React.ReactNode; label: string; desc: string }[] = [
  { page: 'dashboard', href: '/prototype/dashboard', icon: <Video className="w-4 h-4" />, label: '我的视频', desc: '管理与处理' },
  { page: 'kb', href: '/prototype/kb', icon: <Library className="w-4 h-4" />, label: '知识库', desc: '跨视频问答' },
  { page: 'settings', href: '/prototype/settings', icon: <Settings className="w-4 h-4" />, label: 'AI 配置', desc: 'BYOK 密钥' },
]

export function detectPage(pathname: string): ProtoPage {
  if (pathname.startsWith('/prototype/kb')) return 'kb'
  if (pathname.startsWith('/prototype/chat')) return 'chat'
  if (pathname.startsWith('/prototype/settings')) return 'settings'
  if (pathname.startsWith('/prototype/login')) return 'login'
  return 'dashboard'
}

interface ShellProps {
  active: ProtoPage
  children: React.ReactNode
  onUpload?: () => void
  /** 聊天页自带布局，不需要侧栏 */
  bare?: boolean
  /** 右侧主区顶部 hero（可选） */
  hero?: React.ReactNode
}

export function ProtoShell({ active, children, onUpload, bare, hero }: ShellProps) {
  if (bare) return <>{children}</>

  return (
    <div className="h-screen flex bg-[#f7f4ef] text-stone-800 overflow-hidden proto-root">
      <aside className="w-[260px] shrink-0 bg-[#faf8f5] border-r border-stone-200 flex flex-col">
        <div className="p-6 border-b border-stone-200">
          <Link href="/prototype/dashboard" className="block group">
            <div className="text-[22px] font-semibold tracking-tight text-stone-900 proto-serif group-hover:text-amber-900 transition-colors duration-200">
              映知
            </div>
            <p className="text-[12px] text-stone-500 mt-1 italic leading-relaxed">观之以映，释之以知</p>
          </Link>
        </div>

        <nav className="p-4 space-y-1">
          {NAV.map(n => (
            <NavItem key={n.page} {...n} active={active === n.page || (active === 'chat' && n.page === 'dashboard')} />
          ))}
        </nav>

        <div className="flex-1" />

        <div className="p-4 border-t border-stone-200 space-y-2">
          {onUpload && (
            <button
              onClick={onUpload}
              className="w-full h-10 rounded-lg bg-stone-900 text-[#faf8f5] text-[13px] font-medium flex items-center justify-center gap-2 hover:bg-stone-800 proto-btn-lift transition-all duration-200"
            >
              <Upload className="w-4 h-4" />上传视频
            </button>
          )}
          <Link
            href="/prototype/login"
            className="w-full h-9 rounded-lg border border-stone-200 bg-white text-[12px] text-stone-500 flex items-center justify-center gap-1.5 hover:border-stone-300 hover:text-stone-700 transition-colors"
          >
            <LogIn className="w-3.5 h-3.5" />登录页原型
          </Link>
        </div>
      </aside>

      <div className="flex-1 flex flex-col min-w-0">
        {hero}
        <div className="flex-1 min-h-0 overflow-hidden">{children}</div>
      </div>
    </div>
  )
}

function NavItem({ href, icon, label, desc, active }: {
  href: string; icon: React.ReactNode; label: string; desc: string; active?: boolean
}) {
  return (
    <Link
      href={href}
      className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all duration-200 ${
        active ? 'bg-amber-50/80 shadow-sm' : 'hover:bg-stone-100/80 hover:translate-x-0.5'
      }`}
    >
      <div className={`w-8 h-8 rounded-lg flex items-center justify-center transition-colors duration-200 ${
        active ? 'bg-amber-100 text-amber-800' : 'bg-stone-100 text-stone-500'
      }`}>
        {icon}
      </div>
      <div>
        <div className={`text-[13px] ${active ? 'font-medium text-stone-900' : 'text-stone-700'}`}>{label}</div>
        <div className="text-[10px] text-stone-400">{desc}</div>
      </div>
    </Link>
  )
}

/** 页面 hero 区通用样式 */
export function PageHero({ kicker, title, desc, actions }: {
  kicker?: string; title: string; desc?: string; actions?: React.ReactNode
}) {
  return (
    <header className="shrink-0 px-8 pt-8 pb-6 bg-gradient-to-b from-[#faf8f5] to-transparent proto-fade-in">
      {kicker && <div className="text-[11px] text-stone-500 uppercase tracking-wider mb-1">{kicker}</div>}
      <div className="flex items-start justify-between gap-6">
        <div>
          <h1 className="text-[32px] font-semibold text-stone-900 leading-tight proto-serif">{title}</h1>
          {desc && <p className="text-[14px] text-stone-500 mt-2 max-w-lg leading-relaxed">{desc}</p>}
        </div>
        {actions}
      </div>
    </header>
  )
}

export function MiniStat({ value, label, accent, warn }: { value: number | string; label: string; accent?: boolean; warn?: boolean }) {
  return (
    <div className="p-2 rounded-lg bg-white border border-stone-200 text-center proto-card-hover">
      <div className={`text-[18px] font-semibold tabular-nums ${
        warn ? 'text-amber-600' : accent ? 'text-emerald-700' : 'text-stone-800'
      }`}>{value}</div>
      <div className="text-[9px] text-stone-400 mt-0.5">{label}</div>
    </div>
  )
}

export function ChatLink({ taskId, label }: { taskId: number; label?: string }) {
  return (
    <Link
      href={`/prototype/chat/${taskId}`}
      className="inline-flex items-center gap-1 text-[11px] text-amber-800 hover:text-amber-900 font-medium proto-btn-lift"
    >
      <MessageCircle className="w-3 h-3" />{label || '问答'}
    </Link>
  )
}

/** 开发环境：底部页面导航 */
export function ProtoPageNav() {
  const pathname = usePathname()
  if (process.env.NODE_ENV === 'production') return null

  const pages = [
    { href: '/prototype/dashboard', label: '视频库' },
    { href: '/prototype/qa-navigation', label: '问答入口' },
    { href: '/prototype/agent-chat', label: 'Agent UI' },
    { href: '/prototype/kb', label: '知识库' },
    { href: '/prototype/chat/101', label: '单视频问答' },
    { href: '/prototype/kb/1', label: '跨视频问答' },
    { href: '/prototype/settings', label: '设置' },
    { href: '/prototype/login', label: '登录' },
  ]

  return (
    <div
      className="fixed bottom-6 left-1/2 -translate-x-1/2 z-[9999] flex items-center gap-1 px-2 py-1.5 rounded-full shadow-lg border border-white/20 proto-fade-in"
      style={{ background: 'linear-gradient(135deg, #1a1a2e 0%, #16213e 100%)' }}
    >
      <span className="text-[9px] uppercase tracking-widest text-violet-300/70 font-mono px-2">原型预览</span>
      {pages.map(p => {
        const on = pathname === p.href || (p.href !== '/prototype/dashboard' && pathname.startsWith(p.href.replace(/\/\d+$/, '')))
        return (
          <Link
            key={p.href}
            href={p.href}
            className={`px-2.5 py-1 rounded-full text-[11px] transition-all duration-150 ${
              on ? 'bg-white/15 text-white font-medium' : 'text-white/60 hover:text-white hover:bg-white/10'
            }`}
          >
            {p.label}
          </Link>
        )
      })}
    </div>
  )
}
