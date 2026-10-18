import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import { LectureShelf } from "./LectureShelf"
import { lectures, sameVideo } from "./lectures"

describe("lecture shelf", () => {
  it("lists the available lectures and marks the one already in the field", () => {
    const html = renderToStaticMarkup(<LectureShelf link="https://youtu.be/eMlx5fFNoYc" onPick={() => undefined} />)
    for (const item of lectures) expect(html).toContain(item.title)
    expect(lectures).toHaveLength(2)
    expect(html).not.toContain("Kubernetes 架构怎么串起来")
    expect(html).not.toContain("LSTM 的三个门")
    expect(html).toContain("从这些讲课视频试起")
    expect(html).toContain("已填入")
    expect(html.match(/试这篇/g)).toHaveLength(lectures.length - 1)
  })

  it("treats watch and short YouTube links as the same video", () => {
    expect(sameVideo("https://www.youtube.com/watch?v=eMlx5fFNoYc&t=12s", "https://youtu.be/eMlx5fFNoYc")).toBe(true)
    expect(sameVideo("https://www.youtube.com/watch?v=uxwjXLjJOoM", "https://youtu.be/eMlx5fFNoYc")).toBe(false)
    expect(sameVideo("", lectures[0].url)).toBe(false)
  })
})
