import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react"
import { ancestorPath, estimateNodeBox, layoutMind, mindMove, visibleMind, type MindIndex } from "./mindLayout"
import { MAX_MIND_ZOOM, MIN_MIND_ZOOM, minimapPoint, MIND_PADDING } from "./mindViewport"
import { useMindViewport } from "./useMindViewport"
import { useMindReveal } from "./useMindReveal"

const useClientLayout = typeof window === "undefined" ? useEffect : useLayoutEffect
export type MindTarget = { scope: string; node: string; revision: number }

export function MindReader({ index, current, target, active, onLocate }: {
  index: MindIndex
  current: string
  target: MindTarget
  active: boolean
  onLocate: (section: string) => void
}) {
  const [scope, setScope] = useState(target.scope)
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())
  const [selected, setSelected] = useState(target.node)
  const [focused, setFocused] = useState(target.node)
  const [hovered, setHovered] = useState<string | null>(null)
  const refs = useRef(new Map<string, HTMLButtonElement>())
  const keyboardFocus = useRef(false)
  const miniDrag = useRef<number | null>(null)
  const visible = useMemo(() => visibleMind(index, collapsed, scope), [index, collapsed, scope])
  const layoutKey = visible.map((node) => node.id).join("\n")
  const layout = useMemo(() => {
    const depth = index.nodes.get(scope)?.depth ?? 0
    return layoutMind(visible, scope, new Map(visible.map((node) => [node.id, estimateNodeBox(node.label, node.depth - depth)])))
  }, [visible, scope, index])
  const view = useMindViewport(layout, layoutKey, active)
  const selectedNode = index.nodes.get(selected) ?? index.nodes.get(scope)!
  const hot = ancestorPath(index, hovered ?? selectedNode.id)
  const topics = index.nodes.get(index.rootId)!.childIds.map((id) => index.nodes.get(id)!)
  const rootDepth = index.nodes.get(scope)!.depth
  useMindReveal(view.viewport, active, layoutKey)

  useEffect(() => {
    setScope(target.scope); setSelected(target.node); setFocused(target.node)
    setCollapsed((existing) => { const next = new Set(existing); next.delete(target.scope); return next })
    view.focus(target.node, true)
  }, [target.revision])

  useClientLayout(() => {
    if (!keyboardFocus.current) return
    refs.current.get(focused)?.focus({ preventScroll: true })
    keyboardFocus.current = false
  }, [focused, layoutKey])

  function changeScope(id: string) {
    setScope(id); setSelected(id); setFocused(id); setHovered(null)
    setCollapsed((existing) => { const next = new Set(existing); next.delete(id); return next })
    view.focus(id, true)
  }
  function toggle(id: string) {
    view.focus(id, false, true)
    setCollapsed((existing) => { const next = new Set(existing); if (next.has(id)) next.delete(id); else next.add(id); return next })
    setSelected(id); setFocused(id)
  }
  function onKey(event: KeyboardEvent) {
    const move = mindMove(focused, event.key, visible, collapsed)
    if (!move) return
    event.preventDefault()
    if (move.collapsed) setCollapsed(move.collapsed)
    keyboardFocus.current = true
    setFocused(move.focus); setSelected(move.focus)
    view.focus(move.focus)
  }
  function navigateMini(event: PointerEvent<HTMLButtonElement>) {
    const rect = event.currentTarget.getBoundingClientRect()
    const at = minimapPoint({ x: event.clientX - rect.left, y: event.clientY - rect.top }, { width: rect.width, height: rect.height }, { width: layout.width + MIND_PADDING * 2, height: layout.height + MIND_PADDING * 2 })
    view.panTo({ x: at.x - MIND_PADDING, y: at.y - MIND_PADDING })
  }
  const mapW = layout.width + MIND_PADDING * 2
  const mapH = layout.height + MIND_PADDING * 2
  const miniX = Math.max(0, Math.min(mapW - 1, (view.scroll.x - view.world.x) / view.scale + MIND_PADDING))
  const miniY = Math.max(0, Math.min(mapH - 1, (view.scroll.y - view.world.y) / view.scale + MIND_PADDING))
  const ancestors = [...ancestorPath(index, selectedNode.id)].reverse().filter((id) => id !== index.rootId && id !== selectedNode.id)

  return <div className="mind-reader">
    <div className="mind-reader-tools">
      <label className="mind-scope-label">阅读范围<select aria-label="选择导图分支" value={scope} onChange={(event) => changeScope(event.target.value)}>
        <option value={index.rootId}>完整导图</option>{topics.map((node) => <option key={node.id} value={node.id}>{node.label}</option>)}
      </select></label>
      <div className="mind-zoom-tools" aria-label="导图缩放">
        <button type="button" className="mind-tool" aria-label="缩小导图" disabled={view.scale <= MIN_MIND_ZOOM} onClick={() => view.zoom(view.scale / 1.2)}>−</button>
        <span className="mind-zoom-value" aria-label={`缩放比例 ${Math.round(view.scale * 100)}%`}>{Math.round(view.scale * 100)}%</span>
        <button type="button" className="mind-tool" aria-label="放大导图" disabled={view.scale >= MAX_MIND_ZOOM} onClick={() => view.zoom(view.scale * 1.2)}>＋</button>
        <button type="button" className="mind-tool mind-reset" onClick={() => view.focus(selectedNode.id, true)}>复位</button>
      </div>
      <span className="mind-gesture-note">拖动平移 · 滚轮缩放</span>
    </div>
    <div className="mind-reader-body">
      <div className="mind-canvas-shell">
        <div className="mind-viewport" ref={view.viewport} data-dragging={view.dragging} onScroll={view.syncScroll} {...view.pointer} tabIndex={0} role="region" aria-label="导图画布，拖动平移，滚轮缩放">
          <div className="mind-world" style={{ width: view.world.width, height: view.world.height }}>
            <div className="mind-tree" role="tree" aria-label="概念关系" onKeyDown={onKey} onMouseLeave={() => setHovered(null)} style={{ width: layout.width, height: layout.height, left: view.world.x, top: view.world.y, transform: `scale(${view.scale})` }}>
              <svg className="mind-wires" width={layout.width} height={layout.height} aria-hidden="true">{layout.wires.map((wire) => <path key={wire.id} d={wire.d} pathLength={1} data-mind-enter="wire" data-mind-level={wire.depth - rootDepth} data-hot={hot.has(wire.id) || undefined} />)}</svg>
              {visible.map((node) => {
                const box = layout.boxes.get(node.id)!
                const open = !collapsed.has(node.id)
                return <div key={node.id} className="mind-node-place" style={{ width: box.w, height: box.h, transform: `translate(${box.x}px, ${box.y}px)` }}>
                  <div className="mind-node-reveal" data-mind-enter="node" data-mind-level={node.depth - rootDepth} data-mind-from={node.id === scope ? 0 : -1}>
                  <button type="button" role="treeitem" className="mind-node" data-node-id={node.id} data-parent-id={node.parentId ?? undefined} data-depth={node.depth} data-target={node.sectionId ?? undefined} data-root={node.id === scope || undefined} data-hot={hot.has(node.id) || undefined} data-branch={node.childIds.length > 0 || undefined}
                    aria-label={node.label} aria-level={node.depth - rootDepth + 1} aria-selected={selectedNode.id === node.id} aria-expanded={node.childIds.length ? open : undefined} aria-current={node.sectionId && node.sectionId === current ? "location" : undefined}
                    tabIndex={focused === node.id ? 0 : -1} ref={(element) => { if (element) refs.current.set(node.id, element); else refs.current.delete(node.id) }}
                    onMouseEnter={() => setHovered(node.id)} onFocus={() => { setFocused(node.id); setSelected(node.id) }} onClick={() => { setFocused(node.id); setSelected(node.id) }}>
                    <span className="mind-node-label">{node.label}</span>
                  </button>
                  {node.childIds.length ? <button type="button" className="mind-joint" tabIndex={-1} aria-label={`${open ? "折叠" : "展开"}${node.label}`} onClick={() => toggle(node.id)}>{open ? "−" : "＋"}</button> : null}
                  </div>
                </div>
              })}
            </div>
          </div>
        </div>
        <button type="button" className="mind-minimap" aria-label="导图缩略导航，方向键平移" onPointerDown={(event) => { miniDrag.current = event.pointerId; event.currentTarget.setPointerCapture(event.pointerId); navigateMini(event) }}
          onPointerMove={(event) => { if (miniDrag.current === event.pointerId) navigateMini(event) }} onPointerUp={(event) => { miniDrag.current = null; if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId) }} onPointerCancel={() => { miniDrag.current = null }}
          onClick={(event) => { if (event.detail === 0) view.focus(selectedNode.id) }}
          onKeyDown={(event) => {
            const node = view.viewport.current
            if (!node) return
            const directions: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }
            const direction = directions[event.key]
            if (direction) { event.preventDefault(); node.scrollLeft += direction[0] * view.size.width / 3; node.scrollTop += direction[1] * view.size.height / 3; view.syncScroll() }
          }}>
          <svg viewBox={`0 0 ${mapW} ${mapH}`} aria-hidden="true">{visible.map((node) => {
            const box = layout.boxes.get(node.id)!
            return <rect key={node.id} className="mind-mini-node" data-selected={node.id === selectedNode.id || undefined} x={box.x + MIND_PADDING} y={box.y + MIND_PADDING} width={box.w} height={box.h} />
          })}<rect className="mind-mini-view" x={miniX} y={miniY} width={Math.max(1, Math.min(mapW - miniX, view.size.width / view.scale))} height={Math.max(1, Math.min(mapH - miniY, view.size.height / view.scale))} /></svg>
        </button>
      </div>
      <aside className="mind-node-detail" aria-label="所选概念">
        <p className="mind-detail-kind">{selectedNode.id === index.rootId ? "视频主线" : selectedNode.depth === 1 ? "当前主题" : "当前概念"}</p>
        {ancestors.length ? <p className="mind-detail-path">{ancestors.map((id) => index.nodes.get(id)!.label).join(" › ")}</p> : null}
        <h3>{selectedNode.label}</h3>
        {selectedNode.sectionId ? <button type="button" className="btn primary" onClick={() => onLocate(selectedNode.sectionId!)}>查看对应章节 <span aria-hidden="true">↗</span></button> : null}
        {selectedNode.childIds.length ? <div className="mind-detail-branch"><p>下级概念 <span>{selectedNode.childIds.length} 项</span></p><button type="button" className="reading-note-link" onClick={() => toggle(selectedNode.id)}>{collapsed.has(selectedNode.id) ? "展开子概念" : "折叠这个分支"}</button></div> : null}
      </aside>
    </div>
  </div>
}
