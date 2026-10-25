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

it("shows saved chapters without inventing a total and returns review to pending during revision", () => {
  const activity: ActivitySummary = { current_step_id: null, recent_steps: [], has_more: false, history_url: "", history_status: "available", history_expires_at: null,
    plan: { open_ended: true, wrap_up: false, items: [{ id: "write", title: "按证据修订文章", status: "active" }, { id: "validate", title: "独立核验当前候选", status: "pending" }], sections: [{ id: "s1", title: "已保存主题", status: "done" }] } }
  const html = renderToStaticMarkup(<ExecutionPlan activity={activity} completed={false} phase="compose" />)
  expect(html).toContain("已保存章节")
  expect(html).toContain("已保存 1 章，完成后还需核验")
  expect(html).not.toContain("全部 1 章已保存")
  const published = renderToStaticMarkup(<ExecutionPlan activity={activity} completed phase="publish" />)
  expect(published).toContain("已保存 1 章，核验已完成")
  expect(published).not.toContain("完成后还需核验")
  expect(html).not.toContain("stroke-dasharray")
  expect(html).toMatch(/data-state="pending"[^]*独立核验当前候选/)
})

it("does not describe independent advice as a delivery gate", () => {
  const activity = { plan: { open_ended: true, items: [{ id: "write", title: "分章保存完整文章", status: "active" }, { id: "publish", title: "交付阅读和导出", status: "pending" }], sections: [{ id: "s1", title: "已保存主题", status: "done" }] } } as ActivitySummary
  const html = renderToStaticMarkup(<ExecutionPlan activity={activity} completed={false} />)
  expect(html).toContain("已保存 1 章，正在继续生成")
  expect(html).not.toContain("还需核验")
  const published = renderToStaticMarkup(<ExecutionPlan activity={activity} completed />)
  expect(published).toContain("已完成 1 章，可阅读和导出")
  expect(published).not.toContain("核验已完成")
})
