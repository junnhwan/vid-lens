import { describe, expect, it } from "vitest"
import type { ActivityStep, Block, Citation } from "../contract/types"
import { exampleChoices, exampleRoute, replaySteps, replayTimeline, type ExampleData } from "./exampleReplay"

const archives = import.meta.glob("../../public/example/*.json", { eager: true, import: "default" }) as Record<string, ExampleData>
const images = import.meta.glob("../../public/example/**/*.jpg")
const examples = exampleChoices.map((choice) => archives[`../../public/example/${choice.id}.json`])

describe("saved example playback", () => {
  it("resolves direct links and falls back to a known static example", () => {
    expect(exampleRoute("?video=skills&view=qa&question=frame")).toEqual({ id: "skills", mode: "qa", question: "frame" })
    expect(exampleRoute("?video=../../api&view=other")).toEqual({ id: "workflow", mode: "article", question: null })
  })
  it("does not reveal final evidence or final ASR progress during a replayed step", () => {
    const steps = examples[1].generation_steps
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
    const steps = examples[1].questions[1].steps
    const timeline = replayTimeline(steps)
    expect(replaySteps(steps, timeline.duration)).toEqual(steps)
    expect(replaySteps(steps, 0)).toEqual([])
    expect(replaySteps(steps, 1)).toHaveLength(1)
    expect(replaySteps(steps, 1)[0].outcome).toBeNull()
    expect(replayTimeline([]).duration).toBeGreaterThan(0)
  })
  it("preserves a failed investigation step inside a successful article record", () => {
    const steps = examples[1].generation_steps
    expect(steps.some((step) => step.status === "failed")).toBe(true)
    expect(replaySteps(steps, replayTimeline(steps).duration)).toEqual(steps)
    for (const entry of replayTimeline(steps).entries) expect(entry.end - entry.start).toBeGreaterThanOrEqual(1100)
  })
})

describe("public example archives", () => {
  for (const data of examples) it(`${data.id} has self-contained answers, citations, and durable static images`, () => {
    const frameIds = new Set(data.frames.map((frame) => frame.id))
    expect(frameIds.size).toBe(data.frames.length)
    function checkBlocks(blocks: Block[], citations: Record<string, Citation>) {
      for (const block of blocks) {
        for (const id of block.frame_ids || []) expect(frameIds.has(id), id).toBe(true)
        for (const inline of [...(block.content || []), ...(block.items || []).flatMap((item) => item.content)]) {
          if (inline.type === "citation") expect(citations[inline.citation_id || ""]).toBeDefined()
        }
      }
      for (const citation of Object.values(citations)) if (citation.frame_id) expect(frameIds.has(citation.frame_id), citation.frame_id).toBe(true)
    }
    function checkSteps(steps: ActivityStep[]) {
      for (const step of steps) {
        expect(["completed", "failed", "canceled"]).toContain(step.status)
        expect(step.frames.map((frame) => frame.id)).toEqual(step.frame_ids)
        for (const id of step.frame_ids) expect(frameIds.has(id)).toBe(true)
      }
    }
    checkBlocks(data.article.sections.flatMap((section) => section.blocks), data.article.citations)
    checkSteps(data.generation_steps)
    expect(data.questions).toHaveLength(2)
    for (const question of data.questions) {
      expect(question.answer.blocks.length).toBeGreaterThan(0)
      expect(question.duration_ms).toBeGreaterThan(0)
      if (question.context_frame_id) expect(frameIds.has(question.context_frame_id)).toBe(true)
      checkBlocks(question.answer.blocks, question.answer.citations)
      checkSteps(question.steps)
    }
    for (const frame of data.frames) {
      expect(frame.expires_at).toBeNull()
      for (const url of Object.values(frame.urls)) {
        expect(url).toMatch(/^\/example\/[a-z0-9/.-]+\.jpg$/)
        expect(images[`../../public${url}`], url || "").toBeDefined()
      }
    }
    expect(JSON.stringify(data)).not.toMatch(/\/api\/|csrf|token_hash|conversation_id|request_id|provider_request|api_key|"[a-f0-9]{32}"/i)
  })
})
