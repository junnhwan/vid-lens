import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent } from "react"
import type { MindMap as MapModel } from "../contract/types"
import {
  ancestorPath,
  branchOpen,
  canToggle,
  estimateNodeBox,
  expansionForSection,
  indexMind,
  layoutMind,
  mindMove,
  nodeForSection,
  visibleMind,
  type MindBox,
} from "./mindLayout"
import { prefersReduced } from "./motion"

const useClientLayout = typeof window === "undefined" ? useEffect : useLayoutEffect

type Ghost = MindBox & { id: string; label: string; depth: number; wire: string | null }

export function MindMap({ map, current, onJump }: { map: MapModel; current: string; onJump: (id: string) => void }) {
  const indexed = useMemo(() => indexMind(map), [map])
  const [sketch, setSketch] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [hoverId, setHoverId] = useState<string | null>(null)
  const [focusId, setFocusId] = useState<string | null>(null)
  const [measured, setMeasured] = useState<Map<string, { w: number; h: number }>>(() => new Map())
  const [drawn, setDrawn] = useState<Set<string>>(() => new Set())
  const [ghosts, setGhosts] = useState<Ghost[]>([])
  const [frame, setFrame] = useState(0)
  const canvasRef = useRef<HTMLDivElement>(null)
  const treeRef = useRef<HTMLDivElement>(null)
  const refs = useRef(new Map<string, HTMLButtonElement>())
  const known = useRef(new Set<string>())
  const snapshot = useRef(new Map<string, Ghost>())
  const measuredRef = useRef(measured)
  const expandedRef = useRef(expanded)
  measuredRef.current = measured
  expandedRef.current = expanded

  const visible = indexed ? visibleMind(indexed, sketch, expanded) : []
  const visibleKey = visible.map((node) => node.id).join("\n")
  const sizes = new Map<string, { w: number; h: number }>()
  for (const node of visible) sizes.set(node.id, measured.get(node.id) ?? estimateNodeBox(node.label, node.depth))
  const layout = indexed ? layoutMind(visible, indexed.rootId, sizes) : { boxes: new Map(), wires: [], width: 0, height: 0 }
  const activeId = indexed && hoverId && visible.some((node) => node.id === hoverId) ? hoverId : indexed ? nodeForSection(indexed, current) : null
  const hot = indexed ? ancestorPath(indexed, activeId) : new Set<string>()
  const focused = indexed && focusId && visible.some((node) => node.id === focusId) ? focusId : indexed?.rootId ?? ""

  useEffect(() => {
    if (!indexed || !current) return
    const needed = expansionForSection(indexed, current)
    if (!needed) return
    setSketch(false)
    setExpanded((existing) => {
      let changed = false
      const next = new Set(existing)
      for (const id of needed.ids) {
        if (!next.has(id)) {
          next.add(id)
          changed = true
        }
      }
      return changed ? next : existing
    })
  }, [indexed, current])

  useClientLayout(() => {
    const stage = treeRef.current
    if (!stage) return
    const observer = new ResizeObserver(() => setFrame((value) => value + 1))
    observer.observe(stage)
    return () => observer.disconnect()
  }, [])

  useClientLayout(() => {
    const canvas = canvasRef.current
    if (!canvas || canvas.offsetWidth < 1) return
    const next = new Map<string, { w: number; h: number }>()
    let changed = measuredRef.current.size !== visible.length
    for (const node of visible) {
      const el = refs.current.get(node.id)
      if (!el) continue
      const box = { w: Math.ceil(el.offsetWidth), h: Math.ceil(el.offsetHeight) }
      if (box.w < 1 || box.h < 1) return
      next.set(node.id, box)
      const previous = measuredRef.current.get(node.id)
      if (!previous || previous.w !== box.w || previous.h !== box.h) changed = true
    }
    if (changed && next.size === visible.length) setMeasured(next)
  }, [visibleKey, frame])

  useClientLayout(() => {
    const previous = snapshot.current
    const currentIds = new Set(visible.map((node) => node.id))
    const removed: Ghost[] = []
    if (!prefersReduced()) {
      for (const [id, ghost] of previous) {
        if (!currentIds.has(id)) removed.push(ghost)
      }
    }
    const next = new Map<string, Ghost>()
    for (const node of visible) {
      const box = layout.boxes.get(node.id)
      if (!box) continue
      next.set(node.id, { ...box, id: node.id, label: node.label, depth: node.depth, wire: layout.wires.find((wire) => wire.id === node.id)?.d ?? null })
    }
    snapshot.current = next
    if (!removed.length) return
    setGhosts(removed)
    const timer = window.setTimeout(() => setGhosts([]), 340)
    return () => window.clearTimeout(timer)
  }, [visibleKey, layout.width, layout.height])

  useClientLayout(() => {
    const canvas = canvasRef.current
    if (!canvas || canvas.offsetWidth < 1) return
    if (prefersReduced()) {
      known.current = new Set(visible.map((node) => node.id))
      return
    }
    const arrived = visible.map((node) => node.id).filter((id) => !known.current.has(id))
    known.current = new Set(visible.map((node) => node.id))
    if (!arrived.length) return
    setDrawn(new Set(arrived))
    const timer = window.setTimeout(() => setDrawn(new Set()), 1100)
    return () => window.clearTimeout(timer)
  }, [visibleKey, frame])

  useEffect(() => {
    const tree = treeRef.current
    const button = refs.current.get(focused)
    if (!tree || !button) return
    const active = document.activeElement
    if (!(active instanceof HTMLElement) || !tree.contains(active) || active === button) return
    if (!active.classList.contains("concept-card")) return
    button.focus()
  }, [focused, visibleKey])

  useEffect(() => {
    if (!indexed) return
    const canvas = canvasRef.current
    const id = nodeForSection(indexed, current)
    const node = id ? refs.current.get(id) : null
    if (!canvas || !node) return
    const top = node.offsetTop - canvas.clientHeight / 2 + node.clientHeight / 2
    const left = node.offsetLeft - 28
    const nextTop = Math.max(0, top)
    const nextLeft = Math.max(0, left)
    if (Math.abs(canvas.scrollTop - nextTop) < 8 && Math.abs(canvas.scrollLeft - nextLeft) < 8) return
    canvas.scrollTo({ top: nextTop, left: nextLeft, behavior: prefersReduced() ? "auto" : "smooth" })
  }, [current, visibleKey, indexed])

  function toggle(id: string) {
    const node = indexed?.nodes.get(id)
    if (!node) return
    if (sketch) {
      setSketch(false)
      return
    }
    setExpanded((existing) => {
      const next = new Set(existing)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function onKey(event: KeyboardEvent<HTMLDivElement>) {
    if (!indexed) return
    const move = mindMove(focused, event.key, indexed, visible, sketch, expandedRef.current)
    if (!move) return
    event.preventDefault()
    if (move.sketch !== undefined) setSketch(move.sketch)
    if (move.expanded) setExpanded(move.expanded)
    setFocusId(move.focus)
  }

  if (!indexed) return null
  const anchors: { x: number; y: number; hot: boolean }[] = []
  for (const wire of layout.wires) {
    const found = anchors.find((item) => Math.abs(item.x - wire.x1) < 0.5 && Math.abs(item.y - wire.y1) < 0.5)
    if (!found) anchors.push({ x: wire.x1, y: wire.y1, hot: hot.has(wire.id) })
    else if (hot.has(wire.id)) found.hot = true
  }
  return (
    <section className="map concept-map" aria-labelledby="map-label">
      <div className="map-head">
        <div>
          <h2 id="map-label">思维导图</h2>
          <p>从主题到关系，点选概念回到原文</p>
        </div>
        <button type="button" id="map-toggle" aria-expanded={!sketch} aria-controls="map-panel" onClick={() => setSketch((value) => !value)}>
          {sketch ? "展开" : "收起"}
        </button>
      </div>
      <div id="map-panel" className="concept-canvas" ref={canvasRef}>
        <div
          className="concept-stage"
          role="tree"
          aria-label="概念关系"
          ref={treeRef}
          style={{ width: layout.width, height: layout.height }}
          onKeyDown={onKey}
          onMouseLeave={() => setHoverId(null)}
        >
          <svg className="concept-wires" viewBox={`0 0 ${layout.width} ${layout.height}`} width={layout.width} height={layout.height} aria-hidden="true">
            {layout.wires.map((wire) => (
              <g key={wire.id}>
                <path
                  d={wire.d}
                  pathLength={1}
                  data-hot={hot.has(wire.id) ? "true" : undefined}
                  data-draw={drawn.has(wire.id) ? "in" : undefined}
                  style={{ "--depth": wire.depth } as CSSProperties}
                />
                <circle cx={wire.x2} cy={wire.y2} r={3.1} data-hot={hot.has(wire.id) ? "true" : undefined} />
              </g>
            ))}
            {anchors.map((anchor) => (
              <circle key={`${anchor.x}-${anchor.y}`} cx={anchor.x} cy={anchor.y} r={2.6} data-hot={anchor.hot ? "true" : undefined} />
            ))}
            {ghosts.map((ghost) =>
              ghost.wire ? (
                <path key={`ghost-${ghost.id}`} d={ghost.wire} pathLength={1} data-draw="out" />
              ) : null,
            )}
          </svg>
          {visible.map((node) => {
            const box = layout.boxes.get(node.id)
            if (!box) return null
            const open = branchOpen(node, sketch, expanded)
            return (
              <button
                key={node.id}
                type="button"
                role="treeitem"
                className="concept-card"
                data-depth={node.depth}
                data-toggle={canToggle(node, sketch, expanded) ? "true" : undefined}
                data-target={node.sectionId ?? undefined}
                data-hot={hot.has(node.id) ? "true" : undefined}
                data-draw={drawn.has(node.id) ? "in" : undefined}
                aria-expanded={node.childIds.length ? open : undefined}
                aria-current={node.sectionId && node.sectionId === current ? "location" : undefined}
                tabIndex={focused === node.id ? 0 : -1}
                style={{ left: box.x, top: box.y, "--depth": node.depth } as CSSProperties}
                ref={(el) => {
                  if (el) refs.current.set(node.id, el)
                  else refs.current.delete(node.id)
                }}
                onMouseEnter={() => setHoverId(node.id)}
                onFocus={() => setFocusId(node.id)}
                onClick={() => {
                  setFocusId(node.id)
                  if (node.sectionId) onJump(node.sectionId)
                  else if (!open && node.childIds.length) toggle(node.id)
                }}
              >
                {node.depth === 0 ? (
                  <>
                    <span className="concept-kicker">这条视频的主线</span>
                    <strong>{node.label}</strong>
                  </>
                ) : (
                  node.label
                )}
              </button>
            )
          })}
          {visible.map((node) => {
            if (!canToggle(node, sketch, expanded)) return null
            const box = layout.boxes.get(node.id)
            if (!box) return null
            const open = branchOpen(node, sketch, expanded)
            return (
              <button
                key={`${node.id}-joint`}
                type="button"
                className="concept-joint"
                data-open={open ? "true" : "false"}
                aria-label={`${open ? "收起" : "展开"}${node.label}`}
                tabIndex={-1}
                style={{ left: box.x + box.w - 7, top: box.y + box.h / 2 - 7 }}
                onClick={() => toggle(node.id)}
              />
            )
          })}
          {ghosts.map((ghost) => (
            <span
              key={`ghost-node-${ghost.id}`}
              className="concept-card concept-ghost"
              data-depth={ghost.depth}
              data-draw="out"
              aria-hidden="true"
              style={{ left: ghost.x, top: ghost.y, width: ghost.w, height: ghost.h }}
            >
              {ghost.label}
            </span>
          ))}
        </div>
      </div>
    </section>
  )
}
