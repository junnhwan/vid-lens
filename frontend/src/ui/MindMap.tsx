import { useEffect, useId, useMemo, useRef, useState } from "react"
import type { MindMap as MapModel } from "../contract/types"
import { ancestorPath, descendantCount, indexMind, nodeForSection, overviewMind, type MindIndex } from "./mindLayout"
import { MindReader, type MindTarget } from "./MindReader"
import { ReadingDialog } from "./ReadingDialog"
import { EASE, prefersReduced } from "./motion"
import { useMindReveal } from "./useMindReveal"
import "../styles/mind-map.css"

export function MindMap({ map, current, onJump }: { map: MapModel; current: string; onJump: (id: string) => void }) {
  const index = useMemo(() => indexMind(map), [map])
  return index ? <MindContents key={index.rootId} index={index} current={current} onJump={onJump} /> : null
}

function MindContents({ index, current, onJump }: { index: MindIndex; current: string; onJump: (id: string) => void }) {
  const [open, setOpen] = useState(true)
  const [readerOpen, setReaderOpen] = useState(false)
  const [visited, setVisited] = useState(false)
  const [target, setTarget] = useState<MindTarget>({ scope: index.rootId, node: index.rootId, revision: 0 })
  const [page, setPage] = useState(0)
  const [width, setWidth] = useState(704)
  const panelId = useId()
  const titleId = useId()
  const panel = useRef<HTMLDivElement>(null)
  const reveal = useRef<HTMLDivElement>(null)
  const diagram = useRef<HTMLDivElement>(null)
  const animation = useRef<Animation | null>(null)
  const desiredOpen = useRef(true)
  const pending = useRef<string | null>(null)
  const root = index.nodes.get(index.rootId)!
  const capacity = 10
  const pages = Math.max(1, Math.ceil(root.childIds.length / capacity))
  const activePage = Math.min(page, pages - 1)
  const topicIds = root.childIds.slice(activePage * capacity, (activePage + 1) * capacity)
  const overview = overviewMind(index.rootId, topicIds, width)
  const hot = ancestorPath(index, nodeForSection(index, current))
  useMindReveal(diagram, open, topicIds.join("\n"))

  useEffect(() => {
    const node = diagram.current
    if (!node) return
    const resize = () => { if (node.clientWidth > 0) setWidth(node.clientWidth) }
    const observer = new ResizeObserver(resize)
    observer.observe(node); resize()
    return () => observer.disconnect()
  }, [])
  useEffect(() => () => animation.current?.cancel(), [])

  function toggle(keyboard: boolean) {
    const node = panel.current, inner = reveal.current
    const next = !desiredOpen.current
    const from = node ? { height: `${node.getBoundingClientRect().height}px`, opacity: getComputedStyle(node).opacity } : null
    animation.current?.cancel(); desiredOpen.current = next; setOpen(next)
    if (!node || !inner) return
    node.inert = !next
    node.style.overflow = ""
    if (keyboard || prefersReduced() || !from) return
    node.style.overflow = "hidden"
    const motion = node.animate([from, { height: `${next ? inner.getBoundingClientRect().height : 0}px`, opacity: next ? 1 : 0 }], { duration: 200, easing: EASE, fill: "both" })
    animation.current = motion
    motion.onfinish = () => { if (animation.current === motion) { node.style.overflow = ""; motion.cancel(); animation.current = null } }
  }
  function read(scope?: string) {
    if (scope) setTarget((value) => ({ scope, node: scope, revision: value.revision + 1 }))
    setVisited(true); setReaderOpen(true)
  }
  const rootBox = overview.boxes.get(index.rootId)!
  return <section className="mind-map" aria-labelledby={titleId} data-open={open}>
    <header className="mind-map-head">
      <div><h2 id={titleId}>思维导图</h2><p>{root.childIds.length ? `${root.childIds.length} 个主题 · ` : ""}{index.nodes.size} 个概念</p></div>
      <div className="mind-map-actions"><button type="button" className="mind-open-reader" aria-haspopup="dialog" onClick={() => read()}>查看完整导图 <span aria-hidden="true">↗</span></button><button type="button" className="mind-collapse" aria-expanded={open} aria-controls={panelId} onClick={(event) => toggle(event.detail === 0)}>{open ? "收起" : "展开"}</button></div>
    </header>
    <div id={panelId} ref={panel} className="mind-map-panel" aria-hidden={!open}><div className="mind-map-reveal" ref={reveal}>
      <div className="mind-overview" ref={diagram} style={{ height: overview.height }} role="group" aria-label="主题概览">
        <svg className="mind-overview-wires" width={overview.width} height={overview.height} aria-hidden="true">{topicIds.map((id) => {
          const box = overview.boxes.get(id)!
          const left = box.x < rootBox.x
          const x1 = left ? rootBox.x : rootBox.x + rootBox.w, y1 = rootBox.y + rootBox.h / 2
          const x2 = left ? box.x + box.w : box.x, y2 = box.y + box.h / 2, middle = (x1 + x2) / 2
          const path = overview.narrow ? `M ${rootBox.x + rootBox.w / 2} ${rootBox.y + rootBox.h} V 96 H ${box.x + box.w / 2} V ${box.y}` : `M ${x1} ${y1} C ${middle} ${y1}, ${middle} ${y2}, ${x2} ${y2}`
          return <path key={id} d={path} pathLength={1} data-mind-enter="wire" data-mind-level={1} data-hot={hot.has(id) || undefined} />
        })}</svg>
        {[index.rootId, ...topicIds].map((id) => {
          const box = overview.boxes.get(id)!, node = index.nodes.get(id)!, isRoot = id === index.rootId
          return <button type="button" className={`mind-theme${isRoot ? " mind-theme-root" : ""}`} key={id} aria-label={`${isRoot ? "查看完整导图" : "阅读分支"}：${node.label}`} data-mind-enter="node" data-mind-level={isRoot ? 0 : 1} data-mind-from={isRoot ? 0 : box.x < rootBox.x ? 1 : -1} data-hot={hot.has(id) || undefined} style={{ left: box.x, top: box.y, width: box.w, height: box.h }} onClick={() => read(id)}>
            <span className="mind-theme-label">{node.label}</span>{!isRoot ? <span className="mind-theme-count">{descendantCount(index, id)} 项</span> : null}
          </button>
        })}
      </div>
      {pages > 1 ? <div className="mind-overview-pages"><button type="button" disabled={activePage === 0} onClick={() => setPage(activePage - 1)}>← 上一组</button><span>{activePage + 1} / {pages}</span><button type="button" disabled={activePage === pages - 1} onClick={() => setPage(activePage + 1)}>下一组 →</button></div> : null}
      <p className="mind-overview-note">点主题阅读分支，选中概念可定位原文。</p>
    </div></div>
    <ReadingDialog open={readerOpen} title="思维导图" className="mind-dialog" onClose={() => setReaderOpen(false)} onAfterClose={() => { const id = pending.current; pending.current = null; if (id) onJump(id) }}>
      {visited ? <MindReader index={index} current={current} target={target} active={readerOpen} onLocate={(section) => { pending.current = section; setReaderOpen(false) }} /> : null}
    </ReadingDialog>
  </section>
}
