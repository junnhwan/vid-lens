import { useEffect, useRef, useState } from "react"
import { prefersReduced } from "./motion"

export function useRise(target: number | undefined, play: boolean) {
  const [shown, setShown] = useState<number | undefined>(undefined)
  const finished = useRef<number | null>(null)
  const prevPlay = useRef(false)

  useEffect(() => {
    if (target === undefined) return
    if (!play) {
      prevPlay.current = false
      return
    }
    const entered = !prevPlay.current
    prevPlay.current = true
    if (!entered && finished.current !== null && finished.current !== target) {
      finished.current = target
      setShown(target)
      return
    }
    if (!entered && finished.current === target) {
      setShown(target)
      return
    }
    if (prefersReduced() || target <= 0) {
      finished.current = target
      setShown(target)
      return
    }
    setShown(0)
    const start = performance.now()
    const duration = 980
    let frame = 0
    let cancelled = false
    const tick = (now: number) => {
      if (cancelled) return
      const t = Math.min(1, (now - start) / duration)
      const eased = 1 - Math.pow(1 - t, 3)
      const step = Math.round(target * eased)
      setShown(step > 0 ? step : 0)
      if (t < 1) frame = requestAnimationFrame(tick)
      else finished.current = target
    }
    frame = requestAnimationFrame(tick)
    return () => {
      cancelled = true
      cancelAnimationFrame(frame)
    }
  }, [target, play])

  return shown
}
