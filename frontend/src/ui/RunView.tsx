import { useEffect, useRef, useState, type Ref } from "react"
import type { ActivityStep, Frame, RequestView } from "../contract/types"
import { formatClock, formatDuration, platformLabel, provenanceNote } from "../contract/text"
import { describeRequest } from "../app/labels"
import { animateTitle, prefersReduced } from "./motion"
import { ActivityList } from "./ActivityList"
import { FrameThumb } from "./FrameThumb"

export function RunView({
  view,
  steps,
  holding,
  reconnecting,
  historyOpen,
  articleError,
  active,
  viewRef,
  onToggleHistory,
  onStop,
  onRetry,
  onRead,
  onRetryArticle,
  onOpenFrame,
  onBroken,
}: {
  view: RequestView | null
  steps: ActivityStep[]
  holding: boolean
  reconnecting: boolean
  historyOpen: boolean
  articleError: string
  active: boolean
  viewRef?: Ref<HTMLElement>
  onToggleHistory: () => void
  onStop: () => void
  onRetry: () => void
  onRead: () => void
  onRetryArticle: () => void
  onOpenFrame: (frames: Frame[], index: number, origin: HTMLElement) => void
  onBroken: (frame: Frame) => void
}) {
  const titleRef = useRef<HTMLHeadingElement>(null)
  const listBox = useRef<HTMLDivElement>(null)
  const [freshId, setFreshId] = useState<string | null>(null)
  const seen = useRef(new Set<string>())
  const current = steps.find((step) => step.id === view?.activity.current_step_id) ?? null
  const copy = describeRequest(view, current, holding, reconnecting)
  const source = view?.source
  const poster = steps.flatMap((step) => step.frames ?? []).at(0) ?? null
  const shown: Frame[] = []
  for (const step of steps) {
    for (const frame of step.frames ?? []) {
      if (!shown.some((item) => item.id === frame.id)) shown.push(frame)
    }
  }
  const meta = [platformLabel(source?.platform), formatDuration(source?.duration_ms), source?.part?.label].filter(Boolean).join(" · ")
  const note = source
    ? view?.request.reuse === "published_article"
      ? "已有文章，直接打开。"
      : provenanceNote(source.text_provenance?.origin, source.text_provenance?.coverage)
    : "正在确认这条链接。"

  useEffect(() => {
    if (!active) return
    const title = titleRef.current
    if (!title) return
    animateTitle(title, copy.title)
    document.title = `${copy.title} · VidLens`
  }, [active, copy.title])

  useEffect(() => {
    const newest = steps.at(-1)
    if (!newest || seen.current.has(newest.id)) return
    seen.current.add(newest.id)
    setFreshId(newest.id)
    const timer = window.setTimeout(() => setFreshId(null), 260)
    return () => window.clearTimeout(timer)
  }, [steps])

  function toggle() {
    const list = listBox.current
    const from = list?.getBoundingClientRect().height ?? 0
    onToggleHistory()
    if (!list || prefersReduced()) return
    requestAnimationFrame(() => {
      const to = list.getBoundingClientRect().height
      const anim = list.animate([{ height: `${from}px` }, { height: `${to}px` }], { duration: 240, easing: "cubic-bezier(0.23, 1, 0.32, 1)" })
      anim.onfinish = () => anim.cancel()
    })
  }

  return (
    <section className="view-run" ref={viewRef}>
      <div className="source">
        <div className="source-still" aria-hidden="true">
          {poster ? <span className="shot-host"><FrameThumb frame={poster} variant="thumbnail" onBroken={onBroken} /></span> : null}
        </div>
        <div>
          <p className="source-title">{source?.title || "正在确认视频"}</p>
          <p className="source-meta">{meta || view?.request.input_url || ""}</p>
          {note ? <p className="source-note">{note}</p> : null}
        </div>
      </div>
      <div className="activity" aria-busy={copy.busy}>
        <p className="kicker"><span className="pulse" hidden={!copy.busy} aria-hidden="true" /><span>{copy.kicker}</span></p>
        <h1 className="activity-title" ref={titleRef} />
        <p className="purpose">{copy.purpose}</p>
        <div className="run-actions">
          <p className="range" hidden={!copy.range}>{copy.range}</p>
          {view?.actions.can_cancel ? <button type="button" className="btn stop" onClick={onStop}>停止</button> : null}
          {view?.actions.can_retry ? <button type="button" className="btn primary" onClick={onRetry}>重新理解</button> : null}
        </div>
        <div className="ready-banner" hidden={!holding}>
          <p>文章已就绪</p>
          <button type="button" className="btn primary" onClick={onRead}>开始阅读</button>
        </div>
        {articleError ? <p className="form-error">{articleError} <button type="button" className="btn quiet" onClick={onRetryArticle}>重新读取</button></p> : null}
      </div>
      <div className="frames">
        {shown.map((frame, index) => (
          <button
            type="button"
            className="frame-card"
            key={frame.id}
            data-new={freshId && steps.some((step) => (step.frames ?? []).some((item) => item.id === frame.id) && step.id === freshId) ? "true" : undefined}
            onClick={(event) => onOpenFrame(shown, index, event.currentTarget.querySelector(".shot-host") || event.currentTarget)}
          >
            <span className="shot-host"><FrameThumb frame={frame} variant="thumbnail" onBroken={onBroken} /></span>
            <span className="frame-cap">{formatClock(frame.time_ms)}  {frame.caption}</span>
          </button>
        ))}
      </div>
      <div className="history">
        <div ref={listBox}>
          <ActivityList
            steps={steps}
            resourceStatus={view?.request.status || ""}
            detailed={historyOpen}
            open={historyOpen}
            freshId={freshId}
            onOpenFrame={onOpenFrame}
            onBroken={onBroken}
          />
        </div>
        <div className="history-bar">
          <button type="button" className="disclosure" aria-expanded={historyOpen} onClick={toggle}>{historyOpen ? "收起过程" : "查看全部过程"}</button>
        </div>
        {view?.activity.history_status === "expired" ? <p className="history-note">更早的过程已经过期。</p> : null}
        {view?.activity.history_status === "partial" ? <p className="history-note">一部分过程记录已经清理。</p> : null}
      </div>
      <p className="leave-note">离开或刷新不会停止后台工作。已看到的步骤会在刷新后恢复。</p>
    </section>
  )
}
