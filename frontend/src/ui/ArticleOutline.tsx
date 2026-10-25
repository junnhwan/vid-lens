import { useEffect, useState } from "react"
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

export function outlineWindow(count: number, active: number, size: number, requested?: number) {
  const capacity = Math.max(1, size)
  const lastStart = Math.max(0, Math.ceil(count / capacity) - 1) * capacity
  const start = Math.min(lastStart, Math.max(0, requested ?? Math.floor(Math.max(0, active) / capacity) * capacity))
  return { start, end: Math.min(count, start + capacity) }
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
  const [pageSize, setPageSize] = useState(12)
  const [browsed, setBrowsed] = useState<{ current: string; start: number } | null>(null)
  useEffect(() => {
    const resize = () => setPageSize(Math.max(4, Math.min(12, Math.floor((window.innerHeight - 320) / 28))))
    resize()
    window.addEventListener("resize", resize)
    return () => window.removeEventListener("resize", resize)
  }, [])
  const place = sections.findIndex((section) => section.id === current)
  const { start, end } = outlineWindow(sections.length, place, pageSize, browsed?.current === current ? browsed.start : undefined)
  const visible = sections.slice(start, end)
  const arc = arcMarks(visible.length, place - start)
  if (!sections.length) return null
  const shown = hover !== null && hover >= start && hover < end ? hover : arc.engaged ? place : start > 0 ? start : null
  const caption = shown === null ? "从标题读起" : sections[shown].title
  const captionY = Math.max(42, Math.min(arc.height - 42, arc.marks[shown === null ? 0 : shown - start].y))
  const captionIndex = shown === null ? "" : String(shown + 1).padStart(2, "0")
  return (
    <aside className="article-outline" aria-label={label} data-open={open}>
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
          <span className="outline-toggle-label">{open ? "收起" : "展开"}</span>
          <svg className="outline-toggle-icon" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path d="m4 6 4 4 4-4" /></svg>
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
              {visible.map((section, offset) => {
                const index = start + offset
                const mark = arc.marks[offset]
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
                      <span className="arc-mark" aria-hidden="true" />
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
          {sections.length > pageSize ? (
            <div className="outline-pages">
              <button type="button" aria-label="显示较早的章节" disabled={start === 0} onClick={() => { setHover(null); setBrowsed({ current, start: Math.max(0, start - pageSize) }) }}><span aria-hidden="true">↑</span><span className="outline-page-label">上一组</span></button>
              <span className="outline-page-range" aria-live="polite">{String(start + 1).padStart(2, "0")}–{String(end).padStart(2, "0")}</span>
              <button type="button" aria-label="显示后续章节" disabled={end === sections.length} onClick={() => { setHover(null); setBrowsed({ current, start: start + pageSize }) }}><span className="outline-page-label">下一组</span><span aria-hidden="true">↓</span></button>
            </div>
          ) : null}
        </nav>
      ) : null}
    </aside>
  )
}
