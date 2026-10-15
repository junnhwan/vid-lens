import { describe, expect, it } from "vitest"
import type { ActivityStep } from "./types"
import { mergeSteps } from "./activity"
import { decideSnapshot, decideUpsert, type Clock } from "./stream"

const clock: Clock = { revision: 2, runId: "run_gen", seq: 18, cursor: "c18" }

function step(partial: Partial<ActivityStep> & Pick<ActivityStep, "id" | "last_seq">): ActivityStep {
  return {
    run_id: "run_gen",
    segment_no: 1,
    first_seq: partial.last_seq,
    phase: "investigate",
    title: partial.id,
    purpose: null,
    status: "running",
    source_range: null,
    frame_ids: [],
    frames: [],
    outcome: null,
    retry_of_step_id: null,
    started_at: null,
    finished_at: null,
    ...partial,
  }
}

describe("stream decisions", () => {
  it("installs a newer revision and ignores an older one", () => {
    expect(decideSnapshot(clock, { ...clock, revision: 3, seq: 1, cursor: "n" }, false, "sse")).toBe("install")
    expect(decideSnapshot(clock, { ...clock, revision: 1, cursor: "old" }, false, "http")).toBe("ignore")
  })

  it("does not compare sequence numbers across runs", () => {
    expect(decideSnapshot(clock, { ...clock, runId: "run_next", seq: 1, cursor: "x" }, false, "sse")).toBe("resync")
    expect(decideSnapshot(clock, { ...clock, runId: "run_next", seq: 1, cursor: "x" }, false, "http")).toBe("install")
  })

  it("treats a repeated terminal cursor as closure, not a new row", () => {
    expect(decideSnapshot(clock, clock, true, "sse")).toBe("confirm")
    expect(decideSnapshot(clock, clock, false, "sse")).toBe("ignore")
  })

  it("accepts the next step only, and asks for a reread when a sequence number is missing", () => {
    expect(decideUpsert(clock, { ...clock, seq: 19, cursor: "c19" }, 19)).toBe("apply")
    expect(decideUpsert(clock, { ...clock, seq: 19, cursor: "c19" }, 18)).toBe("resync")
    expect(decideUpsert(clock, { ...clock, seq: 21, cursor: "c21" }, 21)).toBe("resync")
    expect(decideUpsert(clock, { ...clock, seq: 18, cursor: "c18" }, 18)).toBe("ignore")
    expect(decideUpsert(null, { ...clock, seq: 19, cursor: "c19" }, 19)).toBe("resync")
  })
})

describe("activity merge", () => {
  it("replaces by id and keeps the newer sequence for the same run", () => {
    const older = step({ id: "menu", last_seq: 19, title: "已取得画面" })
    const stale = step({ id: "menu", last_seq: 17, title: "正在查看" })
    const added = step({ id: "write", last_seq: 20, segment_no: 1, first_seq: 20 })
    const merged = mergeSteps([older], [stale, added], false)
    expect(merged.map((item) => [item.id, item.last_seq, item.title])).toEqual([
      ["menu", 19, "已取得画面"],
      ["write", 20, "write"],
    ])
  })

  it("drops the previous binding when the revision is replaced", () => {
    const kept = mergeSteps([step({ id: "old", last_seq: 4 })], [step({ id: "new", last_seq: 1 })], true)
    expect(kept.map((item) => item.id)).toEqual(["new"])
  })
})
