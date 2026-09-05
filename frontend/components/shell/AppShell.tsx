'use client'

import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { api, getToken } from '@/lib/api'
import type { User } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import UploadModal from '@/components/UploadModal'

// 应用外壳:左侧导航 rail + 顶栏(面包屑 / 搜索 / 上传)。
// 页面通过 useCrumb() 设置面包屑,通过 useShell().openUpload 打开上传模态。

interface ShellCtx {
  user: User | null
  openUpload: () => void
}

const ShellContext = createContext<ShellCtx | null>(null)

export function useShell(): ShellCtx {
  const c = useContext(ShellContext)
  if (!c) throw new Error('useShell 必须在 <AppShell> 内调用')
  return c
}

/** 面包屑由页面自己声明,最后一项加粗(与原型 setCrumb 一致)。 */
export function useCrumb(items: string[]) {
  const { setCrumb } = useContext(CrumbSetter)
  const key = items.join('\u0001')
  useEffect(() => {
    setCrumb(key.split('\u0001'))
  }, [key, setCrumb])
}

const CrumbSetter = createContext<{ setCrumb: (items: string[]) => void }>({ setCrumb: () => {} })

const NAV = [
  { href: '/', label: '工作台', icon: 'home', match: (p: string) => p === '/' },
  { href: '/library', label: '视频库', icon: 'video', match: (p: string) => p.startsWith('/library') || p.startsWith('/video') || p.startsWith('/chat/v') },
  { href: '/kb', label: '知识库', icon: 'folder', match: (p: string) => p.startsWith('/kb') || p.startsWith('/chat/kb') },
] as const

export default function AppShell({ children }: { children: React.ReactNode }) {
  const router = useRouter()
  const pathname = usePathname()
  const [crumb, setCrumb] = useState<string[]>([])
  const [user, setUser] = useState<User | null>(null)
  const [uploadOpen, setUploadOpen] = useState(false)

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login')
      return
    }
    api.profile().then(setUser).catch(() => { /* 401 由 api 层统一跳登录 */ })
  }, [router])

  // 全局 "/" 键 → 视频库并聚焦过滤框(与原型一致)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null
      const typing = t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)
      if (e.key === '/' && !typing) {
        e.preventDefault()
        router.push('/library?focus=1')
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [router])

  const openUpload = useCallback(() => setUploadOpen(true), [])
  const setCrumbStable = useCallback((items: string[]) => setCrumb(items), [])

  const initial = (user?.nickname || user?.username || '').trim().charAt(0).toUpperCase() || '·'

  return (
    <ShellContext.Provider value={{ user, openUpload }}>
      <CrumbSetter.Provider value={{ setCrumb: setCrumbStable }}>
        <div className="app">
          <aside className="rail">
            <Link href="/" className="brand">
              <div className="brand-mark" />
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
            <Link href="/settings" className={`nav-item${pathname.startsWith('/settings') ? ' active' : ''}`}>
              <Icon name="settings" />
              设置
            </Link>
            <Link href="/settings" className="rail-user">
              <span className="avatar">{initial}</span>
              <span className="who">
                <b>{user?.nickname || user?.username || '…'}</b>
                <span>{user?.role === 'DEMO' ? '演示账号 · 只读' : '个人工作区'}</span>
              </span>
            </Link>
          </aside>

          <div className="main">
            <header className="topbar">
              <div className="crumb">
                {crumb.map((item, i) => (
                  i === crumb.length - 1
                    ? <b key={i}>{item}</b>
                    : <span key={i} style={{ display: 'flex', gap: 8 }}>{item}<span className="div">/</span></span>
                ))}
              </div>
              <div className="topbar-spacer" />
              <button className="searchbox" onClick={() => router.push('/library?focus=1')}>
                <Icon name="search" size="sm" />
                搜索视频、知识库、会话
                <kbd>/</kbd>
              </button>
              <button className="btn btn-primary btn-sm" onClick={openUpload}>
                <Icon name="upload" size="sm" />
                上传视频
              </button>
            </header>
            <div className="content" id="content">{children}</div>
          </div>
        </div>
        {uploadOpen && <UploadModal onClose={() => setUploadOpen(false)} />}
      </CrumbSetter.Provider>
    </ShellContext.Provider>
  )
}
