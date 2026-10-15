import type { ActivityStep } from "./types"

export function sortSteps(steps: ActivityStep[]): ActivityStep[] {
  return [...steps].sort((a, b) => a.segment_no - b.segment_no || a.first_seq - b.first_seq || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
}

export function mergeSteps(existing: ActivityStep[], incoming: ActivityStep[], replace: boolean): ActivityStep[] {
  if (replace) return sortSteps(incoming)
  const map = new Map(existing.map((step) => [step.id, step]))
  for (const step of incoming) {
    const previous = map.get(step.id)
    if (previous && previous.run_id === step.run_id && step.last_seq < previous.last_seq) continue
    map.set(step.id, step)
  }
  return sortSteps([...map.values()])
}

export function asSteps(value: unknown): ActivityStep[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is ActivityStep => !!item && typeof item === "object" && typeof (item as ActivityStep).id === "string")
}
