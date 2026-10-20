import { useState } from "react"
import type { CSSProperties } from "react"

export function arcMarks(count: number, active: number) {
  const n = Math.max(count, 0)
  const gap = n > 16 ? 20 : 26
  const height = Math.max(132, (Math.max(n, 1) - 1) * gap + 28)
  const engaged = active >= 0 && active < n
  const marks = Array.from({ length: n }, (_, index) => {
    const along = n <= 1 ? 0.5 : index / (n - 1)
    const arc = Math.sin(along * Math.PI) * 9
    const dist = engaged ? index - active : 3
    const fall = Math.exp(-(dist * dist) / 2.2)
    return {
      x: 4 + arc + fall * 13,
      y: 14 + index * gap,
      width: 10 + fall * 20,
    }
  })
  const focus = engaged ? active : 0
  return { height, marks, engaged, captionY: marks[focus]?.y ?? 14 }
}

export function ArticleOutline({
  sections,
  current,
  onJump,
  label = "文章目录",
}: {
  sections: { id: string; title: string; [key: string]: unknown }[]
  current: string
  onJump: (id: string) => void
  label?: string
}) {
  const [open, setOpen] = useState(true)
  const place = sections.findIndex((section) => section.id === current)
  const arc = arcMarks(sections.length, place)
  if (!sections.length) return null
  const caption = arc.engaged ? sections[place].title : "从标题读起"
  return (
    <aside className="article-outline" aria-label={label} style={{ "--arc-h": `${arc.height}px`, "--caption-y": `${arc.captionY}px` } as CSSProperties}>
      <div className="outline-heading">
        <p className="outline-kicker">
          <span>{label}</span>
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
        <nav id="article-outline-items" aria-label="章节导航">
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
          <div className="arc-nav">
            <ol>
              {sections.map((section, index) => {
                const mark = arc.marks[index]
                return (
                  <li key={section.id}>
                    <a
                      className="arc-link"
                      href={`#${section.id}`}
                      aria-current={current === section.id ? "location" : undefined}
                      aria-label={`${String(index + 1).padStart(2, "0")} ${section.title}`}
                      style={{ "--x": `${mark.x}px`, "--y": `${mark.y}px`, "--w": `${mark.width}px` } as CSSProperties}
                      onClick={(event) => {
                        event.preventDefault()
                        onJump(section.id)
                      }}
                    >
                      <span className="arc-mark" />
                    </a>
                  </li>
                )
              })}
            </ol>
            <p className="arc-caption" aria-hidden="true">{caption}</p>
          </div>
        </nav>
      ) : null}
    </aside>
  )
}
