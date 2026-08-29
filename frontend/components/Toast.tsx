'use client'

import { createContext, useCallback, useContext, useRef, useState, useEffect } from 'react'
import { Check, AlertCircle, Info, X } from 'lucide-react'

// 极简 toast：success / error / info 三态，右上角堆叠，3.5s 自动消失。
// 全局调用：const toast = useToast(); toast.success('已复制')
// 没有引依赖，沿用项目"手搓"风格（SSE/MD 都自写）。

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
    setTimeout(() => dismiss(id), 3500)
  }, [dismiss])

  const api = {
    success: (m: string) => push('success', m),
    error: (m: string) => push('error', m),
    info: (m: string) => push('info', m),
  }

  return (
    <Ctx.Provider value={api}>
      {children}
      {/* Portal 太重，直接 fixed 右上角；z-60 盖过 modal(z-50) */}
      <div className="fixed top-4 right-4 z-[60] flex flex-col gap-2 w-80 max-w-[calc(100vw-2rem)]">
        {toasts.map(t => (
          <ToastCard key={t.id} toast={t} onDismiss={() => dismiss(t.id)} />
        ))}
      </div>
    </Ctx.Provider>
  )
}

function ToastCard({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  const { kind, msg } = toast
  const Icon = kind === 'success' ? Check : kind === 'error' ? AlertCircle : Info
  const accent = kind === 'success' ? 'text-moss' : kind === 'error' ? 'text-rust' : 'text-sienna-700'
  return (
    <div
      className="toast-in flex items-start gap-2.5 bg-paper-0 border border-ink-0/15 shadow-[0_4px_16px_rgba(0,0,0,0.08)] px-3.5 py-2.5"
      role="status"
    >
      <Icon className={`w-4 h-4 mt-0.5 shrink-0 ${accent}`} />
      <p className="flex-1 font-sans text-[13px] leading-snug text-ink-1">{msg}</p>
      <button onClick={onDismiss} className="text-ink-4 hover:text-ink-0 shrink-0 -mt-0.5" aria-label="关闭">
        <X className="w-3.5 h-3.5" />
      </button>
    </div>
  )
}
