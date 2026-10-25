import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import { ArticleOutline, arcMarks, outlineWindow } from "./ArticleOutline"

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

  it("renders a bounded rail with controls and the current chapter link", () => {
    const sections = Array.from({ length: 40 }, (_, index) => ({ id: `chapter-${index}`, title: `章节 ${index + 1}` }))
    const html = renderToStaticMarkup(<ArticleOutline sections={sections} current="chapter-28" onJump={() => undefined} />)
    expect(html.match(/class="arc-link"/g)).toHaveLength(12)
    expect(html).toContain('href="#chapter-28" aria-current="location"')
    expect(html).toContain("显示较早的章节")
    expect(html).toContain("显示后续章节")
    expect(html).toContain("25–36")
  })

  it("keeps short outlines complete and empty outlines absent", () => {
    const sections = [{ id: "one", title: "一" }, { id: "two", title: "二" }]
    const html = renderToStaticMarkup(<ArticleOutline sections={sections} current="one" onJump={() => undefined} />)
    expect(html.match(/class="arc-link"/g)).toHaveLength(2)
    expect(html).not.toContain("outline-pages")
    expect(renderToStaticMarkup(<ArticleOutline sections={[]} current="article-top" onJump={() => undefined} />)).toBe("")
    expect(arcMarks(12, 11).height).toBeLessThan(360)
  })
})
