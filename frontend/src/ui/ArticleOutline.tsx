import { useEffect, useId, useRef, useState } from "react"
import { ReadingDialog } from "./ReadingDialog"
import { EASE, prefersReduced } from "./motion"

export function outlineWindow(count: number, active: number, size: number, requested?: number) {
  const capacity = Math.max(1, size)
  const lastStart = Math.max(0, Math.ceil(count / capacity) - 1) * capacity
  const start = Math.min(lastStart, Math.max(0, requested ?? Math.floor(Math.max(0, active) / capacity) * capacity))
  return { start, end: Math.min(count, start + capacity) }
}

export function ArticleOutline({ sections, current, onJump, label = "文章目录", imageCounts = {} }: {
  sections: { id: string; title: string; [key: string]: unknown }[]
  current: string
  onJump: (id: string) => void
  label?: string
  imageCounts?: Record<string, number>
}) {
  const [open, setOpen] = useState(true)
  const [compactOpen, setCompactOpen] = useState(false)
  const [visited, setVisited] = useState(false)
  const [pageSize, setPageSize] = useState(12)
  const [browsed, setBrowsed] = useState<{ current: string; start: number } | null>(null)
  const [hint, setHint] = useState<string | null>(null)
  const [pointerMotion, setPointerMotion] = useState(true)
  const pending = useRef<string | null>(null)
  const panel = useRef<HTMLDivElement>(null)
  const reveal = useRef<HTMLDivElement>(null)
  const animation = useRef<Animation | null>(null)
  const desiredOpen = useRef(true)
  const navId = useId()
  useEffect(() => {
    const resize = () => { setPageSize(Math.max(3, Math.min(12, Math.floor((window.innerHeight - 270) / 64)))); setHint(null) }
    resize()
    window.addEventListener("resize", resize)
    return () => window.removeEventListener("resize", resize)
  }, [])
  useEffect(() => () => animation.current?.cancel(), [])
  function toggle(keyboard: boolean) {
    const node = panel.current
    const inner = reveal.current
    const next = !desiredOpen.current
    const instant = keyboard || prefersReduced()
    const from = node ? { height: `${node.getBoundingClientRect().height}px`, opacity: getComputedStyle(node).opacity } : null
    animation.current?.cancel()
    desiredOpen.current = next
    setOpen(next)
    setHint(null)
    setPointerMotion(!instant)
    if (!node || !inner) return
    node.inert = !next
    node.style.overflow = ""
    if (instant || !from) return
    node.style.overflow = "hidden"
    const motion = node.animate([from, { height: `${next ? inner.getBoundingClientRect().height : 0}px`, opacity: next ? 1 : 0 }], { duration: 200, easing: EASE, fill: "both" })
    animation.current = motion
    motion.onfinish = () => {
      if (animation.current !== motion) return
      node.style.overflow = ""
      motion.cancel()
      animation.current = null
    }
  }
  function inspectTitle(link: HTMLAnchorElement, id: string) {
    const title = link.querySelector<HTMLElement>(".outline-title")
    setHint(title && (title.scrollHeight > title.clientHeight + 1 || title.scrollWidth > title.clientWidth + 1) ? id : null)
  }
  const place = sections.findIndex((section) => section.id === current)
  const { start, end } = outlineWindow(sections.length, place, pageSize, browsed?.current === current ? browsed.start : undefined)
  if (!sections.length) return null
  const count = place >= 0 ? `${String(place + 1).padStart(2, "0")}/${String(sections.length).padStart(2, "0")}` : `${sections.length} 章`
  function contents(compact = false) {
    function jump(id: string) {
      if (compact) { pending.current = id; setCompactOpen(false) }
      else onJump(id)
    }
    return <nav aria-label="章节导航">
      <a className="outline-top" href="#article-top" aria-current={current === "article-top" ? "location" : undefined} onClick={(event) => { event.preventDefault(); jump("article-top") }}>↑ 回到开头</a>
      <ol className="outline-list" start={start + 1}>
        {sections.slice(start, end).map((section, offset) => <li key={section.id}>
          <a className="outline-link" href={`#${section.id}`} aria-current={current === section.id ? "location" : undefined} data-truncated={hint === section.id || undefined}
            onMouseEnter={(event) => inspectTitle(event.currentTarget, section.id)} onMouseLeave={() => setHint(null)}
            onFocus={(event) => inspectTitle(event.currentTarget, section.id)} onBlur={() => setHint(null)}
            onClick={(event) => { event.preventDefault(); jump(section.id) }}>
            <span className="outline-index" aria-hidden="true">{String(start + offset + 1).padStart(2, "0")}</span>
            <span className="outline-title">{section.title}</span>
            {imageCounts[section.id] ? <span className="outline-images">{imageCounts[section.id]} 图</span> : null}
            <span className="outline-tooltip" aria-hidden="true">{section.title}</span>
          </a>
        </li>)}
      </ol>
      {sections.length > pageSize ? <div className="outline-pages">
        <button type="button" aria-label="显示较早的章节" disabled={start === 0} onClick={() => setBrowsed({ current, start: Math.max(0, start - pageSize) })}>← 上一组</button>
        <span aria-live="polite">{String(start + 1).padStart(2, "0")}–{String(end).padStart(2, "0")}</span>
        <button type="button" aria-label="显示后续章节" disabled={end === sections.length} onClick={() => setBrowsed({ current, start: start + pageSize })}>下一组 →</button>
      </div> : null}
    </nav>
  }
  return <aside className="article-outline" aria-label={label} data-open={open} data-motion={pointerMotion}>
    <div className="outline-heading">
      <p>{label} <span className="outline-count">{count}</span></p>
      <button type="button" aria-expanded={open} aria-controls={navId} onClick={(event) => toggle(event.detail === 0)}>{open ? "收起" : "展开"}
        <svg className="outline-toggle-icon" viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path d="m4 6 4 4 4-4" /></svg>
      </button>
    </div>
    <button type="button" className="outline-compact" aria-haspopup="dialog" onClick={() => { setVisited(true); setCompactOpen(true) }}>
      <span>{label} <span className="outline-count">{count}</span></span><span>查看完整目录 ↗</span>
    </button>
    <div className="outline-desktop" id={navId} ref={panel} aria-hidden={!open}><div className="outline-reveal" ref={reveal}>{contents()}</div></div>
    <ReadingDialog open={compactOpen} title={label} onClose={() => setCompactOpen(false)} className="outline-dialog"
      onAfterClose={() => { const id = pending.current; pending.current = null; if (id) onJump(id) }}>
      {visited ? contents(true) : null}
    </ReadingDialog>
  </aside>
}
