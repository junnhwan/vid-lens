import type { ActivityStep, Answer, ArticleResponse, GenerationModel } from "../contract/types"

export interface ExampleQuestion {
  id: string
  label: string
  question: string
  context_frame_id: string | null
  answer: Answer
  steps: ActivityStep[]
  recorded_at: string
  duration_ms: number
  generation_model?: GenerationModel
}
export interface ExampleData extends ArticleResponse {
  id: string
  quality_note?: string
  generation_steps: ActivityStep[]
  questions: ExampleQuestion[]
}
export type ExampleMode = "article" | "process" | "qa"
export const exampleChoices = [
  { id: "workflow", title: "从 Grill 到 Retro", description: "一套 AI 开发工作流，如何把想法变成代码。", meta: "Bilibili · 09:04", tag: "完整工作流", cover: "/example/frame-02.jpg" },
  { id: "skills", title: "Skills v1.3，三个新技能", description: "从自动实现，到 PR 证据，再到会话复盘。", meta: "YouTube · 14:34", tag: "技能更新", cover: "/example/skills/frame-05.jpg" },
] as const

export function exampleRoute(search: string): { id: string; mode: ExampleMode; question: string | null } {
  const query = new URLSearchParams(search)
  const id = exampleChoices.find((item) => item.id === query.get("video"))?.id ?? "workflow"
  const view = query.get("view")
  return { id, mode: view === "process" || view === "qa" ? view : "article", question: query.get("question") }
}

export function replayTimeline(steps: ActivityStep[]) {
  let duration = 0
  const entries = steps.map((step) => {
    const real = Date.parse(step.finished_at || "") - Date.parse(step.started_at || "")
    const dwell = Number.isFinite(real) ? Math.min(2600, Math.max(1100, real / 20)) : 1600
    const start = duration
    duration += dwell
    return { step, start, end: duration }
  })
  return { entries, duration: Math.max(1800, duration + 700) }
}

// A running replay step must not reveal its final outcome, ASR counter, or
// images before the saved step finishes. Original archived steps stay intact.
export function replaySteps(steps: ActivityStep[], position: number): ActivityStep[] {
  if (position <= 0) return []
  return replayTimeline(steps).entries.filter((entry) => position > entry.start).map(({ step, end }) => position >= end ? step : {
    ...step, status: "running", title: step.title.replace(/^(正在转写音频)[（(].*$/, "$1"),
    outcome: null, frames: [], frame_ids: [], finished_at: null,
  })
}
