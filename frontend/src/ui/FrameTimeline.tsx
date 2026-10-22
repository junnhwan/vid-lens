import { useEffect, useId, useRef, useState, type CSSProperties } from "react"
import { createPortal } from "react-dom"
import type { Frame, Range } from "../contract/types"
import { formatClock, formatRange } from "../contract/text"
import { FrameThumb } from "./FrameThumb"

export function FrameTimeline({ duration, ranges, frames, activeRange, progress, stopped, live, latestId, onOpenFrame, onBroken }: {
  duration: number | null | undefined
  ranges: Range[]
  frames: Frame[]
  activeRange: Range | null
  progress: number | null
  stopped: boolean
  live: boolean
  latestId: string | null
  onOpenFrame: (frame: Frame, origin: HTMLElement) => void
  onBroken: (frame: Frame) => void
}) {
  const tooltipId = useId()
  const track = useRef<HTMLDivElement>(null)
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const peekAnchor = useRef<{ frame: Frame; pin: HTMLElement } | null>(null)
  const [peek, setPeek] = useState<{ id: string; left: number; top: number; origin: number; below: boolean } | null>(null)
  const chronological = [...frames].sort((a, b) => a.time_ms - b.time_ms || a.id.localeCompare(b.id))
  const preview = frames.find((frame) => frame.id === peek?.id)
  const total = duration && duration > 0 ? duration : Math.max(0, ...ranges.map((range) => range.end_ms), ...frames.map((frame) => frame.time_ms))
  const percent = (ms: number) => Math.max(0, Math.min(100, (ms / total) * 100))
  const covered = ranges.reduce((sum, range) => sum + Math.max(0, range.end_ms - range.start_ms), 0)
  const note = progress != null ? `正在转写 ${Math.round(progress * 100)}%`
    : activeRange ? `${stopped ? "停在" : "正在看"} ${formatRange(activeRange)}`
    : covered ? `已看 ${formatClock(covered)} / ${formatClock(total)}` : `全片 ${formatClock(total)}`

  function keepPeek() { clearTimeout(timer.current) }
  function closePeek() { keepPeek(); peekAnchor.current = null; setPeek(null) }
  function leavePeek() { keepPeek(); timer.current = setTimeout(closePeek, 160) }
  function showPeek(frame: Frame, pin: HTMLElement) {
    keepPeek()
    peekAnchor.current = { frame, pin }
    const rect = pin.getBoundingClientRect()
    const width = Math.min(244, window.innerWidth - 24)
    const height = width * 9 / 16 + 52
    const left = Math.max(12, Math.min(window.innerWidth - width - 12, rect.left + rect.width / 2 - width / 2))
    const below = rect.top < height + 20
    const top = Math.max(12, Math.min(window.innerHeight - height - 12, below ? rect.bottom + 10 : rect.top - height - 10))
    setPeek({ id: frame.id, left, top, origin: rect.left + rect.width / 2 - left, below })
  }
  useEffect(() => {
    const close = closePeek
    const reposition = () => {
      const anchor = peekAnchor.current
      if (!anchor) return
      const rect = anchor.pin.getBoundingClientRect()
      if (rect.bottom < 0 || rect.top > window.innerHeight || (document.activeElement !== anchor.pin && !anchor.pin.matches(":hover"))) close()
      else showPeek(anchor.frame, anchor.pin)
    }
    const key = (event: KeyboardEvent) => { if (event.key === "Escape") close() }
    window.addEventListener("scroll", reposition, true)
    window.addEventListener("resize", close)
    window.addEventListener("keydown", key)
    return () => { clearTimeout(timer.current); window.removeEventListener("scroll", reposition, true); window.removeEventListener("resize", close); window.removeEventListener("keydown", key) }
  }, [])
  if (!total) return null
  return (
    <div className="film-ruler" data-stopped={stopped || undefined} data-live={live || undefined} role="group" aria-label={`视频时间轴，${note}`}>
      <div className="ruler-head"><span className="ruler-kicker">视频时间轴</span><span className="ruler-note">{note}</span></div>
      <div className="ruler-track" ref={track}>
        <span className="ruler-perf" aria-hidden="true" />
        {ranges.map((range) => <span className="ruler-range" key={`${range.start_ms}-${range.end_ms}`} style={{ left: `${percent(range.start_ms)}%`, width: `${Math.max(.4, percent(range.end_ms) - percent(range.start_ms))}%` }} />)}
        {progress != null ? <span className="ruler-progress" style={{ width: `${Math.round(progress * 100)}%` }} /> : null}
        {activeRange ? <span className="ruler-now" style={{ left: `${percent(activeRange.start_ms)}%`, width: `${Math.max(.6, percent(activeRange.end_ms) - percent(activeRange.start_ms))}%` }}><i className="ruler-beam" aria-hidden="true" /></span> : null}
        {chronological.map((frame, index) => (
          <button type="button" className="ruler-pin" key={frame.id} data-latest={frame.id === latestId || undefined} data-preview={frame.id === peek?.id || undefined}
            style={{ left: `${Math.max(1, Math.min(99, percent(frame.time_ms)))}%`, "--pin-delay": `${Math.min(index * 35, 140)}ms` } as CSSProperties}
            aria-label={`查看 ${formatClock(frame.time_ms)} 的截图${frame.caption ? `：${frame.caption}` : ""}`} aria-describedby={frame.id === peek?.id ? tooltipId : undefined}
            onPointerEnter={(event) => { if (event.pointerType !== "touch") showPeek(frame, event.currentTarget) }} onPointerLeave={leavePeek}
            onFocus={(event) => showPeek(frame, event.currentTarget)} onBlur={leavePeek}
            onKeyDown={(event) => {
              const next = event.key === "ArrowRight" ? Math.min(chronological.length - 1, index + 1) : event.key === "ArrowLeft" ? Math.max(0, index - 1) : event.key === "Home" ? 0 : event.key === "End" ? chronological.length - 1 : -1
              if (next >= 0) { event.preventDefault(); track.current?.querySelectorAll<HTMLButtonElement>(".ruler-pin")[next]?.focus() }
            }}
            onClick={(event) => { closePeek(); onOpenFrame(frame, event.currentTarget) }}><span aria-hidden="true" /></button>
        ))}
      </div>
      <div className="ruler-scale" aria-hidden="true"><span>00:00</span><span>{formatClock(total / 2)}</span><span>{formatClock(total)}</span></div>
      <p className="ruler-hint">{frames.length ? <><span>{frames.length} 张截图</span><span>悬停预览 · 点击放大</span></> : <span>截取的画面会标记在这里</span>}</p>
      {peek && preview && typeof document !== "undefined" ? createPortal(
        <div className="frame-peek" style={{ left: peek.left, top: peek.top, "--peek-origin": `${peek.origin}px` } as CSSProperties} data-below={peek.below || undefined}
          onPointerEnter={keepPeek} onPointerLeave={leavePeek}>
          <button type="button" className="frame-peek-open" tabIndex={-1} aria-label={`放大 ${formatClock(preview.time_ms)} 的截图`}
            onClick={(event) => {
              const origin = peekAnchor.current?.pin ?? event.currentTarget
              origin.focus({ preventScroll: true })
              closePeek()
              onOpenFrame(preview, origin)
            }}>
            <span className="frame-peek-image"><FrameThumb key={preview.id} frame={preview} variant="thumbnail" onBroken={onBroken} /></span>
            <span className="frame-peek-meta" id={tooltipId} role="tooltip"><time>{formatClock(preview.time_ms)}</time><span>点击查看大图 ↗</span></span>
          </button>
        </div>, document.body) : null}
    </div>
  )
}
