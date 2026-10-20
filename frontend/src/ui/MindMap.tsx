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

function siblingLag(nodes: { id: string; depth: number }[]): Map<string, number> {
  const counts = new Map<number, number>()
  const lags = new Map<string, number>()
  for (const node of nodes) {
    const lag = counts.get(node.depth) ?? 0
    lags.set(node.id, Math.min(lag, 6))
    counts.set(node.depth, lag + 1)
  }
  return lags
}

export function MindMap({ map, current, onJump }: { map: MapModel; current: string; onJump: (id: string) => void }) {
  const indexed = useMemo(() => indexMind(map), [map])
  const [sketch, setSketch] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [hoverId, setHoverId] = useState<string | null>(null)
  const [focusId, setFocusId] = useState<string | null>(null)
  const [measured, setMeasured] = useState<Map<string, { w: number; h: number }>>(() => new Map())
  const [drawn, setDrawn] = useState<Set<string>>(() => new Set())
  const [settled, setSettled] = useState<Set<string>>(() => new Set())
  const [ghosts, setGhosts] = useState<Ghost[]>([])
  const [frame, setFrame] = useState(0)
  const [enlarged, setEnlarged] = useState(false)
  const [scale, setScale] = useState(1)
  const canvasRef = useRef<HTMLDivElement>(null)
  const inlineHostRef = useRef<HTMLDivElement>(null)
  const zoomHostRef = useRef<HTMLDivElement>(null)
  const dialogRef = useRef<HTMLDialogElement>(null)
  const pendingJump = useRef<string | null>(null)
  const jumpRef = useRef(onJump)
  jumpRef.current = onJump
  const treeRef = useRef<HTMLDivElement>(null)
  const refs = useRef(new Map<string, HTMLButtonElement>())
  const snapshot = useRef(new Map<string, Ghost>())
  const growTries = useRef(0)
  const growKey = useRef("")
  const measuredRef = useRef(measured)
  const expandedRef = useRef(expanded)
  measuredRef.current = measured
  expandedRef.current = expanded

  const visible = indexed ? visibleMind(indexed, sketch, expanded) : []
  const visibleKey = visible.map((node) => node.id).join("\n")
  const lags = siblingLag(visible)
  function motionOf(id: string): "in" | "wait" | undefined {
    if (drawn.has(id)) return "in"
    if (settled.has(id)) return undefined
    return "wait"
  }
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
    const host = enlarged ? zoomHostRef.current : inlineHostRef.current
    if (!host) return
    const observer = new ResizeObserver(() => setFrame((value) => value + 1))
    observer.observe(host)
    return () => observer.disconnect()
  }, [enlarged])

  useClientLayout(() => {
    const host = enlarged ? zoomHostRef.current : inlineHostRef.current
    if (!host || layout.width < 1 || layout.height < 1) return
    const pad = 16
    const availW = host.clientWidth - pad
    const availH = enlarged ? host.clientHeight - pad : 420
    if (availW < 1 || availH < 1) return
    const next = Math.min(enlarged ? 1.35 : 1, availW / layout.width, availH / layout.height)
    if (!Number.isFinite(next) || next <= 0) return
    setScale((value) => (Math.abs(value - next) < 0.01 ? value : next))
  }, [enlarged, frame, layout.width, layout.height])

  useEffect(() => {
    const dialog = dialogRef.current
    if (!enlarged || !dialog) return
    if (!dialog.open) dialog.showModal()
    setFrame((value) => value + 1)
  }, [enlarged])

  useEffect(() => {
    if (enlarged || !pendingJump.current) return
    const frame = window.requestAnimationFrame(() => {
      const id = pendingJump.current
      if (!id) return
      pendingJump.current = null
      jumpRef.current(id)
    })
    return () => window.cancelAnimationFrame(frame)
  }, [enlarged, scale])

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
    if (growKey.current !== visibleKey) {
      growKey.current = visibleKey
      growTries.current = 0
    }
    const ids = visible.map((node) => node.id)
    const visibleSet = new Set(ids)
    const drop = [...settled, ...drawn].filter((id) => !visibleSet.has(id))
    if (drop.length) {
      const gone = new Set(drop)
      setSettled((existing) => new Set([...existing].filter((id) => !gone.has(id))))
      setDrawn((existing) => new Set([...existing].filter((id) => !gone.has(id))))
    }
    const canvas = canvasRef.current
    if (!canvas || canvas.offsetWidth < 1) {
      const shown = !!canvas && canvas.getClientRects().length > 0
      if (shown && growTries.current < 12) {
        growTries.current += 1
        const retry = window.requestAnimationFrame(() => setFrame((value) => value + 1))
        return () => window.cancelAnimationFrame(retry)
      }
      if (shown) {
        const missing = ids.filter((id) => !settled.has(id) && !drop.includes(id))
        if (missing.length) setSettled((existing) => new Set([...existing, ...missing]))
      }
      return
    }
    growTries.current = 0
    const fresh = ids.filter((id) => !settled.has(id) && !drawn.has(id) && !drop.includes(id))
    if (prefersReduced()) {
      if (!fresh.length) return
      setSettled((existing) => {
        const next = new Set([...existing].filter((id) => !drop.includes(id)))
        for (const id of fresh) next.add(id)
        return next
      })
      return
    }
    if (fresh.length) {
      setDrawn((existing) => {
        const next = new Set([...existing].filter((id) => !drop.includes(id)))
        for (const id of fresh) next.add(id)
        return next
      })
    }
    const animating = ids.filter((id) => fresh.includes(id) || drawn.has(id))
    if (!animating.length) return
    const lags = siblingLag(visible)
    let finish = 0
    for (const node of visible) {
      if (!animating.includes(node.id)) continue
      finish = Math.max(finish, node.depth * 130 + (lags.get(node.id) ?? 0) * 42 + 900)
    }
    const timer = window.setTimeout(() => {
      const done = new Set(animating)
      setDrawn((existing) => new Set([...existing].filter((id) => !done.has(id))))
      setSettled((existing) => new Set([...existing, ...animating]))
    }, finish)
    return () => window.clearTimeout(timer)
  }, [visibleKey, frame, drawn, settled])

  useEffect(() => {
    const tree = treeRef.current
    const button = refs.current.get(focused)
    if (!tree || !button) return
    const active = document.activeElement
    if (!(active instanceof HTMLElement) || !tree.contains(active) || active === button) return
    if (!active.classList.contains("concept-card")) return
    button.focus()
  }, [focused, visibleKey])

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
  const fittedHeight = Math.ceil(layout.height * scale + 16)
  const fittedWidth = Math.ceil(layout.width * scale + 16)
  function openZoom(event: { target: EventTarget | null }) {
    if (enlarged) return
    const target = event.target
    if (!(target instanceof Element) || target.closest("button")) return
    setEnlarged(true)
  }
  const anchors: { x: number; y: number; hot: boolean; parentId: string; depth: number }[] = []
  for (const wire of layout.wires) {
    const parentId = indexed.nodes.get(wire.id)?.parentId
    if (!parentId) continue
    const found = anchors.find((item) => Math.abs(item.x - wire.x1) < 0.5 && Math.abs(item.y - wire.y1) < 0.5)
    if (!found) anchors.push({ x: wire.x1, y: wire.y1, hot: hot.has(wire.id), parentId, depth: indexed.nodes.get(parentId)?.depth ?? 0 })
    else if (hot.has(wire.id)) found.hot = true
  }
  const stage = (
        <div
          className="concept-stage"
          role="tree"
          aria-label="概念关系"
          ref={treeRef}
          style={{ width: layout.width, height: layout.height, transform: `scale(${scale})` }}
          onKeyDown={onKey}
          onMouseLeave={() => setHoverId(null)}
          onClick={openZoom}
        >
          <svg className="concept-wires" viewBox={`0 0 ${layout.width} ${layout.height}`} width={layout.width} height={layout.height} aria-hidden="true">
            {layout.wires.map((wire) => (
              <g key={wire.id} data-draw={motionOf(wire.id)} style={{ "--depth": wire.depth, "--lag": lags.get(wire.id) ?? 0 } as CSSProperties}>
                <path d={wire.d} pathLength={1} data-hot={hot.has(wire.id) ? "true" : undefined} />
                <circle cx={wire.x2} cy={wire.y2} r={3.1} data-hot={hot.has(wire.id) ? "true" : undefined} />
              </g>
            ))}
            {anchors.map((anchor) => (
              <circle
                key={`${anchor.x}-${anchor.y}`}
                cx={anchor.x}
                cy={anchor.y}
                r={2.6}
                data-hot={anchor.hot ? "true" : undefined}
                data-draw={motionOf(anchor.parentId)}
                style={{ "--depth": anchor.depth, "--lag": lags.get(anchor.parentId) ?? 0 } as CSSProperties}
              />
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
                data-draw={motionOf(node.id)}
                aria-expanded={node.childIds.length ? open : undefined}
                aria-current={node.sectionId && node.sectionId === current ? "location" : undefined}
                tabIndex={focused === node.id ? 0 : -1}
                style={{ left: box.x, top: box.y, "--depth": node.depth, "--lag": lags.get(node.id) ?? 0 } as CSSProperties}
                ref={(el) => {
                  if (el) refs.current.set(node.id, el)
                  else refs.current.delete(node.id)
                }}
                onMouseEnter={() => setHoverId(node.id)}
                onFocus={() => setFocusId(node.id)}
                onClick={() => {
                  setFocusId(node.id)
                  if (node.sectionId) {
                    if (enlarged) {
                      pendingJump.current = node.sectionId
                      setEnlarged(false)
                    } else onJump(node.sectionId)
                  } else if (!open && node.childIds.length) toggle(node.id)
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
                data-draw={motionOf(node.id)}
                aria-label={`${open ? "收起" : "展开"}${node.label}`}
                tabIndex={-1}
                style={{ left: box.x + box.w - 7, top: box.y + box.h / 2 - 7, "--depth": node.depth, "--lag": lags.get(node.id) ?? 0 } as CSSProperties}
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
  )
  return (
    <section className="map concept-map" aria-labelledby="map-label">
      <div className="map-head">
        <div>
          <h2 id="map-label">思维导图</h2>
          <p>整张图都在这里。点空白处或「放大」看大图，点概念回到原文</p>
        </div>
        <div className="concept-actions">
          <button type="button" onClick={() => setEnlarged(true)}>放大</button>
          <button type="button" id="map-toggle" aria-expanded={!sketch} aria-controls="map-panel" onClick={() => setSketch((value) => !value)}>
            {sketch ? "展开" : "收起"}
          </button>
        </div>
      </div>
      <div className="concept-slot" ref={inlineHostRef}>
        {enlarged ? <p className="concept-zoom-hold">导图已在浮层中打开。</p> : (
          <div id="map-panel" className="concept-fit" data-zoomable="true" ref={canvasRef} style={{ height: fittedHeight }} onClick={openZoom}>
            {stage}
          </div>
        )}
      </div>
      {enlarged ? (
        <dialog
          ref={dialogRef}
          className="concept-zoom"
          aria-labelledby="map-zoom-title"
          onCancel={(event) => {
            event.preventDefault()
            setEnlarged(false)
          }}
          onClick={(event) => {
            if (event.target === event.currentTarget) setEnlarged(false)
          }}
        >
          <div className="concept-zoom-head">
            <h2 id="map-zoom-title">思维导图</h2>
            <button type="button" onClick={() => setEnlarged(false)}>关闭</button>
          </div>
          <div className="concept-zoom-body" ref={zoomHostRef}>
            <div id="map-panel" className="concept-fit" ref={canvasRef} style={{ width: fittedWidth, height: fittedHeight }}>
              {stage}
            </div>
          </div>
        </dialog>
      ) : null}
    </section>
  )
}
