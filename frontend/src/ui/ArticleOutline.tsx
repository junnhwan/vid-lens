import { useState } from "react"
import type { CSSProperties } from "react"

export function arcMarks(count: number, active: number) {
  const n = Math.max(count, 0)
  const gap = n > 16 ? 22 : 28
  const height = Math.max(148, (Math.max(n, 1) - 1) * gap + 36)
  const engaged = active >= 0 && active < n
  const marks = Array.from({ length: n }, (_, index) => {
    const along = n <= 1 ? 0.5 : index / (n - 1)
    const arc = Math.sin(along * Math.PI) * 14
    const dist = engaged ? index - active : 3
    const fall = Math.exp(-(dist * dist) / 1.8)
    const spine = 8 + arc
    return {
      spine,
      x: spine + fall * 16,
      y: 18 + index * gap,
      width: 9 + fall * 22,
    }
  })
  const focus = engaged ? active : 0
  return { height, marks, engaged, captionY: marks[focus]?.y ?? 18, spine: smoothSpine(marks) }
}

function smoothSpine(marks: { spine: number; y: number }[]) {
  if (!marks.length) return ""
  if (marks.length === 1) return `M ${marks[0].spine} ${marks[0].y - 10} L ${marks[0].spine} ${marks[0].y + 10}`
  let path = `M ${marks[0].spine.toFixed(1)} ${marks[0].y.toFixed(1)}`
  for (let index = 1; index < marks.length; index += 1) {
    const prev = marks[index - 1]
    const next = marks[index]
    const midX = (prev.spine + next.spine) / 2
    const midY = (prev.y + next.y) / 2
    path += ` Q ${prev.spine.toFixed(1)} ${prev.y.toFixed(1)} ${midX.toFixed(1)} ${midY.toFixed(1)}`
  }
  const last = marks[marks.length - 1]
  path += ` T ${last.spine.toFixed(1)} ${last.y.toFixed(1)}`
  return path
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
  const [hover, setHover] = useState<number | null>(null)
  const place = sections.findIndex((section) => section.id === current)
  const arc = arcMarks(sections.length, place)
  if (!sections.length) return null
  const shown = hover ?? (arc.engaged ? place : null)
  const caption = shown === null ? "从标题读起" : sections[shown].title
  const captionY = shown === null ? arc.marks[0].y : arc.marks[shown].y
  const captionIndex = shown === null ? "" : String(shown + 1).padStart(2, "0")
  return (
    <aside className="article-outline" aria-label={label}>
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
        <nav id="article-outline-items" aria-label="章节导航" onMouseLeave={() => setHover(null)}>
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
          <div className="arc-nav" style={{ "--arc-h": `${arc.height}px` } as CSSProperties}>
            <svg className="arc-spine" viewBox={`0 0 64 ${arc.height}`} aria-hidden="true">
              <path d={arc.spine} />
            </svg>
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
                      style={{
                        "--x": `${mark.x + (hover === index && index !== place ? 8 : 0)}px`,
                        "--y": `${mark.y}px`,
                        "--w": `${mark.width + (hover === index && index !== place ? 12 : 0)}px`,
                      } as CSSProperties}
                      onMouseEnter={() => setHover(index)}
                      onFocus={() => setHover(index)}
                      onBlur={() => setHover((value) => (value === index ? null : value))}
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
            <p key={`${shown ?? "top"}:${caption}`} className="arc-caption" style={{ top: captionY }} aria-hidden="true">
              {captionIndex ? <span className="arc-caption-index">{captionIndex}</span> : null}
              <span className="arc-caption-title">{caption}</span>
            </p>
          </div>
        </nav>
      ) : null}
    </aside>
  )
}
