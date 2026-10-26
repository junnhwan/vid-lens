import { useEffect, useId, useRef, type ReactNode } from "react"
import { EASE, prefersReduced } from "./motion"

export function ReadingDialog({ open, title, onClose, onAfterClose, children, className = "" }: {
  open: boolean
  title: string
  onClose: () => void
  onAfterClose?: () => void
  children: ReactNode
  className?: string
}) {
  const titleId = useId()
  const dialog = useRef<HTMLDialogElement>(null)
  const animation = useRef<Animation | null>(null)
  const origin = useRef<HTMLElement | null>(null)
  const callbacks = useRef({ onClose, onAfterClose })
  const desiredOpen = useRef(open)
  callbacks.current = { onClose, onAfterClose }
  desiredOpen.current = open

  useEffect(() => {
    const node = dialog.current
    if (!node) return
    const from = node.open ? { opacity: getComputedStyle(node).opacity, transform: getComputedStyle(node).transform } : { opacity: "0", transform: "translateY(8px) scale(0.985)" }
    animation.current?.cancel()
    const keyboard = document.activeElement?.matches(":focus-visible")
    if (open) {
      if (!node.open) {
        origin.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
        node.showModal()
      }
      if (!prefersReduced() && !keyboard) {
        const motion = node.animate([from, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE, fill: "both" })
        animation.current = motion
        motion.onfinish = () => motion.cancel()
      }
    } else if (node.open) {
      const finish = () => {
        animation.current?.cancel()
        const scroll = { left: window.scrollX, top: window.scrollY }
        node.close()
        if (!document.querySelector("dialog[open]")) {
          if (origin.current?.isConnected) origin.current.focus({ preventScroll: true })
          window.scrollTo(scroll)
        }
        callbacks.current.onAfterClose?.()
      }
      if (prefersReduced() || keyboard) finish()
      else {
        const motion = node.animate([from, { opacity: 0, transform: "translateY(8px) scale(0.985)" }], { duration: 160, easing: EASE, fill: "both" })
        animation.current = motion
        motion.onfinish = finish
      }
    }
  }, [open])

  useEffect(() => {
    const node = dialog.current
    return () => {
      animation.current?.cancel()
      if (node?.open) node.close()
    }
  }, [])

  return <dialog ref={dialog} className={`reading-dialog ${className}`.trim()} aria-labelledby={titleId}
    onCancel={(event) => { event.preventDefault(); callbacks.current.onClose() }}
    onClose={(event) => { if (!event.currentTarget.open && desiredOpen.current) callbacks.current.onClose() }}
    onClick={(event) => { if (event.target === event.currentTarget) callbacks.current.onClose() }}>
    <header className="reading-dialog-head">
      <h2 id={titleId}>{title}</h2>
      <button type="button" className="text-button" aria-label={`关闭${title}`} onClick={onClose}>关闭</button>
    </header>
    <div className="reading-dialog-body">{children}</div>
  </dialog>
}
