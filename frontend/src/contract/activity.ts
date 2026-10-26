import type { ActivityStep, ActivitySummary } from "./types"

export function sortSteps(steps: ActivityStep[]): ActivityStep[] {
  const starts = new Map<string, number>()
  for (const step of steps) if (step.started_at) { const time = Date.parse(step.started_at); if (Number.isFinite(time)) starts.set(step.run_id, Math.min(starts.get(step.run_id) ?? Infinity, time)) }
  return [...steps].sort((a, b) => {
    if (a.run_id !== b.run_id && starts.has(a.run_id) && starts.has(b.run_id)) {
      const time = starts.get(a.run_id)! - starts.get(b.run_id)!
      if (time) return time
    }
    return a.segment_no - b.segment_no || a.first_seq - b.first_seq || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
  })
}

export function advanceActivity(activity: ActivitySummary, step: ActivityStep, currentId: string | null, firstSeen: boolean): ActivitySummary {
  const calls = step.kind === "model_response" ? Math.max(activity.model_calls ?? 0, step.model_call ?? 0) : activity.model_calls
  const sends = (activity.model_submissions ?? 0) + (step.kind === "model_submission" && firstSeen ? 1 : 0)
  let callDelta = 0
  let sendDelta = 0
  const attempts = activity.attempts?.map((attempt) => {
    if (attempt.run_id !== step.run_id) return attempt
    const modelCalls = step.kind === "model_response" ? Math.max(attempt.model_calls, step.model_call ?? 0) : attempt.model_calls
    const modelSends = attempt.model_submissions + (step.kind === "model_submission" && firstSeen ? 1 : 0)
    callDelta = modelCalls - attempt.model_calls; sendDelta = modelSends - attempt.model_submissions
    return { ...attempt, model_calls: modelCalls, model_submissions: modelSends, ...(step.plan?.sections ? { saved_sections: step.plan.sections.filter((item) => item.status === "done").length, planned_sections: step.plan.open_ended ? 0 : step.plan.sections.length, open_ended: step.plan.open_ended } : {}) }
  })
  return { ...activity, current_step_id: currentId, model_calls: calls, model_submissions: sends, plan: step.plan ?? activity.plan, attempts, total_model_calls: (activity.total_model_calls ?? 0) + callDelta, total_model_submissions: (activity.total_model_submissions ?? 0) + sendDelta }
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

// A new SSE binding/revision replaces its cursor, not the owned history already
// read for this resource. Keep that history through preparation/writing stages.
export function mergeActivitySnapshot(existing: ActivityStep[], incoming: ActivityStep[], options: { replace: boolean; sameResource: boolean; expired: boolean; reusedRunIds?: string[] }): ActivityStep[] {
  const reused = new Set(options.reusedRunIds ?? incoming.filter(step => step.kind === "reuse").map(step => step.run_id))
  const retained = existing.filter(step => !reused.has(step.run_id) || step.kind === "reuse")
  return mergeSteps(retained, incoming, options.replace && (!options.sameResource || options.expired))
}

export function asSteps(value: unknown): ActivityStep[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is ActivityStep => !!item && typeof item === "object" && typeof (item as ActivityStep).id === "string")
}
