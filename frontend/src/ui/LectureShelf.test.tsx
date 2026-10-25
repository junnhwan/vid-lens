import { renderToStaticMarkup } from "react-dom/server"
import { create } from "react-test-renderer"
import { describe, expect, it } from "vitest"
import { LectureShelf } from "./LectureShelf"
import { lectures, sameVideo } from "./lectures"

describe("lecture shelf", () => {
  it("replaces the old recommendations with the four prepared videos and marks the selected one", () => {
    const html = renderToStaticMarkup(<LectureShelf link="https://youtu.be/MN9dGgmLyso" onPick={() => undefined} />)
    for (const item of lectures) expect(html).toContain(item.title)
    expect(lectures.map((item) => new URL(item.url).searchParams.get("v"))).toEqual(["MN9dGgmLyso", "oPZLPUtmROo", "UwfjzyLnvMg", "p7TdPUcPoik"])
    for (const removed of ["3Blue1Brown", "ArjanCodes", "半小时以内", "eMlx5fFNoYc", "uxwjXLjJOoM"]) expect(html).not.toContain(removed)
    expect(html).toContain("从这四条视频试起")
    expect(html.match(/aria-pressed="true"/g)).toHaveLength(1)
    expect(html).toContain("已填入表单")
    expect(html.match(/选这条视频/g)).toHaveLength(lectures.length - 1)
  })

  it("passes the selected video's link and purpose without starting generation", () => {
    const chosen: (typeof lectures)[number][] = []
    const tree = create(<LectureShelf link="" onPick={(item) => chosen.push(item)} />)
    const buttons = tree.root.findAllByType("button")
    expect(buttons).toHaveLength(4)
    buttons.forEach((button) => expect(button.props.type).toBe("button"))
    buttons[2].props.onClick()
    expect(chosen).toEqual([lectures[2]])
    expect(chosen[0].url).toContain("UwfjzyLnvMg")
    expect(chosen[0].purpose).toContain("智能体")
    tree.unmount()
  })

  it("treats watch and short YouTube links as the same video", () => {
    expect(sameVideo("https://www.youtube.com/watch?v=MN9dGgmLyso&t=12s", "https://youtu.be/MN9dGgmLyso")).toBe(true)
    expect(sameVideo("https://www.youtube.com/watch?v=oPZLPUtmROo", "https://youtu.be/MN9dGgmLyso")).toBe(false)
    expect(sameVideo("", lectures[0].url)).toBe(false)
  })
})
