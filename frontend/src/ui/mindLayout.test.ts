import { describe, expect, it } from "vitest"
import type { MindMap } from "../contract/types"
import { estimateNodeBox, indexMind, layoutMind, mindMove, visibleMind } from "./mindLayout"

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

describe("mind map layout", () => {
  const index = indexMind(map)
  if (!index) throw new Error("missing root")

  it("keeps an unlinked branch and the descendant behind it", () => {
    const folded = visibleMind(index, false, new Set())
    expect(folded.map((node) => node.id)).toEqual(["root", "boundary", "vm"])
    const open = visibleMind(index, false, new Set(["vm"]))
    expect(open.map((node) => node.label)).toContain("快照降低重建成本")
    expect(open.find((node) => node.id === "snapshot")?.depth).toBe(3)
    expect(index.nodes.has("loose")).toBe(false)
  })

  it("draws each child to the right of its parent", () => {
    const visible = visibleMind(index, false, new Set(["vm"]))
    const sizes = new Map(visible.map((node) => [node.id, estimateNodeBox(node.label, node.depth)]))
    const layout = layoutMind(visible, index.rootId, sizes)
    const root = layout.boxes.get("root")
    const boundary = layout.boxes.get("boundary")
    const vm = layout.boxes.get("vm")
    const snapshot = layout.boxes.get("snapshot")
    expect(root && boundary && vm && snapshot).toBeTruthy()
    expect(boundary!.x).toBeGreaterThan(root!.x + root!.w)
    expect(vm!.x).toBeGreaterThan(boundary!.x + boundary!.w)
    expect(snapshot!.x).toBeGreaterThan(vm!.x + vm!.w)
    expect(layout.wires.map((wire) => wire.id)).toEqual(["boundary", "vm", "snapshot"])
    expect(layout.wires.every((wire) => wire.d.includes("C"))).toBe(true)
  })

  it("moves across the tree and opens a folded branch from the keyboard", () => {
    const visible = visibleMind(index, false, new Set())
    expect(mindMove("boundary", "ArrowLeft", index, visible, false, new Set())).toEqual({ focus: "root" })
    expect(mindMove("root", "ArrowRight", index, visible, false, new Set())).toEqual({ focus: "boundary" })
    const opened = mindMove("vm", "ArrowRight", index, visible, false, new Set())
    expect(opened?.focus).toBe("snapshot")
    expect(opened?.expanded?.has("vm")).toBe(true)
    const sketch = visibleMind(index, true, new Set())
    expect(mindMove("boundary", "ArrowRight", index, sketch, true, new Set())?.sketch).toBe(false)
  })
})
