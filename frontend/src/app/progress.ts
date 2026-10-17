import type { ActivityStep } from "../contract/types"
import { formatClock } from "../contract/text"

export const QUIET_AFTER_MS = 20_000

export const PHASES = [
  { id: "prepare_source", label: "准备" },
  { id: "overview", label: "通览" },
  { id: "investigate", label: "看画面" },
  { id: "compose", label: "写文章" },
  { id: "validate", label: "核对" },
  { id: "publish", label: "发布" },
] as const

export type PhaseMark = "done" | "current" | "waiting" | "stopped"

const transcription = /^正在转写音频（(\d+:\d{2}) \/ (\d+:\d{2})）$/

function clockSeconds(value: string): number | null {
  const match = /^(\d+):(\d{2})$/.exec(value)
  if (!match) return null
  const seconds = Number(match[2])
  if (seconds > 59) return null
  return Number(match[1]) * 60 + seconds
}

export function parseTranscription(title: string): { done: string; total: string; ratio: number } | null {
  const match = transcription.exec(title)
  if (!match) return null
  const done = clockSeconds(match[1])
  const total = clockSeconds(match[2])
  if (done == null || total == null || total <= 0) return null
  return { done: match[1], total: match[2], ratio: Math.min(1, done / total) }
}

export function displayStepTitle(title: string): string {
  return parseTranscription(title) ? "正在转写音频" : title
}

export function phaseMarks(phase: string | null | undefined, mode: "running" | "done" | "stopped"): PhaseMark[] {
  if (mode === "done") return PHASES.map(() => "done")
  const index = PHASES.findIndex((item) => item.id === phase)
  return PHASES.map((_, item) => {
    if (index < 0) return mode === "stopped" ? "stopped" : "waiting"
    if (item < index) return "done"
    if (item === index) return mode === "stopped" ? "stopped" : "current"
    return "waiting"
  })
}

export function quietNote(phase: string | null | undefined, status: string | null | undefined): string {
  if (status === "retry_wait") return ""
  if (status === "queued") return "还在排队。开始之后，这里会换成实际在做的事。"
  if (status === "waiting_dependency") return "需要先把原来的视频准备回来，才能继续看画面。"
  switch (phase) {
    case "prepare_source":
      return "下载和转写按整条视频进行，这一步通常比后面更久。"
    case "overview":
      return "正在通读整条视频，具体画面会在下一步出现。"
    case "investigate":
      return "正在核对具体片段，新的画面会收在下面。"
    case "compose":
      return "画面已经看过，正在写成可以独立阅读的文章。"
    case "validate":
      return "文章已经写出，正在核对引用和画面是否对得上。"
    case "publish":
      return "核对已通过，正在把文章准备好。"
    default:
      return "这一步还在进行，完成后标题会换成下一步。"
  }
}

export function waitClock(createdAt: string, endMs: number): string | null {
  const start = Date.parse(createdAt)
  if (Number.isNaN(start) || Number.isNaN(endMs)) return null
  return formatClock(Math.max(0, endMs - start))
}

export function stepSpent(step: Pick<ActivityStep, "started_at" | "finished_at">): string | null {
  if (!step.started_at || !step.finished_at) return null
  const ms = Date.parse(step.finished_at) - Date.parse(step.started_at)
  if (Number.isNaN(ms) || ms < 0) return null
  if (ms < 1000) return "<1秒"
  return formatClock(ms)
}

export interface ElapsedAnchor { elapsedMs: number; monotonicMs: number }

export function activityTitle(step: ActivityStep): string {
  if (step.kind !== "model_response") return displayStepTitle(step.title)
  if (step.purpose) return displayStepTitle(step.purpose)
  if (step.title && !/^第\s*\d+\s*次模型调用|^模型调用/.test(step.title)) return displayStepTitle(step.title)
  const ordinal = step.model_call ?? (Number(step.id.match(/_(\d+)$/)?.[1] ?? -1) + 1)
  return ordinal > 0 ? `第 ${ordinal} 次模型调用` : "模型调用"
}

// Browser wall clocks can differ from the server or jump during a run.
export function anchoredElapsed(anchor: ElapsedAnchor, monotonicNow: number): number {
  return Math.max(0, anchor.elapsedMs + Math.max(0, monotonicNow - anchor.monotonicMs))
}
export function serverElapsed(createdAt: string, serverNow: string): number | null {
  const elapsed = Date.parse(serverNow) - Date.parse(createdAt)
  return Number.isFinite(elapsed) ? Math.max(0, elapsed) : null
}
