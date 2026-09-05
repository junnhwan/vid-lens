'use client'

import { createContext, useCallback, useContext, useRef, useState } from 'react'
import { Icon } from '@/components/ui/Icon'

// 全局 toast:底部居中胶囊,与 docs/prototype 的 .toasts/.toast 一致。
// 用法:const toast = useToast(); toast.success('已复制')

type ToastKind = 'success' | 'error' | 'info'
interface Toast {
  id: number
  kind: ToastKind
  msg: string
}

interface ToastCtx {
  success: (msg: string) => void
  error: (msg: string) => void
  info: (msg: string) => void
}

const Ctx = createContext<ToastCtx | null>(null)

export function useToast(): ToastCtx {
  const c = useContext(Ctx)
  if (!c) throw new Error('useToast 必须在 <ToastProvider> 内调用')
  return c
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const seq = useRef(0)

  const dismiss = useCallback((id: number) => {
    setToasts(prev => prev.filter(t => t.id !== id))
  }, [])

  const push = useCallback((kind: ToastKind, msg: string) => {
    const id = ++seq.current
    setToasts(prev => [...prev, { id, kind, msg }])
    setTimeout(() => dismiss(id), 3000)
  }, [dismiss])

  const api = {
    success: (m: string) => push('success', m),
    error: (m: string) => push('error', m),
    info: (m: string) => push('info', m),
  }

  return (
    <Ctx.Provider value={api}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {toasts.map(t => (
          <div key={t.id} className={`toast${t.kind === 'error' ? ' err' : ''}`}>
            <Icon name={t.kind === 'error' ? 'alert' : t.kind === 'info' ? 'info' : 'check'} />
            <span>{t.msg}</span>
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}
