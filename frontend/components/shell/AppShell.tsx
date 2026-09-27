'use client'

import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { api, clearToken, getToken } from '@/lib/api'
import type { User } from '@/lib/types'
import UploadModal from '@/components/UploadModal'
import { ShellFrame } from './ShellFrame'
import { ArtifactQueryProvider } from '@/components/artifacts/ArtifactQueryProvider'

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

export default function AppShell({ children }: { children: React.ReactNode }) {
  const router = useRouter()
  const pathname = usePathname()
  const [crumb, setCrumb] = useState<CrumbItem[]>([])
  const [user, setUser] = useState<User | null>(null)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploadRevision, setUploadRevision] = useState(0)
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

  const openUpload = useCallback(() => setUploadOpen(true), [])
  const setCrumbStable = useCallback((items: CrumbItem[]) => setCrumb(items), [])

  const logout = useCallback(() => {
    if (!confirmLeave()) return
    clearToken()
    router.replace('/login')
  }, [router, confirmLeave])

  return (
    <ShellContext.Provider value={{ user, openUpload, uploadRevision, registerLeaveGuard, confirmLeave }}>
      <CrumbSetter.Provider value={{ setCrumb: setCrumbStable }}>
        <ShellFrame pathname={pathname} crumb={crumb}
          user={{ name: user?.nickname || user?.username || '…', detail: user?.role === 'DEMO' ? '演示账号 · 只读' : '个人工作区' }}
          onImport={openUpload} onLogout={logout} products>
          <ArtifactQueryProvider key={user?.id ?? "anonymous"}>{children}</ArtifactQueryProvider>
        </ShellFrame>
        {uploadOpen && <UploadModal onClose={() => setUploadOpen(false)} onUploaded={() => setUploadRevision(n => n + 1)} />}
      </CrumbSetter.Provider>
    </ShellContext.Provider>
  )
}
