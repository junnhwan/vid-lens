import { describe, expect, it } from "vitest"
import type { ActivityStep, RequestView } from "../contract/types"
import { describeRequest, stepPresentation, starterPrompts } from "./labels"

describe("terminal activity", () => {
  const step = { status: "running" } as ActivityStep

  it("stops presenting unfinished work as active after failure", () => {
    expect(stepPresentation(step, "failed")).toEqual({ status: "stopped", outcome: "处理失败时尚未完成" })
  })

  it("keeps completed work completed after a later failure", () => {
    expect(stepPresentation({ ...step, status: "completed" }, "failed").status).toBe("done")
    expect(stepPresentation(step, "waiting_source").status).toBe("active")
  })

  it("offers three questions that start from the article", () => {
    expect(starterPrompts("沙箱", "隔离边界")).toEqual([
      "《沙箱》主要在讲什么？",
      "「隔离边界」里最关键的一点是什么？",
      "哪些说法可以直接回到画面核对？",
    ])
    expect(starterPrompts(undefined, undefined)[0]).toBe("这条视频主要在讲什么？")
  })
})

describe("source retries", () => {

  it("explains automatic article recovery and keeps failed requests terminal", () => {
    const view = { request: { status: "generating" }, execution: { kind: "article.generate", status: "retry_wait", retry_attempt: 2, retry_reason: "模型响应未完整结束", retry_at: "2026-10-18T08:00:00Z" } } as RequestView
    const description = describeRequest(view, null, false, false)
    expect(description.kicker).toBe("正在自动恢复")
    expect(description.purpose).toContain("第 2 次自动恢复")
    expect(description.purpose).toContain("模型响应未完整结束")
    expect(description.purpose).toContain("无需手动重试")
    const failed = describeRequest({ ...view, request: { ...view.request, status: "failed" }, failure: { code: "PROVIDER_UNAVAILABLE", message: "自动恢复额度已用完", can_retry: true, retry_at: null } }, null, false, false)
    expect(failed.busy).toBe(false)
    expect(failed.title).toBe("自动恢复额度已用完")
    expect(failed.purpose).not.toContain("无需手动重试")
  })
  it("shows the retry wait instead of an old running download", () => {
    const view = { request: { status: "waiting_source" }, execution: { status: "retry_wait", retry_at: null } } as RequestView
    const current = { status: "running", title: "正在获取用于观察的完整视频" } as ActivityStep
    const description = describeRequest(view, current, false, false)
    expect(description.kicker).toBe("将自动重试")
    expect(description.title).toBe("将自动重试")
    expect(description.purpose).toContain("会自动再试一次")
  })
  it("names transcription instead of source preparation", () => {
    const view = { request: { status: "waiting_source" }, execution: { status: "running" } } as RequestView
    const current = { status: "running", title: "正在转写音频（12:03 / 19:43）", kind: "tool_execution" } as ActivityStep
    const description = describeRequest(view, current, false, false)
    expect(description.kicker).toBe("正在转写")
    expect(description.title).toBe("正在转写音频")
    expect(description.purpose).toBe("")
  })
})
