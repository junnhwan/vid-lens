import { renderToStaticMarkup } from "react-dom/server"
import { act, create } from "react-test-renderer"
import { describe, expect, it, vi } from "vitest"
import { MindMap } from "./MindMap"
import { ReadingDialog } from "./ReadingDialog"
import type { MindMap as MapModel } from "../contract/types"

const map: MapModel = { root_id: "root", nodes: [
  { id: "root", parent_id: null, label: "为什么 Agent 需要沙箱", section_id: null },
  { id: "boundary", parent_id: "root", label: "选择隔离边界", section_id: null },
  { id: "vm", parent_id: "boundary", label: "虚拟机隔离内核", section_id: "isolation" },
  { id: "snapshot", parent_id: "vm", label: "快照降低重建成本", section_id: "snapshot" },
] }

describe("concept mind map", () => {
  it("starts with readable themes and keeps the complete tree for the reader", () => {
    const html = renderToStaticMarkup(<MindMap map={map} current="isolation" onJump={() => {}} />)
    expect(html).toContain("选择隔离边界")
    expect(html).toContain("4 个概念")
    expect(html).toContain("查看完整导图")
    expect(html).not.toContain("快照降低重建成本")
    expect(html).not.toContain("data-draw")
    expect(html).not.toContain("scale(0.")
  })
  it("selects concepts without jumping, then locates the chapter only after the dialog closes", async () => {
    const jump = vi.fn()
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<MindMap map={map} current="isolation" onJump={jump} />) })
    const theme = tree.root.findAllByType("button").find((button) => button.props["aria-label"] === "阅读分支：选择隔离边界")!
    await act(async () => theme.props.onClick())
    const concept = tree.root.findAllByType("button").find((button) => button.props["data-node-id"] === "vm")!
    await act(async () => concept.props.onClick())
    expect(jump).not.toHaveBeenCalled()
    const locate = tree.root.findByProps({ "aria-label": "所选概念" }).findAllByType("button").find((button) => button.props.className === "btn primary")!
    await act(async () => locate.props.onClick())
    expect(jump).not.toHaveBeenCalled()
    expect(tree.root.findByType(ReadingDialog).props.open).toBe(false)
    await act(async () => tree.root.findByType(ReadingDialog).props.onAfterClose())
    expect(jump).toHaveBeenCalledTimes(1)
    expect(jump).toHaveBeenCalledWith("isolation")
    await act(async () => tree.unmount())
  })
  it("keeps a folded branch and selection while the article's current chapter changes", async () => {
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<MindMap map={map} current="isolation" onJump={() => {}} />) })
    await act(async () => tree.root.findAllByType("button").find((button) => button.props.className === "mind-open-reader")!.props.onClick())
    await act(async () => tree.root.findAllByType("button").find((button) => button.props["data-node-id"] === "vm")!.props.onClick())
    await act(async () => tree.root.findByProps({ "aria-label": "所选概念" }).findAllByType("button").find((button) => button.props.className === "reading-note-link")!.props.onClick())
    await act(async () => tree.update(<MindMap map={map} current="snapshot" onJump={() => {}} />))
    expect(tree.root.findAllByType("button").some((button) => button.props["data-node-id"] === "snapshot")).toBe(false)
    expect(tree.root.findAllByType("button").find((button) => button.props["data-node-id"] === "vm")!.props["aria-selected"]).toBe(true)
    await act(async () => tree.unmount())
  })
})
