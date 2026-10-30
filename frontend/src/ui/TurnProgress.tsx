import { useEffect, useRef, useState } from "react"
import type { ActivityStep, TurnView } from "../contract/types"
import { formatClock } from "../contract/text"
import { isTerminalStatus } from "../contract/stream"
import { describeTurn } from "../app/labels"

export function turnProgress(view: TurnView, current: ActivityStep | null, now: number, stopping = false) {
  if (stopping || view.turn.status === "cancel_requested") return { title: "正在停止回答", detail: "已请求停止，正在结束当前处理。" }
  const status = view.execution?.status || view.turn.status
  if (status === "queued") return { title: "正在等待处理", detail: "问题已收到，取得处理名额后开始。" }
  if (status === "retry_wait") return { title: "正在等待恢复", detail: view.execution?.retry_reason || "已保存进度，稍后继续。" }
  if (status === "waiting_dependency") return { title: "正在恢复视频画面", detail: "需要先取回视频，完成后继续回答。" }
  const receipt = current?.model_progress
  const review = current?.phase === "validate" || view.execution?.phase === "validate"
  const stage = review ? "正在核对回答依据" : "正在分析问题与视频依据"
  if (current?.kind === "model_response" && current.status === "running" && receipt) {
    const stamp = Date.parse(receipt.observed_at)
    const age = Number.isFinite(stamp) ? Math.max(0, now - stamp) : Number.POSITIVE_INFINITY
    if (receipt.state === "waiting") return { title: `${stage} · 等待模型响应`, detail: "请求已发送，正在等待模型开始返回。" }
    if (age > 15000) return { title: `${stage} · 等待后续响应`, detail: Number.isFinite(age) ? `最近一次模型活动在 ${Math.floor(age / 1000)} 秒前。可以继续等待或停止回答。` : "正在等待新的模型活动。可以停止回答。" }
    if (receipt.state === "thinking") return { title: `${stage} · 模型思考中`, detail: review ? "已收到模型思考片段，依据核对尚未完成；完成后显示回答。" : "已收到模型思考片段，正在处理当前问题。" }
    return { title: `${stage} · 正在接收响应`, detail: "模型正在返回响应，收到完整结果后继续处理。" }
  }
  return { title: describeTurn(view, current).title || stage, detail: review ? "回答正在核对，完成后显示。" : "复杂问题可能需要较长时间，可以随时停止。" }
}

// Anchor to server/receipt time and advance monotonically. A client's wall-clock
// offset must not make fresh stream activity look minutes old.
export function TurnProgress({ view, current, stopping = false }: { view: TurnView; current: ActivityStep | null; stopping?: boolean }) {
  const live = !isTerminalStatus(view.turn.status)
  const [, tick] = useState(0)
  const anchor = useRef({ key: "", server: 0, local: performance.now() })
  const local = performance.now()
  const key = `${view.turn.id}:${view.server_now}:${current?.model_progress?.observed_at}`
  if (anchor.current.key !== key) {
    const timestamps = [view.server_now, current?.model_progress?.observed_at].map(value => Date.parse(value || "")).filter(Number.isFinite)
    const previous = anchor.current.key.startsWith(`${view.turn.id}:`) ? anchor.current.server + local - anchor.current.local : 0
    anchor.current = { key, server: Math.max(previous, ...timestamps, timestamps.length ? 0 : Date.now()), local }
  }
  useEffect(() => {
    if (!live) return
    const timer = window.setInterval(() => tick(value => value + 1), 1000)
    return () => window.clearInterval(timer)
  }, [view.turn.id, live])
  if (!live) return null
  const now = anchor.current.server + local - anchor.current.local
  const copy = turnProgress(view, current, now, stopping)
  const created = Date.parse(view.turn.created_at)
  return <div className="turn-progress">
    <p className="turn-progress-title" role="status">{copy.title}</p>
    <p className="turn-progress-detail">{copy.detail}</p>
    {Number.isFinite(created) ? <p className="turn-progress-clock">本轮已等待 {formatClock(Math.max(0, now - created))}</p> : null}
  </div>
}
