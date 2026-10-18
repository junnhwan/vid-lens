import { describe, expect, it, vi } from "vitest"
import { renderToStaticMarkup } from "react-dom/server"
import { ActivityList } from "../ui/ActivityList"
import { activityGroups, lineageHistoryUrls, loadActivityHistory } from "./activity-history"
import { advanceActivity, mergeActivitySnapshot } from "./activity"
import type { ActivityPage, ActivityStep } from "./types"

const step = (id: string, kind: string, call: number): ActivityStep => ({ id, kind, model_call: call, run_id: "run", first_seq: 1, last_seq: 1, segment_no: 1, phase: "compose", title: "整理本批章节", purpose: "撰写沙箱与快照两章", status: "completed", frames: [], frame_ids: [], started_at: null, finished_at: null, outcome: null, source_range: null, retry_of_step_id: null })
describe("complete execution history", () => {
  it("shows the reason for a source retry in the compact process list", () => {
    const retry = { ...step("source-retry", "retry", 0), phase: "prepare_source", title: "来源准备中断，将自动重试", outcome: { message: "下载长时间没有字节进展。", evidence_status: "not_assessed" } }
    const html = renderToStaticMarkup(<ActivityList steps={[retry]} open={false} detailed={false} resourceStatus="waiting_source" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(html).toContain("下载长时间没有字节进展。")
    expect(html).toContain("来源准备中断，将自动重试")
  })
  it("retains loaded ancestor actions across source-to-article binding changes", () => {
    const prior = {...step("earlier-failed", "tool_execution", 0), run_id: "old", status: "failed", started_at: "2026-10-17T00:00:00Z"}
    const source = {...step("source", "tool_execution", 0), run_id: "source", started_at: "2026-10-17T00:01:00Z"}
    const response = {...step("new-call", "model_response", 1), started_at: "2026-10-17T00:02:00Z"}
    const next = mergeActivitySnapshot([prior, source], [source, response], {replace:true, sameResource:true, expired:false})
    expect(next.map((item) => item.id)).toEqual(["earlier-failed", "source", "new-call"])
    expect(next[0].status).toBe("failed")
    expect(mergeActivitySnapshot(next, [response], {replace:true, sameResource:false, expired:false})).toEqual([response])
    expect(mergeActivitySnapshot(next, [response], {replace:true, sameResource:true, expired:true})).toEqual([response])
  })
  it("includes earlier source failures even when no article attempt existed", async () => {
    const owner = (id: string, prior: string | null) => ({ request: { id, retry_of_id: prior }, activity: { history_url: `/requests/${id}/activity` } })
    const read = vi.fn(async () => owner("source-failed", null))
    expect(await lineageHistoryUrls(owner("current", "source-failed"), read)).toEqual(["/requests/current/activity", "/requests/source-failed/activity"])
    expect(read).toHaveBeenCalledWith("source-failed")
    await expect(lineageHistoryUrls(owner("loop", "loop"), async () => owner("loop", "loop"))).rejects.toThrow("关联记录不完整")
  })
  it("groups retries below one logical call and retains its meaningful title", () => {
    const steps = [step("parent", "model_response", 1), step("send1", "model_submission", 1), step("send2", "model_submission", 1)]
    expect(activityGroups(steps)).toHaveLength(1)
    const html = renderToStaticMarkup(<ActivityList steps={steps} open detailed resourceStatus="completed" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(html.match(/role="listitem"/g)).toHaveLength(1)
    expect(html).toContain("撰写沙箱与快照两章")
    expect(html).toContain("发送 2 次")
    expect(html).toContain("<details")
    expect(activityGroups([...steps, {...step("prior", "model_response", 1), run_id:"prior"}])).toHaveLength(2)
  })
  it("loads every page and deduplicates shared source events across attempts", async () => {
    const shared = step("shared", "tool_execution", 0)
    const read = vi.fn(async (url: string, before: string | null) => ({ items: [shared, step(`${url}:${before}`, "model_response", 1)], has_more: before === null, next_before: before === null ? "page2" : null, history_status: "available" } as ActivityPage))
    const steps = await loadActivityHistory(["current", "prior", "prior"], read)
    expect(read).toHaveBeenCalledTimes(4)
    expect(steps).toHaveLength(5)
  })
  it("reports incomplete or looping pagination instead of claiming full history", async () => {
    await expect(loadActivityHistory(["current"], async () => ({ schema_version: 1, through_cursor: "current", history_expires_at: null, items: [], has_more: true, next_before: "repeat", history_status: "available" }))).rejects.toThrow("分页未完成")
  })
  it("updates live plans and counts without waiting for a new snapshot", () => {
    const activity = { model_calls: 2, model_submissions: 2, current_step_id: null, recent_steps: [], has_more: false, history_url: "", history_status: "available", history_expires_at: null }
    const next = advanceActivity(activity, step("call3", "model_response", 3), "call3", true)
    expect(next.model_calls).toBe(3)
    const sent = advanceActivity(next, step("send3", "model_submission", 3), "call3", true)
    expect(sent.model_submissions).toBe(3)
    expect(advanceActivity(sent, step("send3", "model_submission", 3), "call3", false).model_submissions).toBe(3)
  })
})
