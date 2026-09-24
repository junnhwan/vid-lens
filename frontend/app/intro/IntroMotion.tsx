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

    // 流程连线：整体进入视口后逐段描边生长
    const flowWrap = root.querySelector<HTMLElement>('.intro-flow-wrap')
    const flowObserver = flowWrap
      ? new IntersectionObserver(
          entries => {
            for (const entry of entries) {
              if (!entry.isIntersecting) continue
              entry.target.classList.add('lit')
              flowObserver?.disconnect()
            }
          },
          { root, threshold: 0.2 },
        )
      : null
    if (flowWrap && flowObserver) flowObserver.observe(flowWrap)

    const cleanups: Array<() => void> = []

    // 指针微交互只在“真悬停”设备启用:触屏滚动时的 pointermove 不应触发倾斜/磁吸
    const canHover = window.matchMedia('(hover: hover) and (pointer: fine)').matches

    // 能力卡：追光坐标 + 微倾斜
    if (canHover) {
      for (const card of Array.from(root.querySelectorAll<HTMLElement>('.intro-card'))) {
      const onMove = (e: PointerEvent) => {
        const rect = card.getBoundingClientRect()
        const px = (e.clientX - rect.left) / rect.width
        const py = (e.clientY - rect.top) / rect.height
        card.style.setProperty('--i-mx', `${px * 100}%`)
        card.style.setProperty('--i-my', `${py * 100}%`)
        card.style.transform = `translateY(-2px) rotateY(${(px - 0.5) * 5}deg) rotateX(${(0.5 - py) * 5}deg)`
      }
      const onLeave = () => {
        card.style.transform = ''
      }
      card.addEventListener('pointermove', onMove)
      card.addEventListener('pointerleave', onLeave)
      cleanups.push(() => {
        card.removeEventListener('pointermove', onMove)
        card.removeEventListener('pointerleave', onLeave)
        card.style.transform = ''
      })
      }
    }

    // 按钮磁吸
    if (canHover) {
      for (const btn of Array.from(root.querySelectorAll<HTMLElement>('.intro-cta, .intro-cta-sm'))) {
      const strength = btn.classList.contains('intro-cta-sm') ? 0.22 : 0.4
      const onMove = (e: PointerEvent) => {
        const rect = btn.getBoundingClientRect()
        const dx = e.clientX - (rect.left + rect.width / 2)
        const dy = e.clientY - (rect.top + rect.height / 2)
        btn.style.transform = `translate(${dx * strength * 0.3}px, ${dy * strength * 0.45}px)`
      }
      const onLeave = () => {
        btn.style.transform = ''
      }
      btn.addEventListener('pointermove', onMove)
      btn.addEventListener('pointerleave', onLeave)
      cleanups.push(() => {
        btn.removeEventListener('pointermove', onMove)
        btn.removeEventListener('pointerleave', onLeave)
        btn.style.transform = ''
      })
      }
    }

    return () => {
      observer.disconnect()
      flowObserver?.disconnect()
      root.removeEventListener('scroll', onScroll)
      for (const fn of cleanups) fn()
    }
  }, [])

  return <div className="intro-root" ref={rootRef}>{children}</div>
}
