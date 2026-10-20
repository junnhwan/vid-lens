import type { ActivityPage, ActivityStep, ActivitySummary, RequestBody } from "./types"
import { mergeSteps } from "./activity"

type HistoryOwner = { request: Pick<RequestBody, "id" | "retry_of_id">; activity: Pick<ActivitySummary, "history_url"> }
export async function lineageHistoryUrls(view: HistoryOwner, read: (id: string) => Promise<HistoryOwner>): Promise<string[]> {
  const urls: string[] = []
  const seen = new Set<string>()
  let current = view
  for (let depth = 0; ; depth += 1) {
    if (depth >= 40 || seen.has(current.request.id)) throw new Error("较早过程的关联记录不完整，请单独打开较早的请求查看。")
    seen.add(current.request.id); urls.push(current.activity.history_url)
    if (!current.request.retry_of_id) return urls
    current = await read(current.request.retry_of_id)
  }
}

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

interface ActivityGroup { step: ActivityStep; sends: ActivityStep[]; observations: ActivityStep[] }

function observationSummary(items: ActivityStep[]): ActivityStep {
  const completed = items.filter((item) => item.status === "completed").length
  const active = items.find((item) => item.status === "running" || item.status === "queued")
  const stopped = items.find((item) => item.status === "failed") ?? items.find((item) => item.status === "canceled")
  const status = active?.status ?? stopped?.status ?? "completed"
  const done = completed === items.length
  const finished = items.every((item) => item.finished_at) ? items.reduce((latest, item) => Date.parse(item.finished_at!) > Date.parse(latest!) ? item.finished_at : latest, items[0].finished_at) : null
  return {
    ...items[0], kind: "observation_group", status,
    title: done ? `已保存 ${items.length} 组画面观察` : `${active ? "正在保存画面观察" : "画面观察保存未完成"}（${completed}/${items.length} 组已保存）`,
    last_seq: Math.max(...items.map((item) => item.last_seq)),
    finished_at: active ? null : finished, source_range: null, frames: [], frame_ids: [],
    outcome: done ? { message: "已保存可见事实与未确认问题，后续可从此继续。", evidence_status: "not_assessed" } : stopped?.outcome ?? null,
  }
}

const stagePatterns: [RegExp, string][] = [
  [/转写|复用已完整校验/, "transcribe"],
  [/校验视频时长/, "probe"],
  [/获取用于观察|完整视频/, "download"],
  [/字幕/, "subtitles"],
  [/准备视频和文字来源/, "prepare"],
]

function stageKey(title: string): string {
  return stagePatterns.find(([pattern]) => pattern.test(title))?.[1] ?? title
}

function sourceWork(step: ActivityStep): boolean {
  return step.phase === "prepare_source" && step.kind !== "retry" && step.kind !== "model_response" && step.kind !== "model_submission" && step.kind !== "execution_plan" && step.kind !== "observation_group"
}

function sourceRetry(step: ActivityStep): boolean {
  return step.kind === "retry" && (step.phase === "prepare_source" || /来源准备中断|准备中断/.test(step.title))
}

function newAttempt(title: string): boolean {
  return /第\s*\d+\s*次尝试/.test(title)
}

interface PrepAttempt { items: ActivityStep[]; retry: ActivityStep | null }

function interruptNotice(attempt: PrepAttempt): ActivityStep {
  const stopped = [...attempt.items].reverse().find((step) => step.status === "failed" || step.status === "canceled")
  const reason = attempt.retry?.outcome?.message || stopped?.outcome?.message || "这次准备没有完成。"
  const attemptNo = attempt.retry?.title.match(/第\s*(\d+)\s*次/)?.[1] || attempt.items.map((step) => step.title.match(/第\s*(\d+)\s*次尝试/)?.[1]).find(Boolean)
  const seed = attempt.items[0]
  return {
    ...seed,
    id: attempt.retry?.id ?? `prep-interrupt-${seed.run_id}-${seed.id}`,
    kind: "retry",
    phase: "prepare_source",
    title: attemptNo ? `第 ${attemptNo} 次准备中断` : "上一次准备中断",
    status: "canceled",
    purpose: null,
    source_range: null,
    frames: [],
    frame_ids: [],
    outcome: { message: reason, evidence_status: attempt.retry?.outcome?.evidence_status || stopped?.outcome?.evidence_status || "not_assessed" },
  }
}

// Earlier source-preparation rounds repeat the same stages. Keep one row per
// stage in the open round, and fold an interrupted round into its reason.
export function collapseSourceRetries(steps: ActivityStep[]): ActivityStep[] {
  const attempts: PrepAttempt[] = []
  let attempt: PrepAttempt | null = null
  for (const step of steps) {
    if (sourceRetry(step)) {
      const bucket = attempt ?? { items: [], retry: null }
      if (!attempt) attempts.push(bucket)
      bucket.retry = step
      attempt = null
      continue
    }
    if (!sourceWork(step)) continue
    if (!attempt) {
      attempt = { items: [step], retry: null }
      attempts.push(attempt)
      continue
    }
    const opened = attempt.items[0]
    const split = opened.run_id !== step.run_id || (newAttempt(step.title) && !newAttempt(opened.title))
    if (split) {
      attempt = { items: [step], retry: null }
      attempts.push(attempt)
      continue
    }
    attempt.items.push(step)
  }
  if (!attempts.length) return steps
  const skip = new Set<string>()
  const replace = new Map<string, ActivityStep>()
  attempts.forEach((bucket, index) => {
    const last = index === attempts.length - 1
    const interrupted = bucket.items.some((step) => step.status === "failed" || step.status === "canceled")
    if (bucket.items.length && (bucket.retry || (!last && interrupted))) {
      for (const step of bucket.items) skip.add(step.id)
      if (bucket.retry) skip.add(bucket.retry.id)
      replace.set(bucket.items[0].id, interruptNotice(bucket))
      return
    }
    const latest = new Map<string, ActivityStep>()
    for (const step of bucket.items) latest.set(stageKey(step.title), step)
    const emitted = new Set<string>()
    for (const step of bucket.items) {
      const key = stageKey(step.title)
      if (emitted.has(key)) {
        skip.add(step.id)
        continue
      }
      emitted.add(key)
      const chosen = latest.get(key)!
      if (chosen.id !== step.id) {
        replace.set(step.id, chosen)
        skip.add(chosen.id)
      }
    }
  })
  const result: ActivityStep[] = []
  for (const step of steps) {
    const swapped = replace.get(step.id)
    if (swapped) {
      result.push(swapped)
      continue
    }
    if (skip.has(step.id)) continue
    result.push(step)
  }
  return result
}

export function activityGroups(steps: ActivityStep[]) {
  const groups: ActivityGroup[] = []
  const calls = new Map<string, ActivityGroup>()
  for (const step of steps) {
    if (step.kind === "execution_plan") continue
    if (step.kind === "tool_execution" && step.title === "保存视频画面观察") {
      const previous = groups.at(-1)
      if (previous?.observations.length && step.model_call && previous.step.run_id === step.run_id && previous.step.model_call === step.model_call && previous.step.phase === step.phase) {
        previous.observations.push(step)
        previous.step = observationSummary(previous.observations)
      } else groups.push({ step, sends: [], observations: [step] })
      continue
    }
    if (step.kind !== "model_response" && step.kind !== "model_submission") { groups.push({ step, sends: [], observations: [] }); continue }
    const key = `${step.run_id}:${step.model_call ?? step.id}`
    let group = calls.get(key)
    if (!group) { group = { step, sends: [], observations: [] }; calls.set(key, group); groups.push(group) }
    if (step.kind === "model_response") group.step = step
    else group.sends.push(step)
  }
  return groups
}
