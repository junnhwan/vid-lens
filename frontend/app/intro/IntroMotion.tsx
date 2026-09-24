'use client'

import { useEffect, useRef } from 'react'

export function IntroMotion({ children }: { children: React.ReactNode }) {
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const root = rootRef.current
    if (!root) return

    // 顶栏滚动阴影与动效偏好无关，只切阴影不切背景。
    const onScroll = () => root.classList.toggle('is-scrolled', root.scrollTop > 8)
    root.addEventListener('scroll', onScroll, { passive: true })
    onScroll()

    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      return () => root.removeEventListener('scroll', onScroll)
    }
    if (!('IntersectionObserver' in window)) {
      return () => root.removeEventListener('scroll', onScroll)
    }

    const targets = Array.from(root.querySelectorAll<HTMLElement>('.rv, .rv-item'))
    if (targets.length === 0) return

    // Keep the first viewport visible before enabling the hidden/reveal state.
    const viewport = root.getBoundingClientRect()
    for (const target of targets) {
      const rect = target.getBoundingClientRect()
      if (rect.bottom > viewport.top && rect.top < viewport.bottom) target.classList.add('in')
    }

    const observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        entry.target.classList.add('in')
        observer.unobserve(entry.target)
      }
    }, { root, threshold: 0.12 })

    root.classList.add('motion-ready')
    for (const target of targets) {
      if (!target.classList.contains('in')) observer.observe(target)
    }

    return () => {
      observer.disconnect()
      root.removeEventListener('scroll', onScroll)
    }
  }, [])

  return <div className="intro-root" ref={rootRef}>{children}</div>
}
