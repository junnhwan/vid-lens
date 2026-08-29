'use client'

import { useEffect } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { ChevronLeft, ChevronRight } from 'lucide-react'

export interface PrototypeVariant {
  key: string
  name: string
}

interface Props {
  variants: PrototypeVariant[]
  current: string
  paramKey?: string
}

// 原型专用：底部浮动切换条。仅 dev 环境渲染。
export default function PrototypeSwitcher({ variants, current, paramKey = 'variant' }: Props) {
  const router = useRouter()
  const sp = useSearchParams()

  const idx = Math.max(0, variants.findIndex(v => v.key === current))
  const cur = variants[idx] ?? variants[0]

  const go = (nextIdx: number) => {
    const v = variants[(nextIdx + variants.length) % variants.length]
    const q = new URLSearchParams(sp.toString())
    q.set(paramKey, v.key)
    router.replace(`?${q.toString()}`, { scroll: false })
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement)?.isContentEditable) return
      if (e.key === 'ArrowLeft') go(idx - 1)
      if (e.key === 'ArrowRight') go(idx + 1)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [idx, variants])

  if (process.env.NODE_ENV === 'production') return null

  return (
    <div
      className="fixed bottom-24 left-1/2 -translate-x-1/2 z-[9999] flex items-center gap-1 px-2 py-1.5 rounded-full shadow-lg border border-white/20"
      style={{ background: 'linear-gradient(135deg, #1a1a2e 0%, #16213e 100%)' }}
      role="toolbar"
      aria-label="原型变体切换"
    >
      <button
        type="button"
        onClick={() => go(idx - 1)}
        className="w-8 h-8 rounded-full flex items-center justify-center text-white/80 hover:text-white hover:bg-white/10 transition-colors"
        aria-label="上一个变体"
      >
        <ChevronLeft className="w-4 h-4" />
      </button>
      <div className="px-3 min-w-[180px] text-center">
        <div className="text-[10px] uppercase tracking-widest text-violet-300/70 font-mono">PROTOTYPE</div>
        <div className="text-[13px] font-medium text-white">
          {cur.key} · {cur.name}
        </div>
      </div>
      <button
        type="button"
        onClick={() => go(idx + 1)}
        className="w-8 h-8 rounded-full flex items-center justify-center text-white/80 hover:text-white hover:bg-white/10 transition-colors"
        aria-label="下一个变体"
      >
        <ChevronRight className="w-4 h-4" />
      </button>
    </div>
  )
}
