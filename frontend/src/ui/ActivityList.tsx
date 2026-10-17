import { useEffect, useRef } from "react"
import type { ActivityStep, Frame } from "../contract/types"
import { formatClock, formatRange } from "../contract/text"
import { stepPresentation } from "../app/labels"
import { activityTitle, parseTranscription, stepSpent } from "../app/progress"
import { prefersReduced } from "./motion"
import { FrameThumb } from "./FrameThumb"

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
  onOpenFrame,
  onBroken,
}: {
  steps: ActivityStep[]
  resourceStatus: string
  detailed: boolean
  open: boolean
  freshId?: string | null
  onOpenFrame: (frames: Frame[], index: number, origin: HTMLElement) => void
  onBroken: (frame: Frame) => void
}) {
  const listRef = useRef<HTMLDivElement>(null)
  const jumpRef = useRef<HTMLButtonElement>(null)
  const actions = steps.filter((step) => step.kind !== "execution_plan")
  const visible = open ? actions : actions.slice(-3)

  useEffect(() => {
    const list = listRef.current
    if (!list || !open) return
    const near = list.scrollHeight - list.scrollTop - list.clientHeight < 28
    if (near) list.scrollTop = list.scrollHeight
  }, [open, steps.length, visible.at(-1)?.last_seq])

  function syncJump() {
    const list = listRef.current
    const jump = jumpRef.current
    if (!list || !jump) return
    const overflow = open && list.scrollHeight - list.clientHeight > 8
    const near = list.scrollHeight - list.scrollTop - list.clientHeight < 28
    jump.hidden = !overflow || near
  }

  return (
    <>
      <div id={detailed ? undefined : "run-steps"} className={detailed ? "steps gen-steps" : "steps"} role="list" ref={listRef} onScroll={syncJump} data-open={open ? "true" : undefined}>
        {visible.map((step) => {
          const view = stepPresentation(step, resourceStatus)
          const frames = step.frames ?? []
          const transcribed = parseTranscription(step.title)
          const spent = view.status !== "active" ? stepSpent(step) : null
          const model = step.kind === "model_response" || step.kind === "model_submission"
          return (
            <div className="step" role="listitem" key={step.id} data-kind={step.kind} data-status={view.status} data-new={step.id === freshId ? "true" : undefined}>
              <span className="mark" aria-hidden="true" />
              <div>
                <div className="step-label">
                  {activityTitle(step)}
                  {spent ? <span className="step-spent">用时 {spent}</span> : null}
                  <span className="sr-only">{statusText(view.status)}</span>
                </div>
                {transcribed ? <div className="step-range">{transcribed.done} / {transcribed.total}</div> : null}
                {model && view.status === "active" && step.purpose ? <div className="step-outcome">{step.purpose}</div> : null}
                {(detailed || model) && view.outcome && view.status !== "active" ? <div className="step-outcome">{view.outcome}</div> : null}
                {detailed && step.source_range ? <div className="step-range">{formatRange(step.source_range)}</div> : null}
                {detailed && frames.length ? (
                  <div className="mini-row">
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
        })}
      </div>
      {detailed ? null : (
        <button type="button" className="jump-latest" ref={jumpRef} hidden onClick={() => listRef.current?.scrollTo({ top: listRef.current.scrollHeight, behavior: prefersReduced() ? "auto" : "smooth" })}>
          回到最新
        </button>
      )}
    </>
  )
}
