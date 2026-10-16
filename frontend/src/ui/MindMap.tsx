import { useLayoutEffect, useRef, useState } from "react"
import type { MindMap as MapModel } from "../contract/types"

interface Leaf {
  id: string
  label: string
  sectionId: string
}

const spineWidth = 64

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
  const leafBox = useRef<HTMLDivElement>(null)
  const { root, leaves } = leavesOf(map)
  const signature = leaves.map((leaf) => `${leaf.id}:${leaf.label}`).join("|")
  const row = 52
  const gap = 8
  const count = Math.max(leaves.length, 1)
  const fallbackHeight = count * row + Math.max(0, count - 1) * gap
  const [geom, setGeom] = useState<{ height: number; ys: number[] } | null>(null)
  const height = geom?.height && geom.ys.length === leaves.length ? geom.height : fallbackHeight
  const ys = geom?.ys.length === leaves.length ? geom.ys : leaves.map((_, index) => row / 2 + index * (row + gap))
  const mid = height / 2

  useLayoutEffect(() => {
    if (!open) return
    const box = leafBox.current
    if (!box) return
    const measure = () => {
      const buttons = [...box.querySelectorAll<HTMLButtonElement>(".atlas-leaf")]
      const top = box.getBoundingClientRect().top
      const next = buttons.map((btn) => {
        const rect = btn.getBoundingClientRect()
        return rect.top - top + rect.height / 2
      })
      const measured = Math.max(box.getBoundingClientRect().height, 1)
      setGeom((prev) => {
        if (
          prev &&
          Math.abs(prev.height - measured) < 0.5 &&
          prev.ys.length === next.length &&
          prev.ys.every((y, index) => Math.abs(y - next[index]) < 0.5)
        ) {
          return prev
        }
        return { height: measured, ys: next }
      })
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(box)
    return () => observer.disconnect()
  }, [open, signature])

  if (!leaves.length && !root) return null

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
              <svg className="atlas-svg" viewBox={`0 0 ${spineWidth} ${height}`} width={spineWidth} height={height}>
                <path className="trunk" d={`M8 ${mid}H18`} />
                {leaves.map((leaf, index) => {
                  const y = ys[index] ?? mid
                  const straight = Math.abs(y - mid) < 1
                  const d = straight ? `M18 ${mid}H60` : `M18 ${mid}C32 ${mid} 40 ${y} 60 ${y}`
                  const on = current === leaf.sectionId
                  const hover = hot === leaf.sectionId
                  return (
                    <g key={leaf.id}>
                      <path className={`branch${on ? " is-current" : ""}${hover ? " is-hot" : ""}`} data-branch={leaf.sectionId} d={d} />
                      <circle className={`branch-dot${on ? " is-current" : ""}${hover ? " is-hot" : ""}`} data-branch={leaf.sectionId} cx="60" cy={y} r="2.4" />
                    </g>
                  )
                })}
              </svg>
            </div>
            <div className="atlas-leaves" ref={leafBox}>
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
