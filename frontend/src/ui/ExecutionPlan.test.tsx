import { describe, expect, it } from "vitest"
import { renderToStaticMarkup } from "react-dom/server"
import { ExecutionPlan } from "./ExecutionPlan"
import { ActivityList } from "./ActivityList"
import type { ActivityStep, ActivitySummary } from "../contract/types"
import { mergeSteps } from "../contract/activity"

describe("durable execution presentation", () => {
  it("shows logical calls, actual sends and the saved chapter plan separately", () => {
    const activity = { model_calls: 3, model_submissions: 4, plan: { wrap_up: true, note: "优先收尾", items: [{ id: "write", title: "保存章节", status: "active", detail: "已保存 2 / 4 章" }] } } as ActivitySummary
    const html = renderToStaticMarkup(<ExecutionPlan activity={activity} completed={false} />)
    expect(html).toContain("模型调用 3 次")
    expect(html).toContain("实际发送 4 次")
    expect(html).toContain("已保存 2 / 4 章")
    expect(html).toContain("优先收尾")
  })

  it("keeps every model call when live titles update and avoids repeating the tool title", () => {
    const step = (n: number): ActivityStep => ({ id: `model_run_${n}`, kind: "model_response", model_call: n + 1, run_id: "run", segment_no: 0, first_seq: n + 1, last_seq: n + 1, title: "查看实验画面", phase: "investigate", status: "completed", frames: [], frame_ids: [], purpose: null, outcome: { message: "下一步执行：查看实验画面。", evidence_status: "not_assessed" }, started_at: null, finished_at: null, source_range: null, retry_of_step_id: null })
    const existing = [step(0), step(1)]
    const updated = { ...step(1), last_seq: 8, title: "正在组织文章结构" }
    const steps = mergeSteps(existing, [updated, step(2)], false)
    const html = renderToStaticMarkup(<ActivityList steps={steps} resourceStatus="running" detailed open onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(steps).toHaveLength(3)
    for (const n of [1, 2, 3]) expect(html).toContain(`第 ${n} 次模型调用`)
    expect(html).toContain("已组织文章结构")
    expect(html).toContain("下一步执行：查看实验画面。")
  })
})
