import { describe, expect, it } from "vitest"
import { displayStepTitle, mergeRanges, parseTranscription, phaseMarks, quietNote, restingTitle, stepSpent, waitClock, anchoredElapsed, serverElapsed } from "./progress"

describe("transcription progress", () => {
  it("reads the clock embedded in the activity title", () => {
    expect(parseTranscription("正在转写音频（8:00 / 35:42）")).toEqual({ done: "8:00", total: "35:42", ratio: 480 / (35 * 60 + 42) })
    expect(parseTranscription("正在转写音频（35:43 / 35:43）")?.ratio).toBe(1)
    expect(parseTranscription("正在复用已完整校验视频的转写")).toBeNull()
    expect(displayStepTitle("正在转写音频（1:00 / 35:42）")).toBe("正在转写音频")
    expect(displayStepTitle("正在查看菜单")).toBe("正在查看菜单")
    expect(restingTitle("正在转写音频（12:03 / 19:43）", "done")).toBe("已转写音频")
    expect(restingTitle("正在准备视频和文字来源", "done")).toBe("已准备视频和文字来源")
    expect(restingTitle("正在校验视频时长与素材", "stopped")).toBe("校验视频时长与素材")
    expect(restingTitle("查看实验画面", "done")).toBe("查看实验画面")
  })
})

describe("wait presentation", () => {
  it("marks only the real phase and keeps a quiet line specific to it", () => {
    expect(phaseMarks("investigate", "running")).toEqual(["done", "done", "current", "waiting", "waiting", "waiting"])
    expect(phaseMarks("compose", "stopped")[2]).toBe("done")
    expect(phaseMarks("compose", "stopped")[3]).toBe("stopped")
    expect(phaseMarks(null, "done").every((mark) => mark === "done")).toBe(true)
    expect(quietNote("prepare_source", "running")).toContain("转写")
    expect(quietNote("overview", "retry_wait")).toBe("")
  })

  it("formats elapsed time from the request clock and a finished step", () => {
    expect(waitClock("2026-10-15T04:00:00.000Z", Date.parse("2026-10-15T04:03:05.000Z"))).toBe("03:05")
    expect(stepSpent({ started_at: "2026-10-15T04:00:00.000Z", finished_at: "2026-10-15T04:00:00.400Z" })).toBe("<1秒")
    expect(stepSpent({ started_at: "2026-10-15T04:00:00.000Z", finished_at: "2026-10-15T04:01:12.000Z" })).toBe("01:12")
    expect(stepSpent({ started_at: null, finished_at: null })).toBeNull()
  })
})

it("keeps running time on a server anchor and a monotonic clock", () => {
  const elapsed = serverElapsed("2026-10-16T09:38:20Z", "2026-10-16T09:40:20Z")!
  expect(elapsed).toBe(120_000)
  // The wall clock may be five minutes ahead or behind; it is never an input.
  expect(anchoredElapsed({ elapsedMs: elapsed, monotonicMs: 10_000 }, 12_000)).toBe(122_000)
  expect(anchoredElapsed({ elapsedMs: elapsed, monotonicMs: 10_000 }, 9_000)).toBe(120_000)
  expect(serverElapsed("invalid", "invalid")).toBeNull()
})

it("merges overlapping observed ranges without inventing gaps", () => {
  expect(mergeRanges([
    { start_ms: 30_000, end_ms: 40_000 },
    { start_ms: 10_000, end_ms: 20_000 },
    { start_ms: 18_000, end_ms: 25_000 },
    { start_ms: 40_000, end_ms: 50_000 },
    { start_ms: 70_000, end_ms: 70_000 },
  ])).toEqual([
    { start_ms: 10_000, end_ms: 25_000 },
    { start_ms: 30_000, end_ms: 50_000 },
  ])
})
