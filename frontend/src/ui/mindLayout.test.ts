import { describe, expect, it } from "vitest"
import type { MindMap } from "../contract/types"
import { descendantCount, estimateNodeBox, indexMind, layoutMind, mindMove, overviewMind, visibleMind } from "./mindLayout"

const map: MindMap = {
  root_id: "root",
  nodes: [
    { id: "root", parent_id: null, label: "为什么 Agent 需要沙箱", section_id: null },
    { id: "boundary", parent_id: "root", label: "选择隔离边界", section_id: null },
    { id: "vm", parent_id: "boundary", label: "虚拟机隔离内核", section_id: "isolation" },
    { id: "snapshot", parent_id: "vm", label: "快照降低重建成本", section_id: "snapshot" },
    { id: "loose", parent_id: "missing", label: "脱离主线", section_id: "lost" },
  ],
}
const index = indexMind(map)!

describe("mind map layout", () => {
  it("preserves connected concepts and every chapter link without mutating generated data", () => {
    const before = JSON.stringify(map)
    expect(visibleMind(index, new Set()).map((node) => node.id)).toEqual(["root", "boundary", "vm", "snapshot"])
    expect(index.nodes.has("loose")).toBe(false)
    expect(descendantCount(index, "boundary")).toBe(2)
    expect(index.nodes.get("snapshot")?.sectionId).toBe("snapshot")
    expect(JSON.stringify(map)).toBe(before)
  })
  it("folds any branch and restores its complete subtree", () => {
    expect(visibleMind(index, new Set(["boundary"])).map((node) => node.id)).toEqual(["root", "boundary"])
    expect(visibleMind(index, new Set(["vm"])).map((node) => node.id)).toEqual(["root", "boundary", "vm"])
    expect(visibleMind(index, new Set(), "vm").map((node) => node.id)).toEqual(["vm", "snapshot"])
  })
  it("connects every visible child to the right of its parent", () => {
    const visible = visibleMind(index, new Set())
    const layout = layoutMind(visible, index.rootId, new Map(visible.map((node) => [node.id, estimateNodeBox(node.label, node.depth)])))
    for (const node of visible.slice(1)) {
      const parent = layout.boxes.get(node.parentId!)!, child = layout.boxes.get(node.id)!
      expect(child.x).toBeGreaterThan(parent.x + parent.w)
    }
    expect(layout.wires.map((wire) => wire.id)).toEqual(["boundary", "vm", "snapshot"])
  })
  it("navigates visible tree order and can open and close branches from the keyboard", () => {
    const visible = visibleMind(index, new Set())
    expect(mindMove("vm", "ArrowDown", visible, new Set())).toEqual({ focus: "snapshot" })
    expect(mindMove("vm", "Home", visible, new Set())).toEqual({ focus: "root" })
    expect(mindMove("root", "End", visible, new Set())).toEqual({ focus: "snapshot" })
    const closed = mindMove("boundary", "ArrowLeft", visible, new Set())!
    expect(closed.collapsed?.has("boundary")).toBe(true)
    const folded = visibleMind(index, closed.collapsed!)
    const opened = mindMove("boundary", "ArrowRight", folded, closed.collapsed!)!
    expect(opened.focus).toBe("vm")
    expect(opened.collapsed?.has("boundary")).toBe(false)
    expect(mindMove("vm", "ArrowLeft", visibleMind(index, new Set(["vm"]), "vm"), new Set(["vm"]))).toBeNull()
  })
  it.each([320, 560, 640, 704, 900])("keeps overview boxes inside a %ipx surface without overlaps", (width) => {
    const overview = overviewMind("root", Array.from({ length: 10 }, (_, i) => `topic-${i}`), width)
    const boxes = [...overview.boxes.values()]
    for (const box of boxes) {
      expect(box.x).toBeGreaterThanOrEqual(0)
      expect(box.y).toBeGreaterThanOrEqual(0)
      expect(box.x + box.w).toBeLessThanOrEqual(width)
      expect(box.y + box.h).toBeLessThanOrEqual(overview.height)
    }
    for (let i = 0; i < boxes.length; i++) for (const other of boxes.slice(i + 1)) {
      const box = boxes[i]
      expect(box.x + box.w <= other.x || other.x + other.w <= box.x || box.y + box.h <= other.y || other.y + other.h <= box.y).toBe(true)
    }
    if (width >= 560) expect(overview.height).toBeLessThanOrEqual(336)
  })
  it("keeps all 96 concepts at readable layout dimensions instead of fitting their height", () => {
    const large: MindMap = { root_id: "root", nodes: [map.nodes[0], ...Array.from({ length: 95 }, (_, i) => ({ id: `n${i}`, parent_id: "root", label: "复杂代码库中的上下文组织与研究反馈 ".repeat(5), section_id: `section-${i}` }))] }
    const indexed = indexMind(large)!, nodes = visibleMind(indexed, new Set())
    const layout = layoutMind(nodes, "root", new Map(nodes.map((node) => [node.id, estimateNodeBox(node.label, node.depth)])))
    expect(nodes).toHaveLength(96)
    expect(layout.height).toBeGreaterThan(4000)
    expect([...layout.boxes.values()].every((box) => box.w >= 200 && box.h <= 92)).toBe(true)
  })
  it("handles a root-only map", () => {
    const single = overviewMind("root", [], 704)
    expect(single.boxes.size).toBe(1)
    expect(single.height).toBe(144)
  })
})
