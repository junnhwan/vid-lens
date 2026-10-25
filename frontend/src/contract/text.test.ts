import { describe, expect, it } from "vitest"
import type { Block } from "./types"
import { blockPlainText, choosePassage, contextQuote, formatClock, formatTokens, normalizeQuote, passageContext, quoteMatchesBlock, tokenUsageLabel } from "./text"

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
    const chosen = choosePassage([{ sectionId: "sec", blockId: "blk", quote: "能看到保存流程" }], [{ id: "sec", blocks: [paragraph] }], 1000)
    expect(chosen).toEqual({ ok: true, passage: { quote: "能看到保存流程", passages: [{ sectionId: "sec", blockId: "blk", quote: "能看到保存流程" }] } })
    if (chosen.ok) expect(passageContext(chosen.passage)).toEqual({ passage: { section_id: "sec", block_id: "blk", quote: "能看到保存流程" } })
  })

  it("retains each selected quote across paragraphs and chapters", () => {
    const other: Block = { ...paragraph, id: "other", content: [{ type: "text", text: "另一段" }] }
    const chosen = choosePassage(
      [
        { sectionId: "sec", blockId: "blk", quote: "能看到保存流程和运行。" },
        { sectionId: "next", blockId: "other", quote: "另一" },
      ],
      [{ id: "sec", blocks: [paragraph] }, { id: "next", blocks: [other] }],
      1000,
    )
    expect(chosen.ok).toBe(true)
    if (!chosen.ok) return
    expect(chosen.passage.passages).toHaveLength(2)
    const context = { frame_id: null, ...passageContext(chosen.passage) }
    expect(context.passage).toBeNull()
    expect(context.passages).toEqual([{ section_id: "sec", block_id: "blk", quote: "能看到保存流程和运行。" }, { section_id: "next", block_id: "other", quote: "另一" }])
    expect(contextQuote(context)).toBe("能看到保存流程和运行。\n\n另一")
  })

  it("enforces the total Unicode length including normalized paragraph separators", () => {
    const first: Block = { ...paragraph, id: "a", content: [{ type: "text", text: "甲😀" }] }
    const second: Block = { ...paragraph, id: "b", content: [{ type: "text", text: "乙😀" }] }
    const hits = [{ sectionId: "sec", blockId: "a", quote: "甲😀" }, { sectionId: "sec", blockId: "b", quote: "乙😀" }]
    const sections = [{ id: "sec", blocks: [first, second] }]
    expect(choosePassage(hits, sections, 5).ok).toBe(true)
    expect(choosePassage(hits, sections, 4)).toEqual({ ok: false, reason: "long" })
  })

  it("rejects changed quotes, wrong anchors, duplicate, reversed and skipped blocks", () => {
    const blocks: Block[] = ["甲段。", "乙段。", "丙段。"].map((text, index) => ({ ...paragraph, id: String(index), content: [{ type: "text", text }] }))
    const sections = [{ id: "sec", blocks }]
    const hits = blocks.map((block) => ({ sectionId: "sec", blockId: block.id, quote: blockPlainText(block) }))
    for (const bad of [
      [{ ...hits[0], quote: "改写的文字" }, hits[1]],
      [hits[0], { ...hits[1], sectionId: "elsewhere" }],
      [hits[0], hits[0]], [hits[1], hits[0]], [hits[0], hits[2]],
      [{ ...hits[0], quote: "甲" }, hits[1]],
      [hits[0], { ...hits[1], quote: "乙" }, hits[2]],
      [hits[0], { ...hits[1], quote: "段。" }],
    ]) expect(choosePassage(bad, sections, 1000)).toEqual({ ok: false, reason: "mismatch" })
    expect(choosePassage(hits, sections, 1000).ok).toBe(true)
  })

  it("normalizes whitespace and keeps legacy history and empty context readable", () => {
    const block: Block = { ...paragraph, content: [{ type: "text", text: "甲\u00a0乙\n丙" }] }
    const chosen = choosePassage([{ sectionId: "sec", blockId: "blk", quote: "甲 乙\n丙" }], [{ id: "sec", blocks: [block] }], 5)
    expect(chosen.ok).toBe(true)
    expect(contextQuote({ frame_id: null, passage: { section_id: "sec", block_id: "blk", quote: "旧的单段" } })).toBe("旧的单段")
    expect(contextQuote({ frame_id: null, passage: null })).toBe("")
    expect(passageContext(null)).toEqual({ passage: null })
    expect(choosePassage([], [], 1000)).toEqual({ ok: false, reason: "empty" })
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
