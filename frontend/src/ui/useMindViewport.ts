import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type MouseEvent as ReactMouseEvent } from "react"
import type { MindBox } from "./mindLayout"
import { mindScroll, mindWorld, mindZoom, type Point } from "./mindViewport"

const useClientLayout = typeof window === "undefined" ? useEffect : useLayoutEffect
type Pending = { id: string; reset: boolean; client?: Point } | { point: Point; client: Point }

export function useMindViewport(content: { width: number; height: number; boxes: Map<string, MindBox> }, layoutKey: string, active: boolean) {
  const viewport = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ width: 0, height: 0 })
  const [scale, setScale] = useState(1)
  const [scroll, setScroll] = useState({ x: 0, y: 0 })
  const [request, setRequest] = useState(0)
  const [dragging, setDragging] = useState(false)
  const pending = useRef<Pending | null>(null)
  const drag = useRef<{ id: number; x: number; y: number; left: number; top: number; moved: boolean } | null>(null)
  const suppressClick = useRef(false)
  const scrollFrame = useRef(0)
  const suppressTimer = useRef(0)
  const latest = useRef({ content, size, scale })
  latest.current = { content, size, scale }
  const world = mindWorld(content, size, scale)

  function syncScroll() {
    if (scrollFrame.current) return
    scrollFrame.current = requestAnimationFrame(() => {
      scrollFrame.current = 0
      const node = viewport.current
      if (node) setScroll({ x: node.scrollLeft, y: node.scrollTop })
    })
  }
  function focus(id: string, reset = false, hold = false) {
    const node = viewport.current
    const state = latest.current
    const box = state.content.boxes.get(id)
    const old = mindWorld(state.content, state.size, state.scale)
    pending.current = { id, reset, ...(hold && node && box ? { client: { x: old.x + (box.x + box.w / 2) * state.scale - node.scrollLeft, y: old.y + (box.y + box.h / 2) * state.scale - node.scrollTop } } : {}) }
    if (reset) setScale(1)
    setRequest((value) => value + 1)
  }
  function zoom(value: number, client?: Point) {
    const node = viewport.current
    const state = latest.current
    const next = mindZoom(value)
    if (!node || Math.abs(next - state.scale) < .001) return
    const at = client ?? { x: state.size.width / 2, y: state.size.height / 2 }
    const old = mindWorld(state.content, state.size, state.scale)
    pending.current = { point: { x: (node.scrollLeft + at.x - old.x) / state.scale, y: (node.scrollTop + at.y - old.y) / state.scale }, client: at }
    setScale(next)
  }
  function panTo(point: Point) {
    const node = viewport.current
    const state = latest.current
    if (!node) return
    const position = mindScroll(point, { x: state.size.width / 2, y: state.size.height / 2 }, state.content, state.size, state.scale)
    node.scrollLeft = position.x; node.scrollTop = position.y
    syncScroll()
  }

  useEffect(() => {
    const node = viewport.current
    if (!node || !active) return
    function resize() { if (node && node.clientWidth > 0 && node.clientHeight > 0) setSize((old) => old.width === node.clientWidth && old.height === node.clientHeight ? old : { width: node.clientWidth, height: node.clientHeight }) }
    const observer = new ResizeObserver(resize)
    observer.observe(node); resize()
    const wheel = (event: WheelEvent) => {
      event.preventDefault()
      if (event.shiftKey) {
        node.scrollLeft += event.deltaY + event.deltaX; syncScroll(); return
      }
      const rect = node.getBoundingClientRect()
      const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? node.clientHeight : 1
      zoom(latest.current.scale * Math.exp(-event.deltaY * unit * .0015), { x: event.clientX - rect.left, y: event.clientY - rect.top })
    }
    node.addEventListener("wheel", wheel, { passive: false })
    return () => { observer.disconnect(); node.removeEventListener("wheel", wheel) }
  }, [active])

  useClientLayout(() => {
    const node = viewport.current
    const intent = pending.current
    if (!active || !node || !size.width || !size.height || !intent) return
    const box = "id" in intent ? content.boxes.get(intent.id) : null
    if ("id" in intent && !box) return
    const point = box ? { x: box.x + box.w / 2, y: box.y + box.h / 2 } : (intent as { point: Point }).point
    const client = intent.client ?? { x: size.width / 2, y: size.height / 2 }
    const position = mindScroll(point, client, content, size, scale)
    node.scrollLeft = position.x; node.scrollTop = position.y
    pending.current = null
    syncScroll()
  }, [active, request, layoutKey, size.width, size.height, scale])

  useEffect(() => () => { if (scrollFrame.current) cancelAnimationFrame(scrollFrame.current); if (suppressTimer.current) window.clearTimeout(suppressTimer.current) }, [])

  useEffect(() => { if (!active) { drag.current = null; setDragging(false); suppressClick.current = false } }, [active])

  function down(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return
    suppressClick.current = false
    drag.current = { id: event.pointerId, x: event.clientX, y: event.clientY, left: event.currentTarget.scrollLeft, top: event.currentTarget.scrollTop, moved: false }
  }
  function move(event: ReactPointerEvent<HTMLDivElement>) {
    const start = drag.current
    if (!start || start.id !== event.pointerId) return
    if (!start.moved && Math.hypot(event.clientX - start.x, event.clientY - start.y) < 5) return
    if (!start.moved) { start.moved = true; suppressClick.current = true; event.currentTarget.setPointerCapture(event.pointerId); setDragging(true) }
    event.preventDefault()
    event.currentTarget.scrollLeft = start.left - event.clientX + start.x
    event.currentTarget.scrollTop = start.top - event.clientY + start.y
    syncScroll()
  }
  function up(event: ReactPointerEvent<HTMLDivElement>) {
    if (drag.current?.id !== event.pointerId) return
    drag.current = null; setDragging(false)
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
    window.clearTimeout(suppressTimer.current)
    suppressTimer.current = window.setTimeout(() => { suppressClick.current = false }, 0)
  }
  return { viewport, size, scale, scroll, world, dragging, focus, zoom, panTo, syncScroll, pointer: { onPointerDown: down, onPointerMove: move, onPointerUp: up, onPointerCancel: up, onLostPointerCapture: up, onClickCapture: (event: ReactMouseEvent) => { if (suppressClick.current) { event.preventDefault(); event.stopPropagation() } } } }
}
