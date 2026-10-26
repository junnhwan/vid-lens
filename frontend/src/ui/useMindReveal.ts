import { useCallback, useEffect, useLayoutEffect, useRef, type RefObject } from "react"
import { EASE, prefersReduced } from "./motion"

const useClientLayout = typeof window === "undefined" ? useEffect : useLayoutEffect

// Remember actual elements: a restored view stays still, while remounted
// descendants can grow again when the reader opens their branch.
export function useMindReveal(root: RefObject<HTMLElement>, active: boolean, signature: string) {
  const seen = useRef(new WeakSet<Element>())
  const running = useRef(new Set<Animation>())
  const stop = useCallback(() => {
    for (const animation of running.current) animation.cancel()
    running.current.clear()
  }, [])

  useClientLayout(() => {
    stop()
    const container = root.current
    if (!container || !active) return
    const fresh = [...container.querySelectorAll<HTMLElement | SVGElement>("[data-mind-enter]")].filter((element) => !seen.current.has(element))
    for (const element of fresh) seen.current.add(element)
    if (!fresh.length || prefersReduced() || document.activeElement?.matches(":focus-visible")) return
    const firstLevel = Math.min(...fresh.map((element) => Number(element.getAttribute("data-mind-level"))))
    for (const element of fresh) {
      const level = Number(element.getAttribute("data-mind-level"))
      const wire = element.getAttribute("data-mind-enter") === "wire"
      const delay = Math.min(3, Math.max(0, level - firstLevel)) * 30 + (!wire && level > 0 ? 30 : 0)
      const from = Number(element.getAttribute("data-mind-from") ?? -1) * 6
      const frames = wire
        ? [{ strokeDasharray: "1", strokeDashoffset: "1" }, { strokeDasharray: "1", strokeDashoffset: "0" }]
        : [{ opacity: 0, transform: `translateX(${from}px)` }, { opacity: 1, transform: "none" }]
      const animation = element.animate(frames, { duration: 160, delay, easing: EASE, fill: "backwards" })
      running.current.add(animation)
      animation.onfinish = () => { running.current.delete(animation); animation.cancel() }
    }
    return stop
  }, [active, signature, stop])

  useEffect(() => {
    const container = root.current
    if (!container || !active) return
    container.addEventListener("pointerdown", stop, true)
    container.addEventListener("keydown", stop, true)
    container.addEventListener("wheel", stop, { capture: true, passive: true })
    return () => {
      container.removeEventListener("pointerdown", stop, true)
      container.removeEventListener("keydown", stop, true)
      container.removeEventListener("wheel", stop, true)
    }
  }, [active, stop])
}
