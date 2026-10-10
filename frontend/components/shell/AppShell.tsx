import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { usePathname, useRouter } from '@/lib/router'
import { api, clearToken, getToken } from '@/lib/api'
import type { User } from '@/lib/types'
import UploadModal from '@/components/UploadModal'
import { ShellFrame } from './ShellFrame'
import { ArtifactQueryProvider } from '@/components/artifacts/ArtifactQueryProvider'
import { useVideoAIPreflight } from '@/components/settings/VideoAIPreflight'
import { useLeaveGuard } from './useLeaveGuard'
import { clearSummaryViewState, setSummaryViewOwner } from '@/lib/summaryViewState'

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
  const hasToken = Boolean(getToken())
  const [crumb, setCrumb] = useState<CrumbItem[]>([])
  const [user, setUser] = useState<User | null>(null)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploadRevision, setUploadRevision] = useState(0)
  const uploadPreflight = useVideoAIPreflight('upload')
  const requestUploadAI = uploadPreflight.request
  const { registerLeaveGuard, confirmLeave, clearLeaveGuard } = useLeaveGuard()
  useEffect(() => { setSummaryViewOwner(user?.id ?? null) }, [user?.id])

  useEffect(() => {
    if (!hasToken) {
      router.replace('/login')
      return
    }
    let active = true
    api.profile().then(profile => {
      if (active) setUser(profile)
    }).catch(() => { /* 401 由 api 层统一跳登录 */ })
    return () => { active = false }
  }, [hasToken, router])

  const openUpload = useCallback(() => {
    requestUploadAI('导入视频', () => setUploadOpen(true))
  }, [requestUploadAI])
  const setCrumbStable = useCallback((items: CrumbItem[]) => setCrumb(items), [])

  const logout = useCallback(() => {
    if (!confirmLeave()) return
    clearLeaveGuard()
    clearSummaryViewState()
    clearToken()
    router.replace('/login')
  }, [router, confirmLeave, clearLeaveGuard])

  // Avoid mounting protected pages (and their API effects) during a redirect.
  if (!hasToken) return null

  return (
    <ShellContext.Provider value={{ user, openUpload, uploadRevision, registerLeaveGuard, confirmLeave }}>
      <CrumbSetter.Provider value={{ setCrumb: setCrumbStable }}>
        <ShellFrame pathname={pathname} crumb={crumb}
          user={{ name: user?.nickname || user?.username || '…', detail: user?.role === 'DEMO' ? '演示账号 · 只读' : '个人工作区' }}
          onImport={pathname === '/' || pathname === '/library' || pathname.startsWith('/video/') ? undefined : openUpload} onLogout={logout} products>
          <ArtifactQueryProvider key={user?.id ?? "anonymous"}>
            {children}
            {uploadOpen && <UploadModal onClose={() => setUploadOpen(false)} onUploaded={() => setUploadRevision(n => n + 1)} />}
            {uploadPreflight.dialog}
          </ArtifactQueryProvider>
        </ShellFrame>
      </CrumbSetter.Provider>
    </ShellContext.Provider>
  )
}
