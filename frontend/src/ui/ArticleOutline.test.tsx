import { renderToStaticMarkup } from "react-dom/server"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ArticleOutline, outlineWindow } from "./ArticleOutline"

describe("bounded chapter navigation", () => {
  it("follows the current chapter through a long article", () => {
    expect(outlineWindow(40, 28, 12)).toEqual({ start: 24, end: 36 })
    expect(outlineWindow(40, 39, 12)).toEqual({ start: 36, end: 40 })
    expect(outlineWindow(40, -1, 12)).toEqual({ start: 0, end: 12 })
  })

  it("keeps manually browsed groups and viewport changes in bounds", () => {
    expect(outlineWindow(40, 28, 12, 0)).toEqual({ start: 0, end: 12 })
    expect(outlineWindow(40, 28, 8, 100)).toEqual({ start: 32, end: 40 })
    expect(outlineWindow(3, 0, 12)).toEqual({ start: 0, end: 3 })
    expect(outlineWindow(0, -1, 12)).toEqual({ start: 0, end: 0 })
  })

  it("renders chapter titles together, with bounded groups and the current chapter link", () => {
    const sections = Array.from({ length: 40 }, (_, index) => ({ id: `chapter-${index}`, title: `章节 ${index + 1}` }))
    const html = renderToStaticMarkup(<ArticleOutline sections={sections} current="chapter-28" onJump={() => undefined} />)
    expect(html.match(/class="outline-link"/g)).toHaveLength(12)
    expect(html).toContain('href="#chapter-28" aria-current="location"')
    expect(html).toContain("显示较早的章节")
    expect(html).toContain("显示后续章节")
    expect(html).toContain("25–36")
    expect(html).toContain("章节 25")
    expect(html).toContain("章节 36")
    expect(html).not.toContain("arc-nav")
  })

  it("keeps short outlines complete and empty outlines absent", () => {
    const sections = [{ id: "one", title: "一" }, { id: "two", title: "二" }]
    const html = renderToStaticMarkup(<ArticleOutline sections={sections} current="one" onJump={() => undefined} />)
    expect(html.match(/class="outline-link"/g)).toHaveLength(2)
    expect(html).not.toContain("outline-pages")
    expect(renderToStaticMarkup(<ArticleOutline sections={[]} current="article-top" onJump={() => undefined} />)).toBe("")
    expect(html).toContain("查看完整目录")
  })

  it("keeps the complete long title accessible and labels chapters containing images", () => {
    const title = "从 prompt engineer 到 harness engineer：harness 的两层结构"
    const html = renderToStaticMarkup(<ArticleOutline sections={[{ id: "layers", title }]} current="layers" imageCounts={{ layers: 2 }} onJump={() => undefined} />)
    expect(html).toContain(`<span class="outline-title">${title}</span>`)
    expect(html).toContain("2 图")
  })
})

describe("outline interaction", () => {
  let renderer: ReactTestRenderer
  let reduced = false
  let height = 460
  const cancel = vi.fn()
  const animate = vi.fn(() => ({ cancel, onfinish: null }))
  const panel = { inert: false, style: { overflow: "" }, getBoundingClientRect: () => ({ height }), animate }
  const sections = [{ id: "short", title: "简短标题" }, { id: "long", title: "这是需要完整保留并且可能被两行目录截断的章节标题" }]
  beforeEach(() => {
    reduced = false; height = 460; panel.inert = false; panel.style.overflow = ""; animate.mockClear(); cancel.mockClear()
    vi.stubGlobal("window", { innerHeight: 1080, addEventListener: vi.fn(), removeEventListener: vi.fn(), matchMedia: () => ({ matches: reduced }) })
    vi.stubGlobal("getComputedStyle", () => ({ opacity: "0.6" }))
    act(() => { renderer = create(<ArticleOutline sections={sections} current="short" onJump={vi.fn()} />, { createNodeMock: (element) => element.props.className === "outline-desktop" ? panel : element.props.className === "outline-reveal" ? { getBoundingClientRect: () => ({ height: 460 }) } : null }) })
  })
  afterEach(() => { act(() => renderer.unmount()); vi.unstubAllGlobals() })

  it("only offers the full-title hint when the rendered title is actually clipped", () => {
    const links = renderer.root.findAllByProps({ className: "outline-link" })
    const target = (scrollHeight: number) => ({ querySelector: () => ({ scrollHeight, clientHeight: 44, scrollWidth: 200, clientWidth: 200 }) })
    act(() => links[0].props.onMouseEnter({ currentTarget: target(44) }))
    expect(links[0].props["data-truncated"]).toBeUndefined()
    act(() => links[1].props.onFocus({ currentTarget: target(66) }))
    expect(links[1].props["data-truncated"]).toBe(true)
    act(() => links[1].props.onBlur())
    expect(links[1].props["data-truncated"]).toBeUndefined()
  })

  it("keeps collapsed links inert and reverses from the visible height on a rapid toggle", () => {
    const toggle = () => renderer.root.findAllByType("button").find((button) => button.props["aria-controls"])!
    act(() => toggle().props.onClick({ detail: 1 }))
    expect(panel.inert).toBe(true)
    expect(renderer.root.findByProps({ className: "outline-desktop" }).props["aria-hidden"]).toBe(true)
    expect(animate).toHaveBeenLastCalledWith([{ height: "460px", opacity: "0.6" }, { height: "0px", opacity: 0 }], expect.objectContaining({ duration: 200 }))
    height = 185
    act(() => toggle().props.onClick({ detail: 1 }))
    expect(cancel).toHaveBeenCalled()
    expect(panel.inert).toBe(false)
    expect(animate).toHaveBeenLastCalledWith([{ height: "185px", opacity: "0.6" }, { height: "460px", opacity: 1 }], expect.objectContaining({ duration: 200 }))
  })

  it.each(["keyboard", "reduced motion"])("changes state immediately for %s", (mode) => {
    reduced = mode === "reduced motion"
    const toggle = renderer.root.findAllByType("button").find((button) => button.props["aria-controls"])!
    act(() => toggle.props.onClick({ detail: mode === "keyboard" ? 0 : 1 }))
    expect(panel.inert).toBe(true)
    expect(animate).not.toHaveBeenCalled()
  })
})
