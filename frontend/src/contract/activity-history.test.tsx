import { describe, expect, it, vi } from "vitest"
import { renderToStaticMarkup } from "react-dom/server"
import { ActivityList } from "../ui/ActivityList"
import { activityGroups, collapseSourceRetries, lineageHistoryUrls, loadActivityHistory } from "./activity-history"
import { advanceActivity, mergeActivitySnapshot } from "./activity"
import type { ActivityPage, ActivityStep } from "./types"

const step = (id: string, kind: string, call: number): ActivityStep => ({ id, kind, model_call: call, run_id: "run", first_seq: 1, last_seq: 1, segment_no: 1, phase: "compose", title: "整理本批章节", purpose: "撰写沙箱与快照两章", status: "completed", frames: [], frame_ids: [], started_at: null, finished_at: null, outcome: null, source_range: null, retry_of_step_id: null })
describe("complete execution history", () => {
  it("replaces borrowed source history with an explicit reuse notice without old timing", () => {
    const old = { ...step("old-download", "tool_execution", 0), run_id: "source", phase: "prepare_source", title: "第一次下载和转写", started_at: "2026-10-25T01:00:00Z", finished_at: "2026-10-25T01:02:00Z" }
    const reused = { ...step("reuse:request:source", "reuse", 0), run_id: "source", phase: "prepare_source", title: "复用已有视频和文字来源", outcome: { message: "本次没有重新下载或转写。", evidence_status: "not_assessed" } }
    const writing = { ...step("writing", "tool_execution", 1), run_id: "article" }
    const merged = mergeActivitySnapshot([old, writing], [reused], { replace: false, sameResource: true, expired: false, reusedRunIds: ["source"] })
    expect(merged.map(item => item.id)).toEqual([reused.id, writing.id])
    const html = renderToStaticMarkup(<ActivityList steps={merged} open detailed={false} resourceStatus="generating" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(html).toContain("复用已有视频和文字来源")
    expect(html).toContain("本次没有重新下载或转写")
    expect(html).not.toContain("第一次下载和转写")
    expect(html).not.toContain("耗时")
    expect(html).not.toContain("用时")
  })
  it("shows AI screenshot and refinement headings as separate actions with their outcomes", () => {
    const actions = [
      { ...step("capture", "tool_execution", 1), title: "按需取帧：查看邮件分类结果", outcome: { message: "已取得2张候选画面，继续写作与选图。", evidence_status: "not_assessed" } },
      { ...step("refine", "tool_execution", 1), title: "微调取帧：向后微调至结果出现", source_range: { start_ms: 12000, end_ms: 18000 } },
      { ...step("skip", "tool_execution", 1), title: "略过取帧：查看最终处理结果", outcome: { message: "剩余调用与时间优先用于完成正文，本次略过可选截图。", evidence_status: "not_assessed" } },
    ]
    expect(activityGroups(actions)).toHaveLength(3)
    const html = renderToStaticMarkup(<ActivityList steps={actions} open detailed resourceStatus="generating" onOpenFrame={() => {}} onBroken={() => {}} />)
    for (const action of actions) expect(html).toContain(action.title)
    expect(html).toContain("已取得2张候选画面")
    expect(html).toContain("优先用于完成正文")
    expect(html.match(/role="listitem"/g)).toHaveLength(3)
  })
  it("collects one model call's consecutive observation saves into one expandable row", () => {
    const observations = Array.from({ length: 4 }, (_, index) => ({
      ...step(`observation-${index}`, "tool_execution", 1), phase: "investigate", title: "保存视频画面观察",
      first_seq: 14 + index * 2, last_seq: 15 + index * 2,
      started_at: `2026-10-18T03:23:19.${289 + index * 10}Z`, finished_at: `2026-10-18T03:23:19.${293 + index * 10}Z`,
      outcome: { message: "已保存可见事实与未确认问题，后续可从此继续。", evidence_status: "not_assessed" },
    }))
    const steps = [step("call1", "model_response", 1), ...observations, { ...step("call2", "model_response", 2), status: "running" }]
    expect(activityGroups(steps)).toHaveLength(3)
    const html = renderToStaticMarkup(<ActivityList steps={steps} open detailed resourceStatus="generating" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(html.match(/role="listitem"/g)).toHaveLength(3)
    expect(html).toContain("已保存 4 组画面观察")
    expect(html).toContain("查看 4 组记录")
    expect(html).toContain("第 4 组")
    expect(html.match(/保存视频画面观察/g)).toBeNull()
    expect(observations[0].title).toBe("保存视频画面观察")
    expect(activityGroups(steps)[1].observations).toEqual(observations)
    const compact = renderToStaticMarkup(<ActivityList steps={steps} open={false} detailed={false} freshId="observation-3" resourceStatus="generating" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(compact).toContain("已保存 4 组画面观察")
    expect(compact).toContain('data-new="true"')
  })
  it("keeps different calls, runs, phases and intervening actions separate", () => {
    const observation = { ...step("one", "tool_execution", 1), phase: "investigate", title: "保存视频画面观察" }
    const next = { ...observation, id: "two" }
    for (const other of [{ ...next, model_call: 2 }, { ...next, run_id: "retry" }, { ...next, phase: "compose" }]) {
      expect(activityGroups([observation, other])).toHaveLength(2)
    }
    expect(activityGroups([observation, { ...next, title: "查看新的画面" }, { ...next, id: "three" }])).toHaveLength(3)
    expect(activityGroups([{ ...observation, model_call: undefined }, { ...next, model_call: undefined }])).toHaveLength(2)
  })
  it("updates a grouped observation without increasing its count and preserves failures", () => {
    const saved = { ...step("saved", "tool_execution", 1), phase: "investigate", title: "保存视频画面观察", first_seq: 14, last_seq: 15, started_at: "2026-10-18T03:23:19.289Z", finished_at: "2026-10-18T03:23:19.293Z" }
    const running = { ...saved, id: "pending", first_seq: 16, last_seq: 16, status: "running", started_at: "2026-10-18T03:23:19.302Z", finished_at: null }
    const before = activityGroups([saved, running])[0]
    expect(before.step.status).toBe("running")
    expect(before.step.finished_at).toBeNull()
    expect(before.step.title).toContain("1/2 组已保存")
    const failure = { ...running, status: "failed", last_seq: 17, finished_at: "2026-10-18T03:23:19.306Z", outcome: { message: "保存失败，请稍后重试。", evidence_status: "not_assessed" } }
    const after = activityGroups(mergeActivitySnapshot([saved, running], [failure], { replace: true, sameResource: true, expired: false }))[0]
    expect(after.observations).toHaveLength(2)
    expect(after.step.id).toBe(before.step.id)
    expect(after.step.status).toBe("failed")
    expect(after.step.last_seq).toBe(17)
    expect(after.step.finished_at).toBe(failure.finished_at)
    const html = renderToStaticMarkup(<ActivityList steps={after.observations} open detailed resourceStatus="generating" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(html).toContain('data-status="stopped"')
    expect(html).toContain("保存失败，请稍后重试。")
    expect(html).toContain("1/2 组已保存")
  })
  it("folds an interrupted preparation into one row and keeps the new round", () => {
    const prep = (id: string, title: string, status: string, run: string) => ({ ...step(id, "tool_execution", 0), phase: "prepare_source", title, status, run_id: run })
    const steps = [
      prep("prep-1", "正在准备视频和文字来源", "completed", "run-1"),
      prep("probe-1", "正在校验视频时长与素材", "completed", "run-1"),
      prep("audio-1", "正在转写音频（4:00 / 19:43）", "failed", "run-1"),
      { ...step("retry-1", "retry", 0), phase: "prepare_source", run_id: "run-1", title: "第 1 次来源准备中断，将自动重试", outcome: { message: "下载长时间没有字节进展。", evidence_status: "not_assessed" } },
      prep("prep-2", "正在重新准备视频和文字来源（第 2 次尝试）", "completed", "run-1"),
      prep("probe-2", "正在校验视频时长与素材", "completed", "run-1"),
      prep("early-audio", "正在转写缺少可用字幕的视频", "completed", "run-1"),
      prep("audio-2", "正在转写音频（12:03 / 19:43）", "running", "run-1"),
    ]
    const collapsed = collapseSourceRetries(steps)
    expect(collapsed.map((item) => item.title)).toEqual([
      "第 1 次准备中断",
      "正在重新准备视频和文字来源（第 2 次尝试）",
      "正在校验视频时长与素材",
      "正在转写音频（12:03 / 19:43）",
    ])
    expect(collapsed[0].outcome?.message).toBe("下载长时间没有字节进展。")
    const html = renderToStaticMarkup(<ActivityList steps={steps} open={false} detailed={false} resourceStatus="waiting_source" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(html).toContain("第 1 次准备中断")
    expect(html).toContain("下载长时间没有字节进展。")
    expect(html).toContain("已重新准备视频和文字来源（第 2 次尝试）")
    expect(html).toContain("已校验视频时长与素材")
    expect(html).toContain("视频 12:03 / 19:43")
    expect(html.match(/正在转写音频/g)).toHaveLength(1)
    expect(html).not.toContain("正在准备视频和文字来源")
    expect(html.match(/校验视频时长/g)).toHaveLength(1)
  })
  it("keeps the running step in the journal while it runs, then settles it in place", () => {
    const base = { ...step("video", "tool_execution", 0), phase: "prepare_source", title: "正在获取用于观察的完整视频", outcome: null }
    const running = renderToStaticMarkup(<ActivityList steps={[{ ...base, status: "running", started_at: "2026-10-21T08:00:00Z" }]} open={false} detailed={false} resourceStatus="waiting_source" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(running).toContain("正在获取用于观察的完整视频")
    expect(running).toContain("进行中")
    const done = renderToStaticMarkup(<ActivityList steps={[{ ...base, status: "completed", started_at: "2026-10-21T08:00:00Z", finished_at: "2026-10-21T08:02:49Z" }]} open={false} detailed={false} resourceStatus="waiting_source" onOpenFrame={() => {}} onBroken={() => {}} />)
    expect(done).toContain("已获取用于观察的完整视频")
    expect(done).toContain("用时 02:49")
    expect(done).not.toContain("进行中")
  })
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
