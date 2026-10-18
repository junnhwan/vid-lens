import { describe, expect, it } from "vitest"
import { catalogChoice, exampleArchive, exampleRoute, exampleShelf } from "./exampleReplay"

describe("public example routing", () => {
  it("opens a selected article and ignores a removed static id", () => {
    expect(exampleRoute("?video=9280423ce43c85da9cc539a7576749a7&view=process")).toEqual({ id: "9280423ce43c85da9cc539a7576749a7" })
    expect(exampleRoute("?video=skills&view=qa&question=frame")).toEqual({ id: null })
    expect(exampleRoute("?video=../../api&view=other")).toEqual({ id: null })
    expect(exampleArchive("9280423ce43c85da9cc539a7576749a7")).toBe("/api/v1/examples/9280423ce43c85da9cc539a7576749a7")
    expect(catalogChoice({ id: "9280423ce43c85da9cc539a7576749a7", title: "已发布", description: "第一节", platform: "youtube", duration_ms: null, cover: "" }).meta).toBe("YouTube")
  })

  it("keeps a single article on the page and shelves the rest", () => {
    const items = Array.from({ length: 8 }, (_, index) => ({ id: `a${index}` }))
    expect(exampleShelf(items.slice(0, 1), "a0")).toEqual({ strip: [], rest: [] })
    expect(exampleShelf(items.slice(0, 3), "a1").strip.map((item) => item.id)).toEqual(["a0", "a1", "a2"])
    expect(exampleShelf(items, "a1")).toEqual({ strip: items.slice(0, 6), rest: items.slice(6) })
    const later = exampleShelf(items, "a7")
    expect(later.strip.map((item) => item.id)).toEqual(["a0", "a1", "a2", "a3", "a4", "a7"])
    expect(later.rest.map((item) => item.id)).toEqual(["a5", "a6"])
  })
})
