import { useEffect, useRef } from "react"
import type { Frame } from "../contract/types"
import { formatClock } from "../contract/text"
import { EASE, flyStage, prefersReduced } from "./motion"
import { FrameThumb } from "./FrameThumb"

export function Lightbox({
  frames,
  index,
  open,
  origin,
  canAsk,
  readOnly = false,
  onClose,
  onIndex,
  onAsk,
  onBroken,
}: {
  frames: Frame[]
  index: number
  open: boolean
  origin: HTMLElement | null
  canAsk: boolean
  readOnly?: boolean
  onClose: () => void
  onIndex: (index: number) => void
  onAsk: (frame: Frame) => void
  onBroken: (frame: Frame) => void
}) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const stageRef = useRef<HTMLDivElement>(null)
  const stripRef = useRef<HTMLDivElement>(null)
  const originRef = useRef(origin)
  const closingRef = useRef(false)
  originRef.current = origin
  const frame = frames[index]

  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return
    if (open && !dialog.open) {
      closingRef.current = false
      dialog.showModal()
      if (!prefersReduced()) {
        dialog.animate([{ backgroundColor: "rgba(22, 19, 16, 0)" }, { backgroundColor: "rgba(22, 19, 16, 0.9)" }], {
          duration: 180,
          easing: EASE,
        })
        requestAnimationFrame(() => {
          const stage = stageRef.current
          if (!stage || !dialog.open || closingRef.current) return
          const motion = flyStage(stage, originRef.current, false)
          motion?.finished.then(() => motion.cancel()).catch(() => undefined)
        })
      }
    }
  }, [open])

  useEffect(() => {
    const dialog = dialogRef.current
    if (!open && dialog?.open) dialog.close()
  }, [open])

  useEffect(() => {
    if (!open) return
    function onKey(event: KeyboardEvent) {
      if (event.key === "ArrowRight" && index < frames.length - 1) {
        event.preventDefault()
        onIndex(index + 1)
      }
      if (event.key === "ArrowLeft" && index > 0) {
        event.preventDefault()
        onIndex(index - 1)
      }
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [open, index, frames.length, onIndex])

  useEffect(() => {
    if (!open) return
    const strip = stripRef.current
    const current = strip?.querySelector<HTMLElement>("[aria-current='true']")
    if (!strip || !current) return
    const left = current.offsetLeft - strip.clientWidth / 2 + current.clientWidth / 2
    strip.scrollTo({ left: Math.max(0, left), behavior: prefersReduced() ? "auto" : "smooth" })
  }, [open, index])

  function close(afterClose?: () => void) {
    if (closingRef.current) return
    const dialog = dialogRef.current
    const stage = stageRef.current
    if (!dialog?.open) {
      onClose()
      afterClose?.()
      return
    }
    const finishClose = () => {
      dialog.close()
      const origin = originRef.current
      const target = origin?.closest<HTMLElement>("button") ?? origin
      if (target?.getClientRects().length) target.focus({ preventScroll: true })
      else document.querySelector<HTMLElement>(".gallery-trigger")?.focus({ preventScroll: true })
      onClose()
      afterClose?.()
    }
    if (prefersReduced() || !stage) {
      finishClose()
      return
    }
    closingRef.current = true
    const motion = flyStage(stage, originRef.current, true)
    const fade = dialog.animate([{ backgroundColor: "rgba(22, 19, 16, 0.9)" }, { backgroundColor: "rgba(22, 19, 16, 0)" }], {
      duration: 180,
      easing: EASE,
      fill: "forwards",
    })
    const finish = () => {
      fade.cancel()
      motion?.cancel()
      dialog.style.backgroundColor = ""
      stage.style.transform = ""
      closingRef.current = false
      finishClose()
    }
    if (motion) motion.onfinish = finish
    else fade.onfinish = finish
  }

  return (
    <dialog
      id="lightbox"
      aria-label="画面"
      ref={dialogRef}
      onCancel={(event) => {
        event.preventDefault()
        close()
      }}
      onClick={(event) => {
        if (event.target === dialogRef.current) close()
      }}
    >
      {frame ? (
        <div className="lb">
          <div className="lb-stage" id="lb-stage" ref={stageRef}>
            <FrameThumb frame={frame} onBroken={onBroken} />
          </div>
          <div className="lb-meta">
            <p id="lb-caption"><time className="lb-timestamp">{formatClock(frame.time_ms)}</time>{frame.caption ? <span>{frame.caption}</span> : null}</p>
            <p id="lb-count" className="lb-count">
              {index + 1} / {frames.length}
            </p>
          </div>
          <div className="lb-toolbar">
            <div className="lb-nav">
              <button type="button" className="btn stop" disabled={index === 0} onClick={() => onIndex(index - 1)}>
                上一张
              </button>
              <button type="button" className="btn stop" disabled={index >= frames.length - 1} onClick={() => onIndex(index + 1)}>
                下一张
              </button>
            </div>
            <div className="lb-actions">
              {!readOnly && (
                <p className="lb-note" hidden={canAsk}>
                  文章完成后再问
                </p>
              )}
              {!readOnly && (
                <button
                  type="button"
                  className="btn primary"
                  disabled={!canAsk}
                  onClick={() => {
                    close(() => onAsk(frame))
                  }}
                >
                  问这张图
                </button>
              )}
              <button type="button" className="btn stop" onClick={() => close()}>
                关闭
              </button>
            </div>
          </div>
          {frames.length > 1 ? (
            <div className="lb-strip" ref={stripRef}>
              {frames.map((item, itemIndex) => (
                <button
                  type="button"
                  className="lb-thumb"
                  key={item.id}
                  aria-label={item.caption || formatClock(item.time_ms)}
                  aria-current={itemIndex === index ? "true" : undefined}
                  onClick={() => onIndex(itemIndex)}
                >
                  <FrameThumb frame={item} variant="thumbnail" onBroken={onBroken} />
                </button>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </dialog>
  )
}
