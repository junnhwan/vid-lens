import { useRef } from "react"
import { act, create } from "react-test-renderer"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useMindReveal } from "./useMindReveal"
import { prefersReduced } from "./motion"

vi.mock("./motion", () => ({ EASE: "cubic-bezier(0.23, 1, 0.32, 1)", prefersReduced: vi.fn(() => false) }))
afterEach(() => { vi.unstubAllGlobals(); vi.mocked(prefersReduced).mockReturnValue(false) })

function element(kind: string, level: number) {
  return {
    getAttribute: (name: string) => name === "data-mind-enter" ? kind : name === "data-mind-level" ? String(level) : "-1",
    animate: vi.fn((_frames: Keyframe[], _options: KeyframeAnimationOptions) => ({ cancel: vi.fn(), onfinish: null as (() => void) | null })),
  }
}
function setup() {
  const keyboard = vi.fn(() => false)
  vi.stubGlobal("document", { activeElement: { matches: keyboard } })
  const rootNode = element("node", 0), wire = element("wire", 1), child = element("node", 1)
  let items = [rootNode, wire, child]
  const container = { querySelectorAll: () => items, addEventListener: vi.fn(), removeEventListener: vi.fn() }
  function Harness({ active = true, signature = "root,child", current = "", scale = 1 }) {
    const root = useRef<HTMLDivElement>(null)
    useMindReveal(root, active, signature)
    return <div ref={root} data-current={current} data-scale={scale} />
  }
  return { Harness, container, keyboard, rootNode, wire, child, setItems: (next: typeof items) => { items = next } }
}

describe("mind map growth", () => {
  it("grows outward before introducing the child and bounds the total wait", async () => {
    const { Harness, container, rootNode, wire, child } = setup()
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<Harness />, { createNodeMock: () => container }) })
    const rootDelay = rootNode.animate.mock.calls[0][1].delay!
    const edgeDelay = wire.animate.mock.calls[0][1].delay!
    const childOptions = child.animate.mock.calls[0][1]
    expect(rootDelay).toBeLessThan(edgeDelay)
    expect(edgeDelay).toBeLessThan(childOptions.delay!)
    expect(childOptions.delay! + Number(childOptions.duration)).toBeLessThanOrEqual(300)
    await act(async () => tree.unmount())
  })
  it("does not replay existing concepts for zoom, chapter highlight or reopening", async () => {
    const { Harness, container, rootNode, child } = setup()
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<Harness />, { createNodeMock: () => container }) })
    await act(async () => tree.update(<Harness current="another-chapter" scale={1.5} />))
    await act(async () => tree.update(<Harness active={false} />))
    await act(async () => tree.update(<Harness active />))
    expect(rootNode.animate).toHaveBeenCalledTimes(1)
    expect(child.animate).toHaveBeenCalledTimes(1)
    expect(rootNode.animate.mock.results[0].value.cancel).toHaveBeenCalled()
    await act(async () => tree.unmount())
  })
  it("animates only newly opened descendants, including a deeper nested branch", async () => {
    const { Harness, container, rootNode, wire, child, setItems } = setup()
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<Harness />, { createNodeMock: () => container }) })
    const descendants = Array.from({ length: 5 }, (_, i) => [element("wire", i + 2), element("node", i + 2)]).flat()
    setItems([rootNode, wire, child, ...descendants])
    await act(async () => tree.update(<Harness signature="expanded" />))
    expect(rootNode.animate).toHaveBeenCalledTimes(1)
    for (const item of descendants) {
      expect(item.animate).toHaveBeenCalledTimes(1)
      const options = item.animate.mock.calls[0][1]
      expect(options.delay! + Number(options.duration)).toBeLessThanOrEqual(300)
    }
    await act(async () => tree.unmount())
  })
  it.each(["keyboard", "reduced motion"])("shows everything immediately for %s", async (mode) => {
    const { Harness, container, keyboard, rootNode, child } = setup()
    if (mode === "keyboard") keyboard.mockReturnValue(true)
    else vi.mocked(prefersReduced).mockReturnValue(true)
    let tree!: ReturnType<typeof create>
    await act(async () => { tree = create(<Harness />, { createNodeMock: () => container }) })
    expect(rootNode.animate).not.toHaveBeenCalled()
    expect(child.animate).not.toHaveBeenCalled()
    await act(async () => tree.unmount())
  })
})
