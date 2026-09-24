'use client'

import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { api, clearToken, getToken } from '@/lib/api'
import type { User } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { BrandMark } from '@/components/ui/BrandMark'
import UploadModal from '@/components/UploadModal'
import { useTheme } from '@/components/theme/ThemeProvider'

interface ShellCtx {
  user: User | null
  openUpload: () => void
  uploadRevision: number
  registerLeaveGuard: (guard: (() => boolean) | null) => void
  confirmLeave: () => boolean
}

const ShellContext = createContext<ShellCtx | null>(null)

export function useShell(): ShellCtx {
  const c = useContext(ShellContext)
  if (!c) throw new Error('useShell 必须在 <AppShell> 内调用')
  return c
}

export interface CrumbItem {
  label: string
  href?: string
}

export function useCrumb(items: CrumbItem[]) {
  const { setCrumb } = useContext(CrumbSetter)
  const key = items.map(i => `${i.label}\u0001${i.href || ''}`).join('\u0002')
  useEffect(() => {
    setCrumb(key.split('\u0002').filter(Boolean).map(row => {
      const [label, href] = row.split('\u0001')
      return { label, href: href || undefined }
    }))
  }, [key, setCrumb])
}

const CrumbSetter = createContext<{ setCrumb: (items: CrumbItem[]) => void }>({ setCrumb: () => {} })

const NAV = [
  { href: '/', label: '工作台', icon: 'home', match: (p: string) => p === '/' },
  { href: '/library', label: '视频库', icon: 'video', match: (p: string) => p.startsWith('/library') || p.startsWith('/video') },
  { href: '/kb', label: '知识库', icon: 'folder', match: (p: string) => p.startsWith('/kb') },
  { href: '/chat', label: '问答', icon: 'message', match: (p: string) => p.startsWith('/chat') },
] as const

export default function AppShell({ children }: { children: React.ReactNode }) {
  const router = useRouter()
  const pathname = usePathname()
  const { theme, toggle } = useTheme()
  const [crumb, setCrumb] = useState<CrumbItem[]>([])
  const [user, setUser] = useState<User | null>(null)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploadRevision, setUploadRevision] = useState(0)
  const [railOpen, setRailOpen] = useState(false)
  const leaveGuard = useRef<(() => boolean) | null>(null)
  const restoringHistory = useRef(false)
  const registerLeaveGuard = useCallback((guard: (() => boolean) | null) => { leaveGuard.current = guard }, [])
  const confirmLeave = useCallback(() => leaveGuard.current?.() ?? true, [])

  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!leaveGuard.current) return
      event.preventDefault()
      event.returnValue = ''
    }
    const interceptLink = (event: MouseEvent) => {
      const target = event.target as Element | null
      const link = target?.closest('a[href]') as HTMLAnchorElement | null
      if (!link || !leaveGuard.current || link.target === '_blank' || link.hasAttribute('download') || event.defaultPrevented || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return
      const url = new URL(link.href, location.href)
      if (url.origin !== location.origin || (url.pathname === location.pathname && url.search === location.search)) return
      if (!leaveGuard.current()) event.preventDefault()
    }
    const onPopState = (event: PopStateEvent) => {
      if (restoringHistory.current) { restoringHistory.current = false; return }
      if (leaveGuard.current && !leaveGuard.current()) {
        event.stopImmediatePropagation()
        restoringHistory.current = true
        history.go(1)
      }
    }
    window.addEventListener('beforeunload', beforeUnload)
    document.addEventListener('click', interceptLink, true)
    window.addEventListener('popstate', onPopState, true)
    return () => { window.removeEventListener('beforeunload', beforeUnload); document.removeEventListener('click', interceptLink, true); window.removeEventListener('popstate', onPopState, true) }
  }, [])

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login')
      return
    }
    api.profile().then(setUser).catch(() => { /* 401 由 api 层统一跳登录 */ })
  }, [router])

  useEffect(() => { setRailOpen(false) }, [pathname])
  useEffect(() => {
    if (!railOpen) return
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') setRailOpen(false) }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [railOpen])

  const openUpload = useCallback(() => setUploadOpen(true), [])
  const setCrumbStable = useCallback((items: CrumbItem[]) => setCrumb(items), [])

  const logout = useCallback(() => {
    if (!confirmLeave()) return
    clearToken()
    router.replace('/login')
  }, [router, confirmLeave])

  const initial = (user?.nickname || user?.username || '').trim().charAt(0).toUpperCase() || '·'

  return (
    <ShellContext.Provider value={{ user, openUpload, uploadRevision, registerLeaveGuard, confirmLeave }}>
      <CrumbSetter.Provider value={{ setCrumb: setCrumbStable }}>
        <div className="app">
          {railOpen && <div className="rail-veil" onClick={() => setRailOpen(false)} />}
          <aside id="rail" className={`rail${railOpen ? ' open' : ''}`}>
            <Link href="/" className="brand">
              <BrandMark />
              <div>
                <div className="brand-name">映知</div>
                <div className="brand-sub">VIDLENS</div>
              </div>
            </Link>
            {NAV.map(item => (
              <Link key={item.href} href={item.href} className={`nav-item${item.match(pathname) ? ' active' : ''}`}>
                <Icon name={item.icon} />
                {item.label}
              </Link>
            ))}
            <div className="rail-spacer" />
            <button
              className="nav-item"
              onClick={toggle}
              aria-label={theme === 'dark' ? '切换到浅色' : '切换到深色'}
            >
              <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
              {theme === 'dark' ? '浅色' : '深色'}
            </button>
            <Link href="/settings" className={`nav-item${pathname.startsWith('/settings') ? ' active' : ''}`}>
              <Icon name="settings" />
              设置
            </Link>
            <Link href="/docs" className="nav-item">
              <Icon name="file" />
              文档
            </Link>
            <Link href="/settings" className="rail-user">
              <span className="avatar">{initial}</span>
              <span className="who">
                <b>{user?.nickname || user?.username || '…'}</b>
                <span>{user?.role === 'DEMO' ? '演示账号 · 只读' : '个人工作区'}</span>
              </span>
            </Link>
            <button className="nav-item" onClick={logout} aria-label="退出登录">
              <Icon name="logout" />
              退出登录
            </button>
          </aside>

          <div className="main">
            <header className="topbar">
              <button
                className="topbar-menu"
                onClick={() => setRailOpen(o => !o)}
                aria-label={railOpen ? '关闭菜单' : '打开菜单'}
                aria-expanded={railOpen}
                aria-controls="rail"
              >
                <Icon name={railOpen ? 'x' : 'menu'} />
              </button>
              <nav className="crumb" aria-label="面包屑">
                {crumb.map((item, i) => {
                  const last = i === crumb.length - 1
                  return (
                    <span key={`${item.label}-${i}`} className="crumb-seg">
                      {i > 0 && <span className="div">/</span>}
                      {last || !item.href
                        ? <b>{item.label}</b>
                        : <Link href={item.href}>{item.label}</Link>}
                    </span>
                  )
                })}
              </nav>
            </header>
            <div className="content" id="content">{children}</div>
          </div>
        </div>
        {uploadOpen && <UploadModal onClose={() => setUploadOpen(false)} onUploaded={() => setUploadRevision(n => n + 1)} />}
      </CrumbSetter.Provider>
    </ShellContext.Provider>
  )
}
