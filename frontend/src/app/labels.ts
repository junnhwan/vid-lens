import type { ActivityStep, ExecutionView, RequestView, TurnView } from "../contract/types"
import { clipText, formatRange } from "../contract/text"
import { isTerminalStatus } from "../contract/stream"
import { displayStepTitle, parseTranscription } from "./progress"

const phaseTitle: Record<string, string> = {
  prepare_source: "正在准备视频",
  overview: "正在梳理整条视频",
  investigate: "正在查看画面",
  compose: "正在组织图文文章",
  validate: "正在核对文章",
  publish: "正在发布文章",
  answer: "正在组织回答",
}

function when(iso: string | null | undefined): string {
  if (!iso) return ""
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ""
  return date.toLocaleString("zh-CN", { hour12: false, month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })
}

export function starterPrompts(title: string | undefined, sectionTitle: string | undefined): string[] {
  const main = title ? `《${title}》主要在讲什么？` : "这条视频主要在讲什么？"
  const focus = sectionTitle ? clipText(sectionTitle, 18) : ""
  const second = focus ? `「${focus}」里最关键的一点是什么？` : "最值得记住的结论是什么？"
  return [main, second, "哪些说法可以直接回到画面核对？"]
}

export function describeRequest(view: RequestView | null, current: ActivityStep | null, holding: boolean, reconnecting: boolean) {
  if (!view) {
    return { kicker: reconnecting ? "正在重新连接" : "正在连接", title: "正在读取进度", purpose: "", range: "", busy: true }
  }
  if (holding) {
    return { kicker: "理解已完成", title: "已整理成可阅读的文章", purpose: "过程先留在这里。", range: "", busy: false }
  }
  const status = view.request.status
  const execution = view.execution
  let kicker = "正在理解视频"
  if (reconnecting) kicker = "正在重新连接"
  else if (status === "failed") kicker = "未能完成"
  else if (status === "canceled") kicker = "已停止"
  else if (execution?.status === "retry_wait") kicker = "将自动重试"
  else if (execution?.status === "queued") kicker = "正在排队"
  else if (execution?.status === "waiting_dependency") kicker = "正在恢复画面"
  else if (status === "waiting_source") kicker = "正在准备来源"

  if (status === "failed") {
    return { kicker, title: view.failure?.message || "这次没能完成", purpose: "", range: "", busy: false }
  }
  if (status === "canceled") {
    return { kicker, title: "已停止理解", purpose: "停在已经取得的内容，文章还没有生成。", range: "", busy: false }
  }

  if (execution?.status === "retry_wait") {
    if (execution.kind === "article.generate") {
      const attempt = execution.retry_attempt ? `第 ${execution.retry_attempt} 次自动恢复。` : ""
      return { kicker: "正在自动恢复", title: "正在等待继续生成", purpose: [attempt, execution.retry_reason, fallbackPurpose(execution), "来源与已保存进度会保留，无需手动重试。"].filter(Boolean).join(" "), range: "", busy: true }
    }
    return { kicker, title: fallbackTitle(execution), purpose: fallbackPurpose(execution), range: "", busy: true }
  }
  if (current) {
    const model = current.kind === "model_response"
    const transcribing = parseTranscription(current.title) != null || current.title.startsWith("正在转写")
    return { kicker: kicker === "正在准备来源" && transcribing ? "正在转写" : kicker, title: displayStepTitle(model ? current.purpose || current.title : current.title), purpose: model ? `第 ${current.model_call || ""} 次模型调用` : current.purpose || "", range: formatRange(current.source_range), busy: true }
  }
  return { kicker, title: fallbackTitle(execution), purpose: fallbackPurpose(execution), range: "", busy: !isTerminalStatus(status) }
}

function fallbackTitle(execution: ExecutionView | null): string {
  if (!execution) return "正在处理"
  if (execution.status === "queued" && execution.wait_reason === "capacity") return "正在等待处理名额"
  if (execution.status === "queued") return "正在排队"
  if (execution.status === "retry_wait") return "将自动重试"
  if (execution.status === "waiting_dependency") return "正在恢复原来的视频画面"
  if (execution.status === "cancel_requested") return "正在停止"
  if (execution.phase && phaseTitle[execution.phase]) return phaseTitle[execution.phase]
  return "正在处理"
}

function fallbackPurpose(execution: ExecutionView | null): string {
  const stamp = when(execution?.retry_at)
  if (execution?.status === "retry_wait" && stamp) return `已安排在 ${stamp} 之后继续。`
  if (execution?.status === "retry_wait") return "会自动再试一次，还没有确定开始时间。"
  if (execution?.wait_reason === "media_restore") return "需要先把原来的视频准备回来。"
  return ""
}

export function describeTurn(view: TurnView, current: ActivityStep | null) {
  const status = view.turn.status
  if (status === "cancel_requested") return { title: "正在停止", summary: "正在停止", open: true }
  if (status === "failed") return { title: view.failure?.message || "没能完成回答", summary: "没能完成", open: false }
  if (status === "canceled") return { title: "已停止", summary: "已停止", open: false }
  if (status === "completed") return { title: "", summary: `查看回答过程 · ${view.activity.model_calls ?? 0} 次模型调用`, open: false }
  if (current) return { title: current.purpose || current.title, summary: "", open: true }
  return { title: fallbackTitle(view.execution), summary: "", open: true }
}

export function stepPresentation(step: ActivityStep, resourceStatus: string): { status: "active" | "done" | "stopped"; outcome: string } {
  const unfinished = step.status === "running" || step.status === "queued"
  if (resourceStatus === "canceled" && unfinished) {
    return { status: "stopped", outcome: step.outcome?.message || "取消时尚未完成" }
  }
  if (resourceStatus === "failed" && unfinished) {
    return { status: "stopped", outcome: step.outcome?.message || "处理失败时尚未完成" }
  }
  if (step.status === "completed") return { status: "done", outcome: step.outcome?.message || "" }
  if (step.status === "failed" || step.status === "canceled") return { status: "stopped", outcome: step.outcome?.message || "" }
  return { status: "active", outcome: "" }
}

export function answerKicker(kind: string | undefined): string {
  if (kind === "insufficient_evidence") return "依据不足"
  if (kind === "out_of_scope") return "超出这条视频"
  return ""
}
