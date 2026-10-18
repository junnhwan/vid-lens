import type { ActivityStep, Answer, ArticleResponse, GenerationModel } from "../contract/types"
import { formatDuration, platformLabel } from "../contract/text"

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

export interface CatalogItem {
  id: string
  title: string
  description: string
  platform: string
  duration_ms: number | null
  cover: string
}

const exampleIdPattern = /^[a-f0-9]{32}$/

export function exampleArchive(id: string): string {
  return `/api/v1/examples/${id}`
}

export function catalogChoice(item: CatalogItem) {
  return {
    id: item.id,
    title: item.title,
    description: item.description,
    meta: [platformLabel(item.platform), formatDuration(item.duration_ms)].filter(Boolean).join(" · "),
    tag: "已发布",
    cover: item.cover,
  }
}

export function exampleRoute(search: string): { id: string | null; mode: ExampleMode; question: string | null } {
  const query = new URLSearchParams(search)
  const requested = query.get("video") || ""
  const id = exampleIdPattern.test(requested) ? requested : null
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
