'use client'

import { useCallback, useEffect } from 'react'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { ChevronLeft, ChevronRight } from 'lucide-react'

export interface PrototypeVariant {
  key: string
  name: string
}

interface Props {
  param?: string
  variants: readonly PrototypeVariant[]
  current: string
  caption?: string
  onReset?: () => void
}

export function PrototypeSwitcher({ param = 'variant', variants, current, caption, onReset }: Props) {
  const router = useRouter()
  const searchParams = useSearchParams()

  const pathname = usePathname()
  const go = useCallback((key: string) => {
    const next = new URLSearchParams(searchParams.toString())
    next.set(param, key)
    router.replace(`${pathname}?${next.toString()}`, { scroll: false })
  }, [searchParams, param, router, pathname])

  const index = Math.max(0, variants.findIndex(v => v.key === current))
  const label = variants[index] ?? variants[0]

  const cycle = useCallback((delta: number) => {
    const next = variants[(index + delta + variants.length) % variants.length]
    if (next) go(next.key)
  }, [go, index, variants])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
      if (event.key === 'ArrowLeft') cycle(-1)
      if (event.key === 'ArrowRight') cycle(1)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [cycle])

  if (process.env.NODE_ENV === 'production') return null

  return (
    <div className="fixed bottom-[72px] left-1/2 -translate-x-1/2 z-[80] flex flex-col items-center gap-2 proto-fade-in">
      {caption && (
        <p className="text-[10px] text-ink-4 bg-paper-0/90 px-2.5 py-1 rounded-full border border-ink-0/8">
          {caption}
        </p>
      )}
      <div className="flex items-center gap-1 px-1.5 py-1.5 rounded-full bg-ink-0 text-paper-0 shadow-[0_12px_32px_color-mix(in_srgb,var(--ink-0)_28%,transparent)]">
        <button
          type="button"
          onClick={() => cycle(-1)}
          className="w-8 h-8 rounded-full flex items-center justify-center text-paper-0/70 hover:text-paper-0 hover:bg-paper-0/10"
          aria-label="上一个方案"
        >
          <ChevronLeft className="w-4 h-4" />
        </button>
        <div className="px-3 text-center min-w-[168px]">
          <div className="text-[12px] font-medium tracking-tight">
            {label?.key} · {label?.name}
          </div>
        </div>
        <button
          type="button"
          onClick={() => cycle(1)}
          className="w-8 h-8 rounded-full flex items-center justify-center text-paper-0/70 hover:text-paper-0 hover:bg-paper-0/10"
          aria-label="下一个方案"
        >
          <ChevronRight className="w-4 h-4" />
        </button>
        {onReset && (
          <>
            <span className="w-px h-3.5 bg-paper-0/15 mx-0.5" />
            <button
              type="button"
              onClick={onReset}
              className="px-2.5 py-1 rounded-full text-[11px] text-paper-0/55 hover:text-paper-0 hover:bg-paper-0/8"
            >
              重置
            </button>
          </>
        )}
      </div>
    </div>
  )
}
