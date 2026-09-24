'use client'

import { Children, cloneElement, isValidElement, useEffect, useState } from 'react'

type Props = {
  children: React.ReactNode
  delay?: number
  step?: number
  className?: string
}

/**
 * 逐字浮出：把文本节点拆成 .char 并递增 --cd 延迟。
 * 元素节点(如带渐变文字的 <em>)整体作为一个单元动画,不拆字符 ——
 * -webkit-text-fill-color 会继承给子 span,拆开会让渐变标题变成全透明。
 * 仅在 motion-ready(JS 可用且未开启减少动态效果)时注入 span,
 * 否则保持原始节点:SSR 与无 JS 环境下标题照常完整显示。
 */
export function RevealText({ children, delay = 0, step = 22, className }: Props) {
  const [ready, setReady] = useState(false)

  useEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    const id = requestAnimationFrame(() => setReady(true))
    return () => cancelAnimationFrame(id)
  }, [])

  return (
    <span className={className} data-reveal-text="">
      {ready ? <Split text={children} delay={delay} step={step} /> : children}
    </span>
  )
}

function Split({ text, delay, step }: { text: React.ReactNode; delay: number; step: number }) {
  let d = delay
  const walk = (node: React.ReactNode): React.ReactNode =>
    Children.map(node, child => {
      if (typeof child === 'string') {
        return Array.from(child).map((ch, i) => {
          if (ch.trim() === '') return <span key={`s${i}`}> </span>
          const el = (
            <span className="char" key={i} style={{ '--cd': `${d}ms` } as React.CSSProperties}>
              {ch}
            </span>
          )
          d += step
          return el
        })
      }
      if (isValidElement<Record<string, unknown>>(child)) {
        const props = child.props as { style?: React.CSSProperties; className?: string }
        const el = cloneElement(child, {
          className: `${props.className ?? ''} char`.trim(),
          style: { ...(props.style || {}), '--cd': `${d}ms` } as React.CSSProperties,
        })
        d += step * 4
        return el
      }
      return child
    })

  return <>{walk(text)}</>
}
