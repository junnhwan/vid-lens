import { describe, expect, it } from "vitest"
import type { ActivityStep } from "../contract/types"
import { catalogChoice, exampleArchive, exampleRoute, replaySteps, replayTimeline } from "./exampleReplay"

function step(patch: Partial<ActivityStep> & Pick<ActivityStep, "id" | "title" | "status">): ActivityStep {
  return {
    run_id: "run", segment_no: 1, first_seq: 1, last_seq: 1, phase: "investigate",
    purpose: null, source_range: null, frame_ids: [], frames: [], outcome: null,
    retry_of_step_id: null, started_at: "2026-10-18T00:00:00.000Z", finished_at: "2026-10-18T00:00:20.000Z",
    ...patch,
  }
}

const transcript = step({
  id: "transcribe", title: "正在转写音频（12/22）", status: "completed",
    outcome: { message: "转写完成", evidence_status: "ready" },
  frame_ids: ["frame"], frames: [{ id: "frame", snapshot_id: "snap", time_ms: 1000, requested_time_ms: 1000, width: 1, height: 1, mime_type: "image/jpeg", caption: "", crop: null, replaces_frame_id: null, availability: "ready", unavailable_reason: null, expires_at: null, urls: { full: "/api/v1/examples/x/frames/frame/content", thumbnail: "/api/v1/examples/x/frames/frame/content" } }],
})
const failed = step({ id: "look", title: "查看画面", status: "failed", started_at: "2026-10-18T00:00:20.000Z", finished_at: "2026-10-18T00:00:40.000Z" })

describe("saved example playback", () => {
  it("opens a selected article and ignores a removed static id", () => {
    expect(exampleRoute("?video=9280423ce43c85da9cc539a7576749a7&view=process")).toEqual({ id: "9280423ce43c85da9cc539a7576749a7", mode: "process", question: null })
    expect(exampleRoute("?video=skills&view=qa&question=frame")).toEqual({ id: null, mode: "qa", question: "frame" })
    expect(exampleRoute("?video=../../api&view=other")).toEqual({ id: null, mode: "article", question: null })
    expect(exampleArchive("9280423ce43c85da9cc539a7576749a7")).toBe("/api/v1/examples/9280423ce43c85da9cc539a7576749a7")
    expect(catalogChoice({ id: "9280423ce43c85da9cc539a7576749a7", title: "已发布", description: "第一节", platform: "youtube", duration_ms: null, cover: "" }).meta).toBe("YouTube")
  })
  it("does not reveal final evidence or final ASR progress during a replayed step", () => {
    const steps = [transcript, failed]
    const original = JSON.stringify(steps)
    const current = replaySteps(steps, 1)[0]
    expect(current.status).toBe("running")
    expect(current.title).toBe("正在转写音频")
    expect(current.outcome).toBeNull()
    expect(current.finished_at).toBeNull()
    expect(current.frames).toEqual([])
    expect(current.frame_ids).toEqual([])
    expect(JSON.stringify(steps)).toBe(original)
  })
  it("seeks backward without carrying an answer or evidence from the future", () => {
    const steps = [transcript, failed]
    const timeline = replayTimeline(steps)
    expect(replaySteps(steps, timeline.duration)).toEqual(steps)
    expect(replaySteps(steps, 0)).toEqual([])
    expect(replaySteps(steps, 1)).toHaveLength(1)
    expect(replaySteps(steps, 1)[0].outcome).toBeNull()
    expect(replayTimeline([]).duration).toBeGreaterThan(0)
  })
  it("preserves a failed investigation step inside a successful article record", () => {
    const steps = [transcript, failed]
    expect(steps.some((item) => item.status === "failed")).toBe(true)
    expect(replaySteps(steps, replayTimeline(steps).duration)).toEqual(steps)
    for (const entry of replayTimeline(steps).entries) expect(entry.end - entry.start).toBeGreaterThanOrEqual(1100)
  })
})
