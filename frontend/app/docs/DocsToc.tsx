import { useCallback, useEffect, useState } from 'react'
import { useLocation } from 'react-router'

interface TocItem {
  id: string
  text: string
  level: 2 | 3
}

// 中文标题可直接作为 id;去掉标点与空格,退化时用序号兜底。
function slugify(text: string, index: number): string {
  const s = text
    .trim()
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^\w\u4e00-\u9fa5-]/g, '')
  return s || `sec-${index}`
}

function scan(): TocItem[] {
  const heads = Array.from(
    document.querySelectorAll<HTMLElement>('.docs-article h2, .docs-article h3'),
  )
  return heads.map((h, i) => {
    if (!h.id) h.id = slugify(h.textContent || '', i)
    return {
      id: h.id,
      text: (h.textContent || '').trim(),
      level: h.tagName === 'H3' ? 3 : 2,
    }
  })
}

/**
 * 右侧「本页目录」:从正文 DOM 扫描 h2/h3,页面不必维护两份目录。
 * 路由切换后重扫静态文档标题，并在正文渲染后恢复章节锚点。
 */
export function DocsToc() {
  const { pathname, hash } = useLocation()
  const [items, setItems] = useState<TocItem[]>([])
  const [active, setActive] = useState('')

  useEffect(() => {
    const main = document.querySelector<HTMLElement>('.docs-main')
    if (!main) return

    const list = scan()
    setItems(list)

    let raf = 0
    const measure = () => {
      raf = 0
      if (list.length === 0) return
      const top = main.getBoundingClientRect().top
      let current = list[0].id
      for (const item of list) {
        const el = document.getElementById(item.id)
        if (!el) continue
        if (el.getBoundingClientRect().top - top <= 96) current = item.id
        else break
      }
      setActive(current)
    }
    const onScroll = () => { if (!raf) raf = requestAnimationFrame(measure) }

    // The initial HTML has no headings for the browser to locate. Wait for
    // React's article and TOC layout before restoring a cross-page fragment.
    const restoreFrame = requestAnimationFrame(() => {
      let id = ''
      try { id = decodeURIComponent(hash.slice(1)) } catch { /* Invalid URL fragment: open the page at the top. */ }
      const target = list.some(item => item.id === id) ? document.getElementById(id) : null
      if (target) target.scrollIntoView({ behavior: 'instant', block: 'start' })
      else main.scrollTo({ top: 0, behavior: 'instant' })
      measure()
    })

    main.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      cancelAnimationFrame(restoreFrame)
      main.removeEventListener('scroll', onScroll)
      if (raf) cancelAnimationFrame(raf)
    }
  }, [pathname, hash])

  const jump = useCallback((e: React.MouseEvent<HTMLAnchorElement>, id: string) => {
    e.preventDefault()
    const el = document.getElementById(id)
    if (!el) return
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    el.scrollIntoView({ behavior: reduceMotion ? 'auto' : 'smooth', block: 'start' })
    setActive(id)
  }, [])

  if (items.length < 2) return null

  return (
    <nav className="docs-toc" aria-label="本页目录">
      {/* 宽屏:details 常开、summary 呈静态标题;≤1180 移到正文顶部,可折叠 */}
      <details className="docs-toc-d" open>
        <summary className="docs-toc-title">本页内容</summary>
        <div className="docs-toc-list">
          {items.map(item => (
            <a
              key={item.id}
              href={`#${item.id}`}
              className={`${item.level === 3 ? 'lv3' : ''}${active === item.id ? ' active' : ''}`}
              onClick={e => jump(e, item.id)}
            >
              {item.text}
            </a>
          ))}
        </div>
      </details>
    </nav>
  )
}
