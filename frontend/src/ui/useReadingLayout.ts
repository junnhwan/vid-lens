import { useLayoutEffect, useRef, type CSSProperties, type RefObject } from "react"
import { prefersReduced } from "./motion"

type Layout = "closed" | "open" | "stage"
type Position = { top: number; anchor: HTMLElement | null; offset: number }
type Snapshot = { layout: Layout; article: DOMRect; qa: DOMRect; logTop: number; atBottom: boolean }

export function useReadingLayout(article: RefObject<HTMLDivElement>, qa: RefObject<HTMLDivElement>, layout: Layout, active: boolean, articleId?: string) {
  const previous = useRef<Snapshot | null>(null)
  const reading = useRef<Position | null>(null)
  const frozen = useRef<DOMRect | null>(null)
  const motions = useRef<Animation[]>([])
  const keyboard = useRef(false)
  const instant = useRef(false)
  const paperStart = useRef<number | null>(null)

  function snapshot(): Snapshot | null {
    if (!article.current || !qa.current) return null
    const log = qa.current.querySelector<HTMLElement>(".qa-log")
    return { layout, article: article.current.getBoundingClientRect(), qa: qa.current.getBoundingClientRect(), logTop: log?.scrollTop ?? 0, atBottom: !log || log.scrollHeight - log.scrollTop - log.clientHeight < 80 }
  }

  function prepare(keyboardAction = false) {
    previous.current = snapshot()
    keyboard.current = keyboardAction
    instant.current = keyboardAction || prefersReduced()
    if (layout === "stage" || !article.current) return
    const nodes = [...article.current.querySelectorAll<HTMLElement>("#prose [data-block-id], #prose h2")]
    const anchor = nodes.find((node) => node.getBoundingClientRect().bottom > 96) ?? null
    reading.current = { top: window.scrollY, anchor, offset: anchor?.getBoundingClientRect().top ?? 0 }
    frozen.current = article.current.getBoundingClientRect()
  }

  useLayoutEffect(() => {
    if (!active || !article.current || !qa.current) {
      motions.current.forEach((motion) => motion.cancel())
      previous.current = null
      return
    }
    const before = previous.current
    motions.current.forEach((motion) => motion.cancel())
    motions.current = []
    if (before && before.layout !== layout) {
      if (layout === "stage") window.scrollTo({ top: 0, left: 0, behavior: "instant" })
      else if (before.layout === "stage" && reading.current) {
        const { anchor, offset, top } = reading.current
        window.scrollTo({ top: anchor?.isConnected ? window.scrollY + anchor.getBoundingClientRect().top - offset : top, behavior: "instant" })
      }
      const log = qa.current.querySelector<HTMLElement>(".qa-log")
      if (log) log.scrollTop = before.atBottom ? log.scrollHeight : before.logTop
      const after = snapshot()
      if (after && !prefersReduced() && !keyboard.current) {
        const target = layout === "stage" || before.layout === "stage" ? qa.current : article.current
        const start = target === qa.current ? before.qa : before.article
        const end = target === qa.current ? after.qa : after.article
        if (Math.abs(start.left - end.left) > 1 || Math.abs(start.top - end.top) > 1) {
          const motion = target.animate([{ transform: `translate(${start.left - end.left}px, ${start.top - end.top}px)` }, { transform: "none" }], {
            duration: 280, easing: getComputedStyle(document.documentElement).getPropertyValue("--ease-drawer").trim() || "cubic-bezier(0.32, 0.72, 0, 1)",
          })
          motions.current.push(motion)
          motion.onfinish = () => motion.cancel()
        }
      }
    }
    previous.current = snapshot()
    keyboard.current = false
  }, [layout, active])

  useLayoutEffect(() => {
    previous.current = null
    reading.current = null
    frozen.current = null
    paperStart.current = null
    return () => { motions.current.forEach((motion) => motion.cancel()) }
  }, [articleId])

  const box = frozen.current
  const style = { "--reading-move-duration": instant.current ? "0ms" : "280ms", ...(box ? { "--paper-left": `${box.left}px`, "--paper-top": `${box.top}px`, "--paper-width": `${box.width}px` } : {}) } as CSSProperties
  function restorePaper() {
    const { anchor, offset } = reading.current ?? {}
    if (article.current && anchor?.isConnected && offset !== undefined) {
      article.current.scrollTop += anchor.getBoundingClientRect().top - Math.max(128, offset)
      paperStart.current = article.current.scrollTop
    }
  }
  function rememberPaper() {
    if (article.current && paperStart.current !== null && Math.abs(article.current.scrollTop - paperStart.current) < 1) return
    const anchor = [...(article.current?.querySelectorAll<HTMLElement>("#prose [data-block-id], #prose h2") ?? [])].find((node) => node.getBoundingClientRect().bottom > 128)
    if (anchor) reading.current = { top: reading.current?.top ?? 0, anchor, offset: Math.max(76, anchor.getBoundingClientRect().top - 52) }
    paperStart.current = null
  }
  return { prepare, style, restorePaper, rememberPaper }
}
