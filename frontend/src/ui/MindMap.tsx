import { useEffect, useRef, useState } from "react"
import type { MindMap as MapModel } from "../contract/types"
import { prefersReduced } from "./motion"

export function MindMap({ map, current, onJump }: { map: MapModel; current: string; onJump: (id: string) => void }) {
  const [open, setOpen] = useState(false)
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const canvasRef = useRef<HTMLDivElement>(null)
  const root = map.nodes.find((node) => node.id === map.root_id) ?? map.nodes.find((node) => !node.parent_id)
  const nodes = map.nodes.slice(0, 96)
  const children = (id: string) => nodes.filter((node) => node.parent_id === id && node.id !== root?.id)
  useEffect(() => {
    if (!open) return
    const canvas = canvasRef.current
    const node = canvas?.querySelector<HTMLElement>(`[data-target="${CSS.escape(current)}"]`)
    if (!canvas || !node) return
    const top = node.offsetTop - canvas.clientHeight / 2 + node.clientHeight / 2
    canvas.scrollTo({ top: Math.max(0, top), behavior: prefersReduced() ? "auto" : "smooth" })
  }, [current, open])
  if (!root) return null
  function branch(node: MapModel["nodes"][number], depth: number, ancestors: Set<string>) {
    if (ancestors.has(node.id) || depth > 6) return null
    const path = new Set(ancestors).add(node.id)
    const below = children(node.id).filter((child) => !path.has(child.id))
    const folded = collapsed.has(node.id)
    return (
      <li key={node.id} className="concept-branch" data-depth={depth}>
        <div className="concept-node" data-current={node.section_id === current || undefined}>
          {below.length ? (
            <button
              type="button"
              className="concept-fold"
              aria-expanded={!folded}
              aria-label={`${folded ? "展开" : "收起"}${node.label}`}
              onClick={() =>
                setCollapsed((previous) => {
                  const next = new Set(previous)
                  if (next.has(node.id)) next.delete(node.id)
                  else next.add(node.id)
                  return next
                })
              }
            >
              <span aria-hidden="true">{folded ? "+" : "−"}</span>
            </button>
          ) : (
            <span className="concept-dot" aria-hidden="true" />
          )}
          {node.section_id ? (
            <button
              type="button"
              data-target={node.section_id}
              aria-current={node.section_id === current ? "location" : undefined}
              onClick={() => onJump(node.section_id!)}
            >
              {node.label}
              <span className="concept-jump" aria-hidden="true">
                ↗
              </span>
            </button>
          ) : (
            <span className="concept-label">{node.label}</span>
          )}
        </div>
        {below.length ? (
          <div className="disclose" data-open={folded ? "false" : "true"}>
            <div className="disclose-inner">
              <ul>{below.map((child) => branch(child, depth + 1, path))}</ul>
            </div>
          </div>
        ) : null}
      </li>
    )
  }
  const first = children(root.id)
  return (
    <section className="map concept-map" aria-labelledby="map-label">
      <div className="map-head">
        <div>
          <h2 id="map-label">思维导图</h2>
          <p>从主题到关系，点选概念回到原文</p>
        </div>
        <button type="button" id="map-toggle" aria-expanded={open} aria-controls="map-panel" onClick={() => setOpen((value) => !value)}>
          {open ? "收起" : "展开"}
        </button>
      </div>
      <div className="concept-strip" hidden={open}>
        <button type="button" className="concept-root" onClick={() => onJump("article-top")}>
          <span>这条视频的主线</span>
          <strong>{root.label}</strong>
        </button>
        <div className="concept-chips">
          {first.map((node) =>
            node.section_id ? (
              <button
                type="button"
                key={node.id}
                aria-current={node.section_id === current ? "location" : undefined}
                onClick={() => onJump(node.section_id!)}
              >
                {node.label}
              </button>
            ) : (
              <span key={node.id}>{node.label}</span>
            ),
          )}
        </div>
      </div>
      <div id="map-panel" className="concept-canvas" ref={canvasRef} hidden={!open}>
        <button type="button" className="concept-root" data-target="article-top" onClick={() => onJump("article-top")}>
          <span>这条视频的主线</span>
          <strong>{root.label}</strong>
        </button>
        <ul className="concept-tree" aria-label="概念关系">
          {first.map((node) => branch(node, 1, new Set([root.id])))}
        </ul>
      </div>
    </section>
  )
}
