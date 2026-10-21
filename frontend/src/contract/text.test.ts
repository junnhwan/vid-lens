import { describe, expect, it } from "vitest"
import type { Block } from "./types"
import { blockPlainText, choosePassage, formatClock, formatTokens, normalizeQuote, quoteMatchesBlock, tokenUsageLabel } from "./text"

const paragraph: Block = {
  id: "blk",
  type: "paragraph",
  origin: "video",
  content: [
    { type: "text", text: "菜单展开后能看到保存流程和运行。", marks: [] },
    { type: "citation", citation_id: "cite" },
  ],
}

describe("passage text", () => {
  it("collapses unicode whitespace without changing other characters", () => {
    expect(normalizeQuote("  菜单\u00a0展开\n后  ")).toBe("菜单 展开 后")
  })

  it("ignores citation marks when matching one block", () => {
    expect(blockPlainText(paragraph)).toBe("菜单展开后能看到保存流程和运行。")
    expect(quoteMatchesBlock(paragraph, "能看到保存流程")).toBe(true)
    const chosen = choosePassage([{ sectionId: "sec", blockId: "blk", block: paragraph }], "能看到保存流程", 1000)
    expect(chosen).toEqual({ ok: true, passage: { sectionId: "sec", blockId: "blk", quote: "能看到保存流程" } })
  })

  it("keeps a cross-block selection instead of cutting it down", () => {
    const other: Block = { ...paragraph, id: "other", content: [{ type: "text", text: "另一段" }] }
    const chosen = choosePassage(
      [
        { sectionId: "sec", blockId: "blk", block: paragraph },
        { sectionId: "sec", blockId: "other", block: other },
      ],
      "能看到保存流程 另一段",
      1000,
    )
    expect(chosen).toEqual({ ok: false, reason: "cross" })
  })

  it("formats source time from milliseconds", () => {
    expect(formatClock(312000)).toBe("05:12")
    expect(formatClock(2_142_000)).toBe("35:42")
    expect(formatClock(3_935_000)).toBe("1:05:35")
  })

  it("keeps token counts compact without inventing precision", () => {
    expect(formatTokens(0)).toBe("0")
    expect(formatTokens(999)).toBe("999")
    expect(formatTokens(1000)).toBe("1k")
    expect(formatTokens(21649)).toBe("21.6k")
    expect(formatTokens(236500)).toBe("236.5k")
    expect(formatTokens(1_000_000)).toBe("1m")
    expect(tokenUsageLabel({ input: 21649, output: 3102 })).toBe("输入 21,649 tokens，输出 3,102 tokens")
  })
})
