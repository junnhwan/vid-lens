import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type { Article } from "../contract/types"
import { prefersReduced } from "./motion"

const useClientLayout = typeof window === "undefined" ? useEffect : useLayoutEffect

export function ArticleOutline({ sections, current, onJump }: { sections: Article["sections"]; current: string; onJump: (id: string) => void }) {
  const [open, setOpen] = useState(true)
  const navRef = useRef<HTMLElement>(null)
  const [mark, setMark] = useState<{ top: number; height: number; progress: number } | null>(null)
  const place = sections.findIndex((section) => section.id === current)
  const sectionKey = sections.map((section) => `${section.id}:${section.title}`).join("\n")
  useClientLayout(() => {
    const nav = navRef.current
    if (!nav || !open) return
    const measure = () => {
      const list = nav.querySelector<HTMLElement>(".outline-list")
      const link = nav.querySelector<HTMLElement>("ol a[aria-current='location']")
      if (!list || !link) {
        setMark((value) => (value === null ? value : null))
        return
      }
      const top = Math.round(link.getBoundingClientRect().top - list.getBoundingClientRect().top)
      const height = Math.round(link.getBoundingClientRect().height)
      const next = { top: top + 6, height: Math.max(18, height - 12), progress: Math.max(18, top + height / 2 - 8) }
      setMark((value) => (value && value.top === next.top && value.height === next.height && value.progress === next.progress ? value : next))
    }
    measure()
    const link = nav.querySelector<HTMLElement>("a[aria-current='location']")
    if (link) {
      const delta = link.getBoundingClientRect().top - nav.getBoundingClientRect().top - nav.clientHeight / 2 + link.clientHeight / 2
      if (Math.abs(delta) > 12) nav.scrollTo({ top: Math.max(0, nav.scrollTop + delta), behavior: prefersReduced() ? "auto" : "smooth" })
    }
    const observer = new ResizeObserver(measure)
    observer.observe(nav)
    const list = nav.querySelector(".outline-list")
    if (list) observer.observe(list)
    return () => observer.disconnect()
  }, [current, open, sectionKey])
  if (!sections.length) return null
  return (
    <aside className="article-outline" aria-label="文章目录">
      <div className="outline-heading">
        <p className="outline-kicker">
          <span>文章目录</span>
          <span className="outline-count" aria-hidden="true">
            {place >= 0 ? (
              <>
                <b>{String(place + 1).padStart(2, "0")}</b>
                <span>/{String(sections.length).padStart(2, "0")}</span>
              </>
            ) : (
              String(sections.length).padStart(2, "0")
            )}
          </span>
        </p>
        <button type="button" aria-expanded={open} aria-controls="article-outline-items" onClick={() => setOpen((value) => !value)}>
          {open ? "收起" : "展开"}
        </button>
      </div>
      {open ? (
        <nav id="article-outline-items" aria-label="章节导航" ref={navRef}>
          <a
            className="outline-top"
            href="#article-top"
            aria-current={current === "article-top" ? "location" : undefined}
            onClick={(event) => {
              event.preventDefault()
              onJump("article-top")
            }}
          >
            <span className="outline-return" aria-hidden="true">↑</span>
            <span className="outline-top-label">回到开头</span>
          </a>
          <div className="outline-list">
            <span className="outline-progress" style={{ height: mark?.progress ?? 0 }} aria-hidden="true" />
            {mark ? <span className="outline-mark" style={{ top: mark.top, height: mark.height }} aria-hidden="true" /> : null}
            <ol>
              {sections.map((section, index) => (
                <li key={section.id}>
                  <a
                    href={`#${section.id}`}
                    aria-current={current === section.id ? "location" : undefined}
                    title={section.title}
                    onClick={(event) => {
                      event.preventDefault()
                      onJump(section.id)
                    }}
                  >
                    <span className="outline-index" aria-hidden="true">
                      {String(index + 1).padStart(2, "0")}
                    </span>
                    <span className="outline-title">{section.title}</span>
                  </a>
                </li>
              ))}
            </ol>
          </div>
        </nav>
      ) : null}
    </aside>
  )
}
