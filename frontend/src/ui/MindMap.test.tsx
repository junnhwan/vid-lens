import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import { MindMap } from "./MindMap"

describe("concept mind map", () => {
  it("retains unlinked branch concepts and links only the open descendants", () => {
    const html = renderToStaticMarkup(
      <MindMap
        current="isolation"
        onJump={() => undefined}
        map={{
          root_id: "root",
          nodes: [
            { id: "root", parent_id: null, label: "为什么 Agent 需要沙箱", section_id: null },
            { id: "boundary", parent_id: "root", label: "选择隔离边界", section_id: null },
            { id: "vm", parent_id: "boundary", label: "虚拟机隔离内核", section_id: "isolation" },
            { id: "snapshot", parent_id: "vm", label: "快照降低重建成本", section_id: "snapshot" },
          ],
        }}
      />,
    )
    expect(html).toContain("选择隔离边界")
    expect(html).toContain('data-depth="2"')
    expect(html).toContain('data-target="isolation"')
    expect(html).not.toContain("快照降低重建成本")
    expect(html).toContain('aria-label="展开虚拟机隔离内核"')
    expect(html).toContain('aria-label="概念关系"')
    expect(html).toContain("放大")
    expect(html).not.toContain("concept-canvas")
    expect(html).toContain("<path")
    expect(html).not.toContain("concept-chips")
  })
})
