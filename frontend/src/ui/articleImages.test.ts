import { describe, expect, it } from "vitest"
import type { Article, Frame } from "../contract/types"
import { articleImages } from "./articleImages"

const frame = (id: string, caption = "画面图注"): Frame => ({ id, snapshot_id: "s", time_ms: 12500, requested_time_ms: 12000, width: 1280, height: 720, mime_type: "image/jpeg", caption, crop: null, replaces_frame_id: null, availability: "ready", unavailable_reason: null, expires_at: null, urls: { full: `/${id}.jpg`, thumbnail: `/${id}-thumb.jpg` } })

describe("published article images", () => {
  it("keeps chapter and sequence order, deduplicates references and excludes unadopted or question frames", () => {
    const sections: Article["sections"] = [
      { id: "intro", title: "先读文字", blocks: [{ id: "p", type: "paragraph", origin: "video", frame_ids: ["private"] }] },
      { id: "steps", title: "两步操作", blocks: [{ id: "sequence", type: "figure", origin: "video", frame_ids: ["b", "a", "b"], caption: "菜单的前后变化" }] },
      { id: "recap", title: "再看结论", blocks: [{ id: "repeat", type: "figure", origin: "video", frame_ids: ["a", "c"] }] },
    ]
    const frames = new Map(["private", "candidate", "a", "b", "c"].map((id) => [id, frame(id)]))
    const groups = articleImages(sections, frames)
    expect(groups.map((group) => [group.chapter, group.sectionId, group.images.map((image) => image.frameId)])).toEqual([[2, "steps", ["b", "a"]], [3, "recap", ["c"]]])
    expect(groups[0].images[0]).toMatchObject({ caption: "菜单的前后变化", sectionId: "steps", blockId: "sequence", frame: frames.get("b") })
    expect(groups[1].images[0].caption).toBe("画面图注")
  })

  it("retains a published image's location when its frame metadata is missing or expired", () => {
    const expired = { ...frame("expired"), availability: "expired" }
    const sections: Article["sections"] = [{ id: "s", title: "原章节", blocks: [{ id: "figure", type: "figure", origin: "video", frame_ids: ["missing", "expired"] }] }]
    const images = articleImages(sections, new Map([[expired.id, expired]]))[0].images
    expect(images).toHaveLength(2)
    expect(images[0]).toMatchObject({ frameId: "missing", frame: undefined, blockId: "figure" })
    expect(images[1].frame?.availability).toBe("expired")
  })

  it("omits an image region for a text-only article", () => {
    expect(articleImages([{ id: "s", title: "文字", blocks: [{ id: "p", type: "paragraph", origin: "video" }] }], new Map())).toEqual([])
  })
})
