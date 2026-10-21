import { useEffect, useRef } from "react"
import type { ActivityStep, Frame } from "../contract/types"
import { formatClock, formatRange, formatTokens, tokenUsageLabel } from "../contract/text"
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

function sendName(title: string, index: number) {
  const match = title.match(/发送\s*(\d+)/)
  return match ? `发送 ${match[1]}` : title || `第 ${index + 1} 次发送`
}

export function ActivityList({
  steps,
  resourceStatus,
  detailed,
  open,
  freshId,
  settledId,
  onOpenFrame,
  onBroken,
}: {
  steps: ActivityStep[]
  resourceStatus: string
  detailed: boolean
  open: boolean
  freshId?: string | null
  settledId?: string | null
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
    const callUsage = step.kind === "model_response" ? step.usage : null
    const named = activityTitle(step)
    const title = step.kind === "retry" || view.status === "active" ? named : restingTitle(named, view.status)
    const live = view.status === "active"
    return (
      <div className="step" role="listitem" key={step.id} data-kind={step.kind} data-status={view.status} data-new={step.id === freshId || observations.some((item) => item.id === freshId) ? "true" : undefined} data-settled={step.id === settledId && view.status === "done" ? "true" : undefined}>
        <span className="mark" aria-hidden="true" />
        <div>
          <div className="step-label">
            <span className="step-title">{title}</span>
            {model ? <span className="step-spent">第 {step.model_call ?? "?"} 次模型调用{sends.length ? ` · 发送 ${sends.length} 次` : ""}</span> : null}
            {spent ? <span className="step-spent">用时 {spent}</span> : null}
            {live ? <span className="step-live" aria-hidden="true"><i />进行中</span> : null}
            {callUsage ? (
              <span className="step-usage" title={tokenUsageLabel(callUsage)}>
                <span className="sr-only">{tokenUsageLabel(callUsage)}</span>
                <span aria-hidden="true"><span className="usage-label">用量</span>入 <b>{formatTokens(callUsage.input)}</b> · 出 <b>{formatTokens(callUsage.output)}</b></span>
              </span>
            ) : null}
            <span className="sr-only">{statusText(view.status)}</span>
          </div>
          {transcribed && view.status === "active" ? <div className="step-range">视频 {transcribed.done} / {transcribed.total}</div> : null}
          {model && sends.length ? (
            <details className="model-sends">
              <summary>
                <span className="journal-disclosure-dot" aria-hidden="true" />
                <span>发送详情</span>
                <span className="journal-disclosure-count">{sends.length} 次</span>
                <span className="journal-disclosure-caret" aria-hidden="true" />
              </summary>
              <ol className="send-list">
                {sends.map((send, index) => {
                  const detail = stepPresentation(send, resourceStatus)
                  const duration = detail.status !== "active" ? stepSpent(send) : null
                  const usage = send.usage
                  const meta = Boolean(usage || duration || detail.status === "active")
                  return (
                    <li key={send.id} data-status={detail.status}>
                      <span className="send-index" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
                      <span className="send-copy">
                        <span className="send-title">{sendName(send.title, index)}</span>
                        <span className="send-outcome">{detail.status === "active" ? "等待响应" : detail.outcome || "已结束"}</span>
                      </span>
                      {meta ? (
                        <span className="send-meta">
                          {usage ? (
                            <span className="send-usage" title={tokenUsageLabel(usage)}>
                              <span className="sr-only">{tokenUsageLabel(usage)}</span>
                              <span aria-hidden="true">入 <b>{formatTokens(usage.input)}</b> · 出 <b>{formatTokens(usage.output)}</b></span>
                            </span>
                          ) : null}
                          {detail.status === "active" ? (
                            <span className="send-live"><i aria-hidden="true" />进行中</span>
                          ) : duration ? (
                            <span className="send-time">{duration}</span>
                          ) : null}
                        </span>
                      ) : null}
                    </li>
                  )
                })}
              </ol>
            </details>
          ) : null}
          {(detailed || model || step.kind === "retry" || view.status === "stopped") && view.outcome && view.status !== "active" ? <div className="step-outcome">{view.outcome}</div> : null}
          {observations.length > 1 ? (
            <details className="observation-details">
              <summary>
                <span className="journal-disclosure-dot" aria-hidden="true" />
                <span>查看 {observations.length} 组记录</span>
                <span className="journal-disclosure-caret" aria-hidden="true" />
              </summary>
              <ol className="observation-list">
                {observations.map((item, index) => {
                  const detail = stepPresentation(item, resourceStatus)
                  const duration = detail.status !== "active" ? stepSpent(item) : null
                  return (
                    <li key={item.id} data-status={detail.status}>
                      <span className="observation-index">第 {index + 1} 组</span>
                      <span className="observation-state">{statusText(detail.status)}{duration ? ` · ${duration}` : ""}</span>
                      {detail.status === "stopped" && detail.outcome ? <span className="observation-note">{detail.outcome}</span> : null}
                      {item.source_range ? <span className="observation-range">{formatRange(item.source_range)}</span> : null}
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
