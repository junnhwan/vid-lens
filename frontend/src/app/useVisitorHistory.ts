import { useState } from "react"
import { lineageHistoryUrls, loadActivityHistory } from "../contract/activity-history"
import type { ActivityStep, Frame, RequestView, TurnView } from "../contract/types"
import type { VisitorContext } from "./visitorContext"
import { normalizeStep } from "./visitorContract"

interface HistoryResources {
  snapshot(turnId?: string): RequestView | TurnView | null | undefined
  merge(id: string, steps: ActivityStep[], turnId?: string): boolean
  absorb(frames: Frame[]): void
}

export function useVisitorHistory(context: VisitorContext, resources: HistoryResources) {
  const [historyState, setHistoryState] = useState<Record<string, string>>({})

  async function load(turnId?: string) {
    const runtime = context.runtime()
    const view = resources.snapshot(turnId)
    if (!runtime || !view) return
    const id = turnId ?? context.request()!.request.id
    const generation = context.workspace.epoch
    const owner = context.workspace.route
    if (historyState[id] === "正在加载完整过程…") return
    setHistoryState(previous => ({ ...previous, [id]: "正在加载完整过程…" }))
    const owns = () => context.workspace.owns(generation, owner)
    try {
      const urls = [view.activity.history_url, ...(view.activity.attempts ?? []).filter(attempt => !attempt.current).map(attempt => attempt.history_url)]
      if ("request" in view) urls.push(...await lineageHistoryUrls(view, id => runtime.api.getRequest(id)))
      if (!owns()) return
      const steps = (await loadActivityHistory(urls, (url, before) => runtime.api.activity(url, before))).map(normalizeStep)
      if (!owns() || !resources.merge(id, steps, turnId)) return
      resources.absorb(steps.flatMap(step => step.frames))
      setHistoryState(previous => ({ ...previous, [id]: "" }))
    } catch (error) {
      if (!owns()) return
      setHistoryState(previous => ({ ...previous, [id]: error instanceof Error ? error.message : "完整过程加载失败，请收起后重新展开。" }))
    }
  }

  return { historyState, load, reset: () => setHistoryState({}) }
}
