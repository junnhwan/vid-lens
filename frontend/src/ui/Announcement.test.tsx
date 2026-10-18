import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import { memoryStore } from "../api/writes"
import { AnnouncementDialog } from "./Announcement"
import { announcementMemory } from "./announcementMemory"

describe("announcement reading", () => {
  it("remembers a dismissed publication across page loads and shows a new version", () => {
    const storage = memoryStore()
    const firstPage = announcementMemory(() => storage)
    expect(firstPage.hasRead("publication-1")).toBe(false)
    firstPage.markRead("publication-1")
    const nextPage = announcementMemory(() => storage)
    expect(nextPage.hasRead("publication-1")).toBe(true)
    expect(nextPage.hasRead("publication-2")).toBe(false)
  })

  it("still dismisses in the current page when storage is blocked or full", () => {
    const blocked = announcementMemory(() => { throw new Error("storage blocked") })
    expect(blocked.hasRead("one")).toBe(false)
    expect(() => blocked.markRead("one")).not.toThrow()
    expect(blocked.hasRead("one")).toBe(true)
    expect(blocked.hasRead("two")).toBe(false)

    const full = announcementMemory(() => ({ getItem: () => null, setItem: () => { throw new Error("quota") } }))
    full.markRead("one")
    expect(full.hasRead("one")).toBe(true)
  })

  it("renders owner content as text with an accessible title and close controls", () => {
    const html = renderToStaticMarkup(<AnnouncementDialog announcement={{ version: "one", title: "<script>alert(1)</script>", body: "<img src=x onerror=alert(1)>\n下一行" }} onDismiss={() => {}} />)
    expect(html).not.toContain("<script>")
    expect(html).not.toContain("<img")
    expect(html).toContain("&lt;script&gt;")
    expect(html).toContain("\n下一行")
    expect(html).toContain("aria-labelledby=")
    expect(html).toContain("aria-describedby=")
    expect(html).toContain("关闭公告")
    expect(html).toContain("知道了，继续")
  })

  it("marks an unpublished preview and explains that it does not dismiss the visitor notice", () => {
    const html = renderToStaticMarkup(<AnnouncementDialog preview announcement={{ version: "", title: "预览", body: "正文" }} onDismiss={() => {}} />)
    expect(html).toContain("公告预览")
    expect(html).toContain("预览不会发布，也不会影响访客的已读状态。")
    expect(html).toContain("返回编辑")
  })
})
