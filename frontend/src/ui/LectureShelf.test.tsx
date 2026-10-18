import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import { LectureShelf } from "./LectureShelf"
import { lectures, sameVideo } from "./lectures"

describe("lecture shelf", () => {
  it("lists four short lectures and marks the one already in the field", () => {
    const html = renderToStaticMarkup(<LectureShelf link="https://youtu.be/YCzL96nL7j0" onPick={() => undefined} />)
    for (const item of lectures) expect(html).toContain(item.title)
    expect(html).toContain("从这些讲课视频试起")
    expect(html).toContain("已填入")
    expect(html.match(/试这篇/g)).toHaveLength(3)
  })

  it("treats watch and short YouTube links as the same video", () => {
    expect(sameVideo("https://www.youtube.com/watch?v=8C_SCDbUJTg&t=12s", "https://youtu.be/8C_SCDbUJTg")).toBe(true)
    expect(sameVideo("https://www.youtube.com/watch?v=eMlx5fFNoYc", "https://youtu.be/YCzL96nL7j0")).toBe(false)
    expect(sameVideo("", lectures[0].url)).toBe(false)
  })
})