import { useEffect, useRef, useState } from "react"
import type { Article } from "../contract/types"
import { prefersReduced } from "./motion"

export function ArticleOutline({ sections, current, onJump }: { sections: Article["sections"]; current: string; onJump: (id: string) => void }) {
  const [open, setOpen] = useState(true)
  const navRef = useRef<HTMLElement>(null)
  useEffect(() => {
    const nav = navRef.current
    const link = nav?.querySelector<HTMLElement>("a[aria-current='location']")
    if (!nav || !link) return
    const top = link.offsetTop - nav.clientHeight / 2 + link.clientHeight / 2
    nav.scrollTo({ top: Math.max(0, top), behavior: prefersReduced() ? "auto" : "smooth" })
  }, [current])
  if (!sections.length) return null
  return (
    <aside className="article-outline" aria-label="文章目录">
      <div className="outline-heading">
        <span>文章目录</span>
        <button type="button" aria-expanded={open} aria-controls="article-outline-items" onClick={() => setOpen((value) => !value)}>
          {open ? "收起" : "展开"}
        </button>
      </div>
      {open ? (
        <nav id="article-outline-items" aria-label="章节导航" ref={navRef}>
          <a
            className="outline-top"
            href="#article-top"
            onClick={(event) => {
              event.preventDefault()
              onJump("article-top")
            }}
          >
            <span className="outline-top-label">回到开头</span>
            <span aria-hidden="true">↑</span>
          </a>
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
        </nav>
      ) : null}
    </aside>
  )
}
