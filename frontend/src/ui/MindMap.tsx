import { useState } from "react"
import type { MindMap as MapModel } from "../contract/types"
interface Leaf {
  id: string
  label: string
  sectionId: string
}

function leavesOf(map: MapModel): { root: string; leaves: Leaf[] } {
  const root = map.nodes.find((node) => node.id === map.root_id) ?? map.nodes.find((node) => !node.parent_id)
  const linked = map.nodes.filter((node) => node.section_id && node.id !== root?.id)
  return {
    root: root?.label || "这篇",
    leaves: linked.map((node) => ({ id: node.id, label: node.label, sectionId: node.section_id as string })),
  }
}

export function MindMap({ map, current, onJump }: { map: MapModel; current: string; onJump: (id: string) => void }) {
  const [open, setOpen] = useState(true)
  const [hot, setHot] = useState<string | null>(null)
  const { root, leaves } = leavesOf(map)
  if (!leaves.length && !root) return null
  const row = 52
  const gap = 8
  const count = Math.max(leaves.length, 1)
  const height = count * row + Math.max(0, count - 1) * gap
  const mid = height / 2
  const yOf = (index: number) => row / 2 + index * (row + gap)

  return (
    <section className="map" aria-labelledby="map-label">
      <div className="map-head">
        <h2 id="map-label">内容导图</h2>
        <button
          type="button"
          id="map-toggle"
          aria-expanded={open}
          aria-controls="map-panel"
          onClick={() => setOpen((value) => !value)}
        >
          {open ? "收起" : "展开"}
        </button>
      </div>
      <div id="map-panel" className="disclose" data-open={open ? "true" : "false"}>
        <div className="disclose-inner">
          <div className="atlas" data-current={current}>
            <button type="button" className="atlas-root node" data-target="article-top" aria-current={current === "article-top" ? "location" : undefined} onClick={() => onJump("article-top")}>
              <strong>{root}</strong>
              <em>这篇的主线</em>
            </button>
            <div className="atlas-spine" aria-hidden="true" style={{ height }}>
              <span className="atlas-seed" style={{ top: mid - 4 }} />
              <svg className="atlas-svg" viewBox={`0 0 72 ${height}`} width="72" height={height}>
                <path className="trunk" d={`M8 ${mid}H20`} />
                {leaves.map((leaf, index) => {
                  const y = yOf(index)
                  const straight = Math.abs(y - mid) < 1
                  const d = straight ? `M20 ${mid}H70` : `M20 ${mid}C34 ${mid} 42 ${y} 70 ${y}`
                  const on = current === leaf.sectionId
                  const hover = hot === leaf.sectionId
                  return (
                    <g key={leaf.id}>
                      <path className={`branch${on ? " is-current" : ""}${hover ? " is-hot" : ""}`} data-branch={leaf.sectionId} d={d} />
                      <circle className={`branch-dot${on ? " is-current" : ""}${hover ? " is-hot" : ""}`} data-branch={leaf.sectionId} cx="70" cy={y} r="2.4" />
                    </g>
                  )
                })}
              </svg>
            </div>
            <div className="atlas-leaves" style={{ height }}>
              {leaves.map((leaf, index) => (
                <button
                  type="button"
                  className="atlas-leaf node"
                  key={leaf.id}
                  data-target={leaf.sectionId}
                  aria-current={current === leaf.sectionId ? "location" : undefined}
                  onMouseEnter={() => setHot(leaf.sectionId)}
                  onMouseLeave={() => setHot(null)}
                  onClick={() => onJump(leaf.sectionId)}
                >
                  <span className="atlas-index" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
                  <span className="atlas-leaf-text"><strong>{leaf.label}</strong></span>
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
