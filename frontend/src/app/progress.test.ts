import { describe, expect, it } from "vitest"
import { displayStepTitle, parseTranscription, phaseMarks, quietNote, stepSpent, waitClock } from "./progress"

describe("transcription progress", () => {
  it("reads the clock embedded in the activity title", () => {
    expect(parseTranscription("正在转写音频（8:00 / 35:42）")).toEqual({ done: "8:00", total: "35:42", ratio: 480 / (35 * 60 + 42) })
    expect(parseTranscription("正在转写音频（35:43 / 35:43）")?.ratio).toBe(1)
    expect(parseTranscription("正在复用已完整校验视频的转写")).toBeNull()
    expect(displayStepTitle("正在转写音频（1:00 / 35:42）")).toBe("正在转写音频")
    expect(displayStepTitle("正在查看菜单")).toBe("正在查看菜单")
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
    expect(stepSpent({ started_at: "2026-10-15T04:00:00.000Z", finished_at: "2026-10-15T04:00:00.400Z" })).toBeNull()
    expect(stepSpent({ started_at: "2026-10-15T04:00:00.000Z", finished_at: "2026-10-15T04:01:12.000Z" })).toBe("01:12")
    expect(stepSpent({ started_at: null, finished_at: null })).toBeNull()
  })
})
