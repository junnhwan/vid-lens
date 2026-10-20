import { useEffect, useRef, useState, type Ref } from "react"
import type { ActivityStep, Frame, Range, RequestView } from "../contract/types"
import { formatClock, formatDuration, platformLabel, provenanceNote } from "../contract/text"
import { describeRequest } from "../app/labels"
import { PHASES, parseTranscription, phaseMarks, quietNote, waitClock, anchoredElapsed, serverElapsed, type PhaseMark } from "../app/progress"
import { animateTitle } from "./motion"
import { ActivityList } from "./ActivityList"
import { FrameThumb } from "./FrameThumb"
import { ModelIdentity } from "./ModelIdentity"
import { ExecutionPlan } from "./ExecutionPlan"
import { ShareControl } from "./ShareControl"

export function RunView({
  view,
  csrf,
  steps,
  holding,
  reconnecting,
  historyOpen,
  historyMessage,
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
  csrf?: string
  steps: ActivityStep[]
  holding: boolean
  reconnecting: boolean
  historyOpen: boolean
  historyMessage?: string
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
  const acknowledged = useRef(0)
  const [freshId, setFreshId] = useState<string | null>(null)
  const [framesOpen, setFramesOpen] = useState(false)
  const seen = useRef(new Set<string>())
  const current = steps.find((step) => step.id === view?.activity.current_step_id) ?? null
  const copy = describeRequest(view, current, holding, reconnecting)
  const stopped = view?.request.status === "failed" || view?.request.status === "canceled"
  const source = view?.source
  const poster = steps.flatMap((step) => step.frames ?? []).at(0) ?? null
  const shown: Frame[] = []
  for (const step of steps) {
    for (const frame of step.frames ?? []) {
      if (!shown.some((item) => item.id === frame.id)) shown.push(frame)
    }
  }
  const latest = shown.at(-1) ?? null
  const strip = framesOpen ? shown.slice(0, -1) : shown
  const transcribed = current && copy.title === "正在转写音频" ? parseTranscription(current.title) : null
  const phaseId = view?.execution?.phase || current?.phase || (stopped ? steps.at(-1)?.phase : null) || null
  const marks = phaseMarks(phaseId, holding || view?.request.status === "completed" ? "done" : stopped ? "stopped" : "running")
  const clock = useWaitClock(view?.request.created_at, view?.request.updated_at, active && copy.busy, view?.server_now)
  const quiet = copy.busy ? quietNote(phaseId, view?.execution?.status) : ""
  const motion = waitMotion(copy.busy, phaseId, current, Boolean(transcribed))
  const meta = [platformLabel(source?.platform), formatDuration(source?.duration_ms), source?.part?.label].filter(Boolean).join(" · ")
  const note = source
    ? view?.request.reuse === "published_article"
      ? "已有文章，直接打开。"
      : provenanceNote(source.text_provenance?.origin, source.text_provenance?.coverage)
    : "正在确认这条链接。"
  const unseen = !framesOpen && shown.length > acknowledged.current

  useEffect(() => {
    if (!active) return
    const title = titleRef.current
    if (!title) return
    animateTitle(title, copy.title)
    document.title = `${copy.title} · VidLens`
  }, [active, copy.title])

  useEffect(() => {
    if (seen.current.size === 0) {
      for (const step of steps) seen.current.add(step.id)
      return
    }
    const newest = steps.at(-1)
    if (!newest || seen.current.has(newest.id)) return
    seen.current.add(newest.id)
    setFreshId(newest.id)
    const timer = window.setTimeout(() => setFreshId(null), 260)
    return () => window.clearTimeout(timer)
  }, [steps])

  function toggleHistory() {
    onToggleHistory()
  }

  function toggleFrames() {
    if (!framesOpen) acknowledged.current = shown.length
    setFramesOpen((open) => !open)
  }

  function openAt(frame: Frame, origin: HTMLElement) {
    const index = shown.findIndex((item) => item.id === frame.id)
    onOpenFrame(shown, index < 0 ? 0 : index, origin)
  }

  return (
    <section className="view-run" ref={viewRef}>
      <div className="source">
        {poster ? (
          <div className="source-still" aria-hidden="true">
            <span className="shot-host">
              <FrameThumb frame={poster} variant="thumbnail" onBroken={onBroken} />
            </span>
          </div>
        ) : null}
        <div>
          <p className="source-title">{source?.title || (stopped ? "视频处理未完成" : "正在确认视频")}</p>
          <p className="source-meta">{meta || view?.request.input_url || ""}</p>
          {note ? <p className="source-note">{note}</p> : null}
          <ModelIdentity model={view?.request.generation_model} purpose={view?.request.purpose} detailLevel={view?.request.detail_level} />
        </div>
      </div>
      <div className="activity" aria-busy={copy.busy} data-motion={motion || undefined}>
        <div className="run-pin">
        <p className="kicker">
          <span className="pulse" hidden={!copy.busy} aria-hidden="true" />
          <span>{copy.kicker}</span>
          {clock ? (
            <span className="elapsed">
              {copy.busy ? "已进行" : "用时"} {clock}
            </span>
          ) : null}
        </p>
        <h1 className="activity-title" ref={titleRef} />
        {copy.purpose ? <p className="purpose">{copy.purpose}</p> : null}
        {view?.failure?.diagnostic_id ? <p className="meta">错误编号：{view.failure.diagnostic_id}</p> : null}
        {quiet ? <p className="quiet-note">{quiet}</p> : null}
        {transcribed ? <TranscriptionMeter done={transcribed.done} total={transcribed.total} ratio={transcribed.ratio} /> : null}
        <PhaseSpine marks={marks} />
        <div className="run-actions">
          {csrf ? <ShareControl requestId={view?.request.id} csrf={csrf} /> : null}
          <p className="range" hidden={!copy.range}>
            {copy.range}
          </p>
          {view?.actions.can_cancel ? (
            <button type="button" className="btn stop" onClick={onStop}>
              停止
            </button>
          ) : null}
          {view?.actions.can_retry ? (
            <button type="button" className="btn primary" onClick={onRetry}>
              {view.activity.model_calls ? "从已保存进度继续" : "重新理解"}
            </button>
          ) : null}
        </div>
        <div className="ready-banner" hidden={!holding}>
          <p>文章已就绪</p>
          <button type="button" className="btn primary" onClick={onRead}>
            开始阅读
          </button>
        </div>
        {articleError ? (
          <p className="form-error">
            {articleError}{" "}
            <button type="button" className="btn quiet" onClick={onRetryArticle}>
              重新读取
            </button>
          </p>
        ) : null}
        </div>
      </div>
      <ExecutionPlan activity={view?.activity} completed={view?.request.status === "completed"} />
      {shown.length ? (
        <div className="frame-panel">
          <Coverage duration={source?.duration_ms} ranges={steps.flatMap((step) => (step.source_range ? [step.source_range] : []))} />
          <div className="frames frame-drawer" data-open={framesOpen ? "true" : "false"}>
              <div className="disclose" data-open={framesOpen ? "true" : "false"}>
                <div className="disclose-inner">
                  {latest ? (
                    <FrameButton
                      frame={latest}
                      hero
                      carry
                      fresh={isFresh(latest, steps, freshId)}
                      onOpen={(origin) => openAt(latest, origin)}
                      onBroken={onBroken}
                    />
                  ) : null}
                </div>
              </div>
              {strip.length ? (
                <div className="filmstrip">
                  {strip.map((frame) => (
                    <FrameButton
                      key={frame.id}
                      frame={frame}
                      carry={!framesOpen && frame.id === latest?.id}
                      fresh={isFresh(frame, steps, freshId)}
                      onOpen={(origin) => openAt(frame, origin)}
                      onBroken={onBroken}
                    />
                  ))}
                </div>
              ) : null}
          </div>
          <button type="button" className="disclosure frame-toggle" aria-expanded={framesOpen} onClick={toggleFrames}>
            {framesOpen ? "收起画面" : unseen ? `展开 ${shown.length} 张画面，有新的` : `展开 ${shown.length} 张画面`}
          </button>
        </div>
      ) : null}
      <div className="history">
        <div ref={listBox}>
          <ActivityList
            steps={steps}
            resourceStatus={view?.request.status || ""}
            detailed={historyOpen}
            open={historyOpen}
            freshId={freshId}
            headline={copy.title}
            onOpenFrame={onOpenFrame}
            onBroken={onBroken}
          />
        </div>
        <div className="history-bar">
          <button type="button" className="disclosure" aria-expanded={historyOpen} onClick={toggleHistory}>
            {historyOpen ? "收起过程" : "查看全部过程"}
          </button>
        </div>
        {view?.activity.history_status === "expired" ? <p className="history-note">更早的过程已经过期。</p> : null}
        {historyOpen && historyMessage ? <p className="history-note" role="status">{historyMessage}</p> : null}
        {view?.activity.history_status === "partial" ? <p className="history-note">一部分过程记录已经清理。</p> : null}
      </div>
      <p className="leave-note">
        {stopped ? "这次处理已停止。已完成的过程记录仍可查看。" : "可以离开这个页面，处理会在后台继续。"}
      </p>
    </section>
  )
}

function waitMotion(busy: boolean, phase: string | null, current: ActivityStep | null, transcribed: boolean): string {
  if (!busy) return ""
  if (transcribed) return "transcribe"
  if (current?.kind === "model_response" || current?.kind === "model_submission") return "model"
  if (phase === "investigate") return "watch"
  if (phase === "compose") return "write"
  if (phase === "prepare_source") return "prepare"
  return ""
}

function useWaitClock(createdAt: string | undefined, updatedAt: string | undefined, live: boolean, serverNow: string | undefined): string | null {
  const [now, setNow] = useState(() => performance.now())
  const anchor = useRef({ createdAt: undefined as string | undefined, serverNow: undefined as string | undefined, elapsedMs: 0, monotonicMs: performance.now() })
  const current = performance.now()
  if (createdAt && (anchor.current.createdAt !== createdAt || anchor.current.serverNow !== serverNow)) {
    const elapsed = serverNow ? serverElapsed(createdAt, serverNow) : null
    const previous = anchor.current.createdAt === createdAt ? anchoredElapsed(anchor.current, current) : 0
    anchor.current = { createdAt, serverNow, elapsedMs: Math.max(previous, elapsed ?? Math.max(0, Date.now() - Date.parse(createdAt))), monotonicMs: current }
  }
  useEffect(() => {
    if (!live) return
    setNow(performance.now())
    const timer = window.setInterval(() => setNow(performance.now()), 1000)
    return () => window.clearInterval(timer)
  }, [live, createdAt])
  if (!createdAt) return null
  if (live) return formatClock(anchoredElapsed(anchor.current, Math.max(now, current)))
  const end = updatedAt ? Date.parse(updatedAt) : Number.NaN
  return Number.isNaN(end) ? null : waitClock(createdAt, end)
}

function isFresh(frame: Frame, steps: ActivityStep[], freshId: string | null): boolean {
  return Boolean(freshId && steps.some((step) => step.id === freshId && (step.frames ?? []).some((item) => item.id === frame.id)))
}

function FrameButton({
  frame,
  hero,
  carry,
  fresh,
  onOpen,
  onBroken,
}: {
  frame: Frame
  hero?: boolean
  carry?: boolean
  fresh: boolean
  onOpen: (origin: HTMLElement) => void
  onBroken: (frame: Frame) => void
}) {
  return (
    <button
      type="button"
      className={hero ? "frame-card frame-hero" : "frame-card"}
      data-frame-id={frame.id}
      data-new={fresh ? "true" : undefined}
      onClick={(event) => onOpen(event.currentTarget.querySelector(".shot-host") || event.currentTarget)}
    >
      <span className="shot-host" data-carry-from={carry ? "true" : undefined}>
        <FrameThumb frame={frame} variant="thumbnail" onBroken={onBroken} />
      </span>
      <span className="frame-cap">
        {formatClock(frame.time_ms)} {frame.caption}
      </span>
    </button>
  )
}

function TranscriptionMeter({ done, total, ratio }: { done: string; total: string; ratio: number }) {
  const percent = Math.round(ratio * 100)
  return (
    <div
      className="transcribe"
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent}
      aria-valuetext={`视频 ${done} / ${total}`}
    >
      <div className="transcribe-track">
        <div className="transcribe-fill" style={{ transform: `scaleX(${ratio})` }} />
      </div>
      <p className="transcribe-meta">
        <span>视频 {done} / {total}</span>
      </p>
    </div>
  )
}

function PhaseSpine({ marks }: { marks: PhaseMark[] }) {
  return (
    <ol className="phase-spine" aria-label="处理阶段">
      {PHASES.map((phase, index) => (
        <li key={phase.id} data-state={marks[index]} aria-current={marks[index] === "current" ? "step" : undefined}>
          <span className="phase-dot" aria-hidden="true" />
          <span>{phase.label}</span>
        </li>
      ))}
    </ol>
  )
}

function Coverage({ duration, ranges }: { duration: number | null | undefined; ranges: Range[] }) {
  if (!duration || duration <= 0 || ranges.length === 0) return null
  return (
    <div className="coverage">
      <div className="coverage-track" aria-hidden="true">
        {ranges.map((range, index) => {
          const left = Math.max(0, Math.min(100, (range.start_ms / duration) * 100))
          const width = Math.max(0.8, Math.min(100 - left, ((range.end_ms - range.start_ms) / duration) * 100))
          return (
            <span
              className="coverage-mark"
              key={`${range.start_ms}-${range.end_ms}-${index}`}
              style={{ left: `${left}%`, width: `${width}%` }}
            />
          )
        })}
      </div>
    </div>
  )
}
