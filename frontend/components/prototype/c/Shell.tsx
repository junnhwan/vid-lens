'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import {
  Video, Settings, LogIn, Upload, MessageCircle,
} from 'lucide-react'

export type ProtoPage = 'dashboard' | 'chat' | 'settings' | 'login'

const NAV: { page: ProtoPage; href: string; icon: React.ReactNode; label: string; desc: string }[] = [
  { page: 'dashboard', href: '/prototype/dashboard', icon: <Video className="w-4 h-4" />, label: '工作台原型', desc: '视频库布局' },
  { page: 'chat', href: '/prototype/agent-chat', icon: <MessageCircle className="w-4 h-4" />, label: 'Agent 原型', desc: '问答 UI' },
  { page: 'settings', href: '/prototype/settings', icon: <Settings className="w-4 h-4" />, label: '设置原型', desc: 'AI 配置布局' },
]

export function detectPage(pathname: string): ProtoPage {
  if (pathname.startsWith('/prototype/agent-chat')) return 'chat'
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
    <div className="h-[100dvh] flex bg-paper-1 text-ink-0 overflow-hidden proto-root">
      <aside className="w-[248px] shrink-0 bg-paper-0/70 border-r border-ink-0/8 flex flex-col">
        <div className="px-5 py-6 border-b border-ink-0/8">
          <Link href="/prototype" className="block group">
            <div className="flex items-center gap-2.5">
              <span className="w-7 h-7 rounded-[7px] bg-ink-0 text-paper-0 text-[11px] font-semibold flex items-center justify-center tracking-tight">
                映
              </span>
              <span className="text-[18px] font-semibold tracking-tight text-ink-0 group-hover:text-sienna-700 transition-colors duration-200">
                映知
              </span>
            </div>
            <p className="text-[12px] text-ink-4 mt-2.5 leading-relaxed">观之以映，释之以知</p>
          </Link>
        </div>

        <nav className="p-3 space-y-0.5">
          {NAV.map(n => (
            <NavItem key={n.page} {...n} active={active === n.page} />
          ))}
        </nav>

        <div className="flex-1" />

        <div className="p-4 border-t border-ink-0/8 space-y-2">
          {onUpload && (
            <button
              onClick={onUpload}
              className="w-full h-10 rounded-lg bg-ink-0 text-paper-0 text-[13px] font-medium flex items-center justify-center gap-2 hover:bg-ink-1 proto-btn-lift"
            >
              <Upload className="w-4 h-4" />上传视频
            </button>
          )}
          <Link
            href="/"
            className="w-full h-9 rounded-lg text-[12px] text-ink-3 flex items-center justify-center hover:text-ink-1 transition-colors"
          >
            进入正式产品
          </Link>
          <Link
            href="/prototype/login"
            className="w-full h-9 rounded-lg text-[12px] text-ink-4 flex items-center justify-center gap-1.5 hover:text-ink-2 transition-colors"
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
      className={`relative w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors duration-200 ${
        active ? 'bg-sienna-500/8 text-ink-0' : 'hover:bg-ink-0/4'
      }`}
    >
      {active && (
        <span className="absolute left-0 top-2.5 bottom-2.5 w-0.5 rounded-full bg-sienna-500" />
      )}
      <div className={`w-8 h-8 rounded-md flex items-center justify-center ${
        active ? 'text-sienna-700' : 'text-ink-4'
      }`}>
        {icon}
      </div>
      <div>
        <div className={`text-[13px] ${active ? 'font-medium text-ink-0' : 'text-ink-2'}`}>{label}</div>
        <div className="text-[10px] text-ink-5">{desc}</div>
      </div>
    </Link>
  )
}

/** 页面 hero 区通用样式 */
export function PageHero({ kicker, title, desc, actions }: {
  kicker?: string; title: string; desc?: string; actions?: React.ReactNode
}) {
  return (
    <header className="shrink-0 px-8 pt-7 pb-5 proto-fade-in">
      {kicker && <div className="text-[12px] text-ink-4 mb-1.5">{kicker}</div>}
      <div className="flex items-start justify-between gap-6">
        <div>
          <h1 className="text-[28px] font-semibold text-ink-0 leading-tight tracking-tight text-balance">{title}</h1>
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
      <div className={`text-[18px] font-semibold tabular-nums tracking-tight ${
        warn ? 'text-sienna-600' : accent ? 'text-moss' : 'text-ink-0'
      }`}>{value}</div>
      <div className="text-[10px] text-ink-5 mt-0.5">{label}</div>
    </div>
  )
}

export function ChatLink({ taskId, label }: { taskId: number; label?: string }) {
  return (
    <Link
      href={`/chat/${taskId}`}
      className="inline-flex items-center gap-1 text-[11px] text-sienna-700 hover:text-sienna-600 font-medium proto-btn-lift"
    >
      <MessageCircle className="w-3 h-3" />{label || '问答'}
    </Link>
  )
}

/** 开发环境：底部导航（正式产品 vs 设计原型） */
export function ProtoPageNav() {
  const pathname = usePathname()
  if (process.env.NODE_ENV === 'production') return null

  const product = [
    { href: '/', label: '首页' },
    { href: '/chat/101', label: '问答' },
    { href: '/kb', label: '知识库' },
  ]

  const proto = [
    { href: '/prototype', label: '入口' },
    { href: '/prototype/dashboard', label: '工作台' },
    { href: '/prototype/agent-chat', label: 'Agent' },
  ]

  return (
    <nav
      aria-label="原型切换"
      className="fixed bottom-5 left-1/2 -translate-x-1/2 z-[80] proto-fade-in"
    >
      <div className="flex items-center gap-1 px-1.5 py-1.5 rounded-full bg-ink-0 text-paper-0 shadow-[0_12px_32px_color-mix(in_srgb,var(--ink-0)_28%,transparent)]">
        <span className="text-[10px] text-paper-0/45 px-2 font-medium">产品</span>
        {product.map(p => (
          <NavPill key={p.href} href={p.href} label={p.label} active={pathname === p.href || (p.href !== '/' && pathname.startsWith(p.href))} />
        ))}
        <span className="w-px h-3.5 bg-paper-0/15 mx-0.5" />
        <span className="text-[10px] text-sienna-400 px-2 font-medium">原型</span>
        {proto.map(p => (
          <NavPill key={p.href} href={p.href} label={p.label} active={pathname === p.href || pathname.startsWith(p.href + '/')} />
        ))}
      </div>
    </nav>
  )
}

function NavPill({ href, label, active }: { href: string; label: string; active?: boolean }) {
  return (
    <Link
      href={href}
      className={`px-2.5 py-1 rounded-full text-[11px] transition-colors duration-150 ${
        active ? 'bg-paper-0/12 text-paper-0 font-medium' : 'text-paper-0/55 hover:text-paper-0 hover:bg-paper-0/8'
      }`}
    >
      {label}
    </Link>
  )
}
