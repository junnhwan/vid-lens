import { useEffect, useRef, useState, type CSSProperties, type Ref } from "react"
import type { ActivityStep, Frame, Range, RequestView } from "../contract/types"
import { formatClock, formatDuration, formatRange, platformLabel, provenanceNote } from "../contract/text"
import { describeRequest } from "../app/labels"
import { mergeRanges, parseTranscription, quietNote, waitClock, anchoredElapsed, serverElapsed } from "../app/progress"
import { animateTitle } from "./motion"
import { ActivityList } from "./ActivityList"
import { FrameThumb } from "./FrameThumb"
import { ModelIdentity } from "./ModelIdentity"
import { ExecutionPlan } from "./ExecutionPlan"
import { ShareControl } from "./ShareControl"
import { WaitTips } from "./WaitTips"

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
  const stillRef = useRef<HTMLSpanElement>(null)
  const [freshId, setFreshId] = useState<string | null>(null)
  const [freshFrameId, setFreshFrameId] = useState<string | null>(null)
  const seen = useRef(new Set<string>())
  const seenFrames = useRef(new Set<string>())
  const primedSteps = useRef(false)
  const primedFrames = useRef(false)
  const current = steps.find((step) => step.id === view?.activity.current_step_id) ?? null
  const copy = describeRequest(view, current, holding, reconnecting)
  const stopped = view?.request.status === "failed" || view?.request.status === "canceled"
  const source = view?.source
  const shown: Frame[] = []
  for (const step of steps) {
    for (const frame of step.frames ?? []) {
      if (!shown.some((item) => item.id === frame.id)) shown.push(frame)
    }
  }
  const latest = shown.at(-1) ?? null
  const latestIndex = latest ? shown.findIndex((item) => item.id === latest.id) : -1
  const freshFrame = latest ? latest.id === freshFrameId : false
  const transcribed = current && copy.title === "正在转写音频" ? parseTranscription(current.title) : null
  const phaseId = view?.execution?.phase || current?.phase || (stopped ? steps.at(-1)?.phase : null) || null
  const phaseMode = holding || view?.request.status === "completed" ? "done" : stopped ? "stopped" : "running"
  const clock = useWaitClock(view?.request.created_at, view?.request.updated_at, active && copy.busy, view?.server_now)
  const quiet = copy.busy ? quietNote(phaseId, view?.execution?.status) : ""
  const motion = waitMotion(copy.busy, phaseId, current, Boolean(transcribed))
  const ranges = mergeRanges(steps.flatMap((step) => (step.source_range ? [step.source_range] : [])))
  const activeRange = current?.source_range ?? (stopped ? steps.at(-1)?.source_range ?? null : null)
  const meta = [platformLabel(source?.platform), formatDuration(source?.duration_ms), source?.part?.label].filter(Boolean).join(" · ")
  const note = source
    ? view?.request.reuse === "published_article"
      ? "已有文章，直接打开。"
      : provenanceNote(source.text_provenance?.origin, source.text_provenance?.coverage)
    : "正在确认这条链接。"
  const hasActions = Boolean((csrf && view?.request.id) || view?.actions.can_cancel || view?.actions.can_retry)
  const journalCaption = copy.busy
    ? "新的动作会继续落在这里。"
    : stopped
      ? "已完成的动作都保留在这里。"
      : "画面、调用和核对按发生顺序保留。"

  useEffect(() => {
    if (!active) return
    const title = titleRef.current
    if (!title) return
    animateTitle(title, copy.title)
    document.title = `${copy.title} · VidLens`
  }, [active, copy.title])

  useEffect(() => {
    if (!primedSteps.current) {
      primedSteps.current = true
      for (const step of steps) seen.current.add(step.id)
      return
    }
    const newest = steps.at(-1)
    if (!newest || seen.current.has(newest.id)) return
    seen.current.add(newest.id)
    setFreshId(newest.id)
    const timer = window.setTimeout(() => setFreshId(null), 560)
    return () => window.clearTimeout(timer)
  }, [steps])

  useEffect(() => {
    if (!primedFrames.current) {
      primedFrames.current = true
      for (const frame of shown) seenFrames.current.add(frame.id)
      return
    }
    if (!latest || seenFrames.current.has(latest.id)) return
    seenFrames.current.add(latest.id)
    setFreshFrameId(latest.id)
    const timer = window.setTimeout(() => setFreshFrameId(null), 560)
    return () => window.clearTimeout(timer)
  }, [latest])

  function openAt(frame: Frame, origin: HTMLElement) {
    const index = shown.findIndex((item) => item.id === frame.id)
    onOpenFrame(shown, index < 0 ? 0 : index, origin)
  }

  return (
    <section className="view-run" ref={viewRef} data-state={stopped ? "stopped" : holding ? "holding" : "running"}>
      <header className="screening-head">
        <div className="screening-copy">
          <p className="source-title">{source?.title || (stopped ? "视频处理未完成" : "正在确认视频")}</p>
          <p className="source-meta">{meta || view?.request.input_url || ""}</p>
          {note ? <p className="source-note">{note}</p> : null}
        </div>
        <ModelIdentity model={view?.request.generation_model} purpose={view?.request.purpose} detailLevel={view?.request.detail_level} />
      </header>
      <div className="screening-now" aria-busy={copy.busy} data-motion={motion || undefined}>
        <div
          className="viewfinder"
          data-empty={latest ? undefined : "true"}
          data-tone={stopped ? "stopped" : undefined}
          data-live={copy.busy ? "true" : undefined}
        >
          <div className="viewfinder-scene">
            {latest ? (
              <span className="viewfinder-still" ref={stillRef} data-new={freshFrame ? "true" : undefined}>
                <FrameThumb frame={latest} variant="full" onBroken={onBroken} />
              </span>
            ) : (
              <span className="viewfinder-signal" aria-hidden="true">
                <i className="signal-scan" />
              </span>
            )}
            <span className="viewfinder-brackets" aria-hidden="true" />
            {copy.busy ? <span className="vf-sweep" aria-hidden="true" /> : null}
            {copy.busy && phaseId ? <span className="vf-phase-sweep" key={phaseId} aria-hidden="true" /> : null}
            <div className="viewfinder-top">
              <p className="kicker vf-kicker">
                <span className="pulse" hidden={!copy.busy} aria-hidden="true" />
                <span>{copy.kicker}</span>
              </p>
              {clock ? (
                <span className="elapsed vf-clock">
                  {copy.busy ? "已进行" : "用时"} {clock}
                </span>
              ) : null}
            </div>
            <div className="viewfinder-caption">
              <h1 className="activity-title vf-title" ref={titleRef} />
              {copy.purpose ? <p className="vf-purpose">{copy.purpose}</p> : null}
              <div className="vf-meta">
                {copy.range ? <span className="vf-range">{copy.range}</span> : null}
                {latest ? (
                  <span>
                    {formatClock(latest.time_ms)}
                    {latest.caption ? ` · ${latest.caption}` : ""} · 第 {latestIndex + 1} / {shown.length} 帧
                  </span>
                ) : null}
                {view?.failure?.diagnostic_id ? <span>错误编号：{view.failure.diagnostic_id}</span> : null}
                {latest ? (
                  <button
                    type="button"
                    className="vf-expand"
                    onClick={() => {
                      if (stillRef.current) openAt(latest, stillRef.current)
                    }}
                  >
                    查看大图
                  </button>
                ) : null}
              </div>
              {transcribed ? <TranscriptionMeter done={transcribed.done} total={transcribed.total} ratio={transcribed.ratio} /> : null}
            </div>
          </div>
        </div>
        {quiet ? <p className="quiet-note">{quiet}</p> : null}
        {copy.busy ? (
          <WaitTips
            key={`${phaseId ?? "connecting"}:${view?.execution?.status ?? "running"}`}
            phase={phaseId}
            status={view?.execution?.status}
          />
        ) : null}
        {hasActions ? (
          <div className="run-actions">
            {csrf && view?.request.id ? <ShareControl requestId={view.request.id} csrf={csrf} /> : null}
            <span className="run-spacer" aria-hidden="true" />
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
        ) : null}
        {holding ? (
          <div className="ready-banner">
            <p>文章已就绪</p>
            <button type="button" className="btn primary" onClick={onRead}>
              开始阅读
            </button>
          </div>
        ) : null}
        {articleError ? (
          <p className="form-error">
            {articleError}{" "}
            <button type="button" className="btn quiet" onClick={onRetryArticle}>
              重新读取
            </button>
          </p>
        ) : null}
      </div>
      <FilmRuler
        duration={source?.duration_ms}
        ranges={ranges}
        frames={shown}
        activeRange={activeRange}
        progress={transcribed?.ratio ?? null}
        stopped={stopped}
        live={copy.busy}
        latestId={latest?.id ?? null}
        onOpenFrame={openAt}
      />
      <ExecutionPlan
        activity={view?.activity}
        completed={view?.request.status === "completed"}
        phase={phaseId}
        phaseMode={phaseMode}
      />
      <div className="history">
        <div className="journal-head">
          <div>
            <p className="journal-kicker">
              现场记录
              {copy.busy ? (
                <span className="agent-dots" aria-hidden="true">
                  <i />
                  <i />
                  <i />
                </span>
              ) : null}
            </p>
            <p className="journal-caption">{journalCaption}</p>
          </div>
          <button type="button" className="disclosure" aria-expanded={historyOpen} onClick={onToggleHistory}>
            {historyOpen ? "收起过程" : "查看全部过程"}
          </button>
        </div>
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
        {view?.activity.history_status === "expired" ? <p className="history-note">更早的过程已经过期。</p> : null}
        {historyOpen && historyMessage ? (
          <p className="history-note" role="status">
            {historyMessage}
          </p>
        ) : null}
        {view?.activity.history_status === "partial" ? <p className="history-note">一部分过程记录已经清理。</p> : null}
      </div>
      <p className="leave-note">{stopped ? "这次处理已停止。已完成的过程记录仍可查看。" : "可以离开这个页面，处理会在后台继续。"}</p>
    </section>
  )
}

function FilmRuler({
  duration,
  ranges,
  frames,
  activeRange,
  progress,
  stopped,
  live,
  latestId,
  onOpenFrame,
}: {
  duration: number | null | undefined
  ranges: Range[]
  frames: Frame[]
  activeRange: Range | null
  progress: number | null
  stopped: boolean
  live: boolean
  latestId: string | null
  onOpenFrame: (frame: Frame, origin: HTMLElement) => void
}) {
  const total = duration && duration > 0 ? duration : Math.max(0, ...ranges.map((range) => range.end_ms), ...frames.map((frame) => frame.time_ms))
  if (!total) return null
  const percent = (ms: number) => Math.max(0, Math.min(100, (ms / total) * 100))
  const covered = ranges.reduce((sum, range) => sum + Math.max(0, range.end_ms - range.start_ms), 0)
  const note =
    progress != null
      ? `正在转写 ${Math.round(progress * 100)}%`
      : activeRange
        ? `${stopped ? "停在" : "正在看"} ${formatRange(activeRange)}`
        : covered
          ? `已看 ${formatClock(covered)} / ${formatClock(total)}`
          : frames.length
            ? `全片 ${formatClock(total)} · ${frames.length} 帧已标记`
            : `全片 ${formatClock(total)}`
  return (
    <div
      className="film-ruler"
      data-stopped={stopped ? "true" : undefined}
      data-live={live ? "true" : undefined}
      role="group"
      aria-label={`视频时间轴，${note}`}
    >
      <div className="ruler-head">
        <span className="ruler-kicker">视频时间轴</span>
        <span className="ruler-note">{note}</span>
      </div>
      <div className="ruler-track">
        <span className="ruler-perf" aria-hidden="true" />
        {ranges.map((range, index) => (
          <span
            className="ruler-range"
            key={`${range.start_ms}-${range.end_ms}-${index}`}
            style={{
              left: `${percent(range.start_ms)}%`,
              width: `${Math.max(0.4, percent(range.end_ms) - percent(range.start_ms))}%`,
            }}
          />
        ))}
        {progress != null ? <span className="ruler-progress" style={{ width: `${Math.round(progress * 100)}%` }} /> : null}
        {activeRange ? (
          <span
            className="ruler-now"
            style={{
              left: `${percent(activeRange.start_ms)}%`,
              width: `${Math.max(0.6, percent(activeRange.end_ms) - percent(activeRange.start_ms))}%`,
            }}
          >
            <i className="ruler-beam" aria-hidden="true" />
          </span>
        ) : null}
        {frames.map((frame, index) => (
          <button
            type="button"
            className="ruler-pin"
            key={frame.id}
            data-latest={frame.id === latestId ? "true" : undefined}
            style={
              {
                left: `${Math.max(1, Math.min(99, percent(frame.time_ms)))}%`,
                "--pin-delay": `${Math.min(index * 45, 360)}ms`,
              } as CSSProperties
            }
            title={`${formatClock(frame.time_ms)} ${frame.caption}`}
            aria-label={`打开 ${formatClock(frame.time_ms)} 的画面`}
            onClick={(event) => onOpenFrame(frame, event.currentTarget)}
          >
            <span aria-hidden="true" />
          </button>
        ))}
      </div>
      <div className="ruler-scale" aria-hidden="true">
        <span>00:00</span>
        <span>{formatClock(total / 2)}</span>
        <span>{formatClock(total)}</span>
      </div>
    </div>
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
        <span>
          视频 {done} / {total}
        </span>
      </p>
    </div>
  )
}
