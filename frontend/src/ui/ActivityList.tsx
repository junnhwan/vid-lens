import { useEffect, useRef } from "react"
import type { ActivityStep, Frame } from "../contract/types"
import { formatClock, formatRange } from "../contract/text"
import { stepPresentation } from "../app/labels"
import { activityTitle, parseTranscription, restingTitle, stepSpent } from "../app/progress"
import { prefersReduced } from "./motion"
import { FrameThumb } from "./FrameThumb"
import { activityGroups, collapseSourceRetries } from "../contract/activity-history"

function statusText(status: string) {
  if (status === "done") return "已完成"
  if (status === "active") return "进行中"
  if (status === "stopped") return "已停止"
  return ""
}

export function ActivityList({
  steps,
  resourceStatus,
  detailed,
  open,
  freshId,
  headline,
  onOpenFrame,
  onBroken,
}: {
  steps: ActivityStep[]
  resourceStatus: string
  detailed: boolean
  open: boolean
  freshId?: string | null
  headline?: string
  onOpenFrame: (frames: Frame[], index: number, origin: HTMLElement) => void
  onBroken: (frame: Frame) => void
}) {
  const listRef = useRef<HTMLDivElement>(null)
  const jumpRef = useRef<HTMLButtonElement>(null)
  const actions = activityGroups(collapseSourceRetries(steps))
  const older = actions.slice(0, Math.max(0, actions.length - 3))
  const recent = actions.slice(-3)

  useEffect(() => {
    const list = listRef.current
    if (!list || !open) return
    const near = list.scrollHeight - list.scrollTop - list.clientHeight < 28
    if (near) list.scrollTop = list.scrollHeight
  }, [open, steps.length, actions.at(-1)?.step.last_seq])

  function syncJump() {
    const list = listRef.current
    const jump = jumpRef.current
    if (!list || !jump) return
    const overflow = open && list.scrollHeight - list.clientHeight > 8
    const near = list.scrollHeight - list.scrollTop - list.clientHeight < 28
    jump.hidden = !overflow || near
  }

  function renderStep({ step, sends, observations }: (typeof actions)[number]) {
    const view = stepPresentation(step, resourceStatus)
    const frames = step.frames ?? []
    const transcribed = parseTranscription(step.title)
    const spent = view.status !== "active" ? stepSpent(step) : null
    const model = step.kind === "model_response" || step.kind === "model_submission"
    const named = activityTitle(step)
    const title = step.kind === "retry" || view.status === "active" ? named : restingTitle(named, view.status)
    const repeated = Boolean(headline) && view.status === "active" && title === headline
    const videoLine = transcribed && view.status === "active"
    const outcomeLine = (detailed || model || step.kind === "retry" || view.status === "stopped") && view.outcome && view.status !== "active"
    if (repeated && !model && !spent && !videoLine && !outcomeLine && observations.length <= 1 && !(detailed && (step.source_range || frames.length))) return null
    return (
      <div className="step" role="listitem" key={step.id} data-kind={step.kind} data-status={view.status} data-new={step.id === freshId || observations.some((item) => item.id === freshId) ? "true" : undefined}>
        <span className="mark" aria-hidden="true" />
        <div>
          {repeated && !model && !spent ? (
            <span className="sr-only">{statusText(view.status)}</span>
          ) : (
            <div className="step-label">
              {repeated ? null : title}
              {model ? <span className="step-spent">第 {step.model_call ?? "?"} 次模型调用{sends.length ? ` · 发送 ${sends.length} 次` : ""}</span> : null}
              {spent ? <span className="step-spent">用时 {spent}</span> : null}
              <span className="sr-only">{statusText(view.status)}</span>
            </div>
          )}
          {transcribed && view.status === "active" ? <div className="step-range">视频 {transcribed.done} / {transcribed.total}</div> : null}
          {model && sends.length ? <details className="model-sends"><summary>发送详情</summary>{sends.map((send) => <p key={send.id}>{send.title} · {stepPresentation(send, resourceStatus).status === "active" ? "等待响应" : stepPresentation(send, resourceStatus).outcome || "已结束"}{stepSpent(send) ? ` · ${stepSpent(send)}` : ""}</p>)}</details> : null}
          {(detailed || model || step.kind === "retry" || view.status === "stopped") && view.outcome && view.status !== "active" ? <div className="step-outcome">{view.outcome}</div> : null}
          {observations.length > 1 ? (
            <details className="observation-details">
              <summary>查看 {observations.length} 组记录</summary>
              <ol>
                {observations.map((item, index) => {
                  const detail = stepPresentation(item, resourceStatus)
                  const duration = detail.status !== "active" ? stepSpent(item) : null
                  return (
                    <li key={item.id}>
                      第 {index + 1} 组 · {statusText(detail.status)}{duration ? ` · 用时 ${duration}` : ""}
                      {detail.status === "stopped" && detail.outcome ? <div>{detail.outcome}</div> : null}
                      {item.source_range ? <div>{formatRange(item.source_range)}</div> : null}
                    </li>
                  )
                })}
              </ol>
            </details>
          ) : null}
          {detailed && step.source_range ? <div className="step-range">{formatRange(step.source_range)}</div> : null}
          {frames.length ? (
            <div className="mini-row" aria-label="画面证据">
              {frames.map((frame, index) => (
                <button
                  type="button"
                  className="mini-shot"
                  key={frame.id}
                  onClick={(event) => onOpenFrame(frames, index, event.currentTarget.querySelector(".shot-host") || event.currentTarget)}
                >
                  <span className="shot-host"><FrameThumb frame={frame} variant="thumbnail" onBroken={onBroken} /></span>
                  <span className="mini-time">{formatClock(frame.time_ms)}</span>
                </button>
              ))}
            </div>
          ) : null}
        </div>
      </div>
    )
  }

  return (
    <>
      <div id={detailed ? undefined : "run-steps"} className={detailed ? "steps gen-steps" : "steps"} role="list" ref={listRef} onScroll={syncJump} data-open={open ? "true" : undefined}>
        {older.length ? (
          <div className="disclose" data-open={open ? "true" : "false"} role="presentation">
            <div className="disclose-inner" role="presentation">
              {older.map(renderStep)}
            </div>
          </div>
        ) : null}
        {recent.map(renderStep)}
      </div>
      {detailed ? null : (
        <button type="button" className="jump-latest" ref={jumpRef} hidden onClick={() => listRef.current?.scrollTo({ top: listRef.current.scrollHeight, behavior: prefersReduced() ? "auto" : "smooth" })}>
          回到最新
        </button>
      )}
    </>
  )
}
