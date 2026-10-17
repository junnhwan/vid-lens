import type { ActivityPage, ActivityStep } from "./types"
import { mergeSteps } from "./activity"

export async function loadActivityHistory(urls: string[], read: (url: string, before: string | null) => Promise<ActivityPage>): Promise<ActivityStep[]> {
  let result: ActivityStep[] = []
  for (const url of new Set(urls)) {
    let before: string | null = null
    const cursors = new Set<string>()
    for (let pageNo = 0; ; pageNo += 1) {
      if (pageNo >= 100) throw new Error("过程记录过多，请重新展开以重试。")
      const page = await read(url, before)
      if (page.history_status === "expired") throw new Error("部分过程记录已过期，已保存的结果仍可阅读。")
      result = mergeSteps(result, page.items, false)
      if (!page.has_more) break
      if (!page.next_before || cursors.has(page.next_before)) throw new Error("过程记录分页未完成，请重新展开以重试。")
      cursors.add(page.next_before)
      before = page.next_before
    }
  }
  return result
}

export function activityGroups(steps: ActivityStep[]) {
  const groups: { step: ActivityStep; sends: ActivityStep[] }[] = []
  const calls = new Map<string, { step: ActivityStep; sends: ActivityStep[] }>()
  for (const step of steps) {
    if (step.kind === "execution_plan") continue
    if (step.kind !== "model_response" && step.kind !== "model_submission") { groups.push({ step, sends: [] }); continue }
    const key = `${step.run_id}:${step.model_call ?? step.id}`
    let group = calls.get(key)
    if (!group) { group = { step, sends: [] }; calls.set(key, group); groups.push(group) }
    if (step.kind === "model_response") group.step = step
    else group.sends.push(step)
  }
  return groups
}
