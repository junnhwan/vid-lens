import { useEffect, useMemo, useState } from "react"
import { tipKindLabel, waitTips } from "../app/waitTips"

const DWELL_MS = 6400

export function WaitTips({ phase, status }: { phase: string | null; status: string | null | undefined }) {
  const tips = useMemo(() => waitTips(phase, status), [phase, status])
  const [index, setIndex] = useState(0)
  const [paused, setPaused] = useState(false)
  const rotating = tips.length > 1
  const tip = tips[index % tips.length]

  useEffect(() => {
    if (!rotating || paused) return
    const timer = window.setInterval(() => setIndex((value) => (value + 1) % tips.length), DWELL_MS)
    return () => window.clearInterval(timer)
  }, [paused, rotating, tips.length])

  if (!tip) return null

  return (
    <div className="wait-tips" role="note" aria-label="等待时的小贴士" data-paused={paused ? "true" : undefined}>
      <span className="wait-tips-kicker">放映小贴士</span>
      <p className="wait-tips-text" key={tip.text}>
        {tip.text}
      </p>
      <span className="wait-tips-kind">{tipKindLabel[tip.kind]}</span>
      {rotating ? (
        <button type="button" className="wait-tips-pause" onClick={() => setPaused((value) => !value)}>
          {paused ? "继续" : "暂停"}
        </button>
      ) : null}
      {rotating ? (
        <span className="wait-tips-track" aria-hidden="true">
          <i
            key={`${index}-${paused ? "paused" : "running"}`}
            style={{ animationDuration: `${DWELL_MS}ms` }}
          />
        </span>
      ) : null}
    </div>
  )
}
