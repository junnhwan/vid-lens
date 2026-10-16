import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { useRise } from "./countUp"

type Burst = { id: number; n: number; repeat: boolean }

function Thumb() {
  return (
    <svg className="like-thumb" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M8.2 10.4 11 4.2c.4-.8 1.5-.9 2.1-.2.4.5.5 1.2.3 1.8L12.4 9h5.1c1.2 0 2.1 1.1 1.9 2.3l-1.1 6.2A2.2 2.2 0 0 1 16.1 19H8.2V10.4z" />
      <path d="M3.6 10.2h3.2c.4 0 .7.3.7.7v7.4c0 .4-.3.7-.7.7H3.6c-.6 0-1.1-.5-1.1-1.1v-6.6c0-.6.5-1.1 1.1-1.1z" />
    </svg>
  )
}

function sprayStyle(n: number, i: number, drops: number, bloom: boolean) {
  const angle = ((n * 52 - 90) * Math.PI) / 180
  const spread = angle + (i - (drops - 1) / 2) * 0.42
  const reach = 18 + (i % 3) * 9 + (bloom ? 10 : 0)
  return {
    ["--dx" as string]: `${Math.cos(spread) * reach}px`,
    ["--dy" as string]: `${Math.sin(spread) * reach}px`,
    ["--spin" as string]: `${((i * 37 + n * 13) % 70) - 35}deg`,
  }
}

export function LikeButton({ count, ready, csrf, active = true }: { count?: number; ready: boolean; csrf: string; active?: boolean }) {
  const [liked, setLiked] = useState(false)
  const [inked, setInked] = useState(false)
  const [total, setTotal] = useState(count)
  const [fresh, setFresh] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [bursts, setBursts] = useState<Burst[]>([])
  const [blooming, setBlooming] = useState(false)
  const api = useRef(new HttpApi())
  const sealRef = useRef<HTMLSpanElement>(null)
  const taps = useRef(0)
  const saving = useRef(false)
  api.current.csrfToken = csrf
  const shown = useRise(total, active && total !== undefined)
  useEffect(() => {
    setTotal(count)
  }, [count])
  useEffect(() => {
    if (!ready) return
    let active = true
    void api.current
      .like()
      .then((r) => {
        if (!active) return
        setLiked(r.liked)
        if (r.liked) setInked(true)
        setTotal(r.count)
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [ready])

  function play(repeat: boolean) {
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) return
    const n = taps.current + 1
    taps.current = n
    const id = n
    const bloom = n % 5 === 0
    setBursts((list) => [...list.slice(-5), { id, n, repeat }])
    if (bloom) {
      setBlooming(true)
      window.setTimeout(() => setBlooming(false), 520)
    }
    const seal = sealRef.current
    if (seal) {
      const tilt = ((n % 5) - 2) * 11
      for (const anim of seal.getAnimations()) anim.cancel()
      seal.animate(
        [
          { transform: "translateY(0) rotate(0deg) scale(1)" },
          { transform: `translateY(3px) rotate(${tilt}deg) scale(0.88)`, offset: 0.28 },
          { transform: `translateY(0) rotate(${tilt * 0.35}deg) scale(1.06)`, offset: 0.62 },
          { transform: "translateY(0) rotate(0deg) scale(1)" },
        ],
        { duration: 460, easing: "cubic-bezier(0.23, 1, 0.32, 1)" },
      )
    }
    window.setTimeout(() => {
      setBursts((list) => list.filter((item) => item.id !== id))
    }, 860)
  }

  async function like() {
    if (!ready) return
    const repeat = liked || saving.current
    play(repeat)
    if (!repeat) setInked(true)
    if (repeat) return
    saving.current = true
    setBusy(true)
    setError("")
    try {
      const r = await api.current.like(true)
      setLiked(r.liked)
      setInked(r.liked)
      setTotal((prev) => {
        if (prev !== r.count) setFresh(true)
        return r.count
      })
    } catch {
      try {
        const r = await api.current.like()
        setLiked(r.liked)
        setInked(r.liked)
        setTotal(r.count)
        if (!r.liked) setError("暂时没能保存，稍后可以再试。")
      } catch {
        setInked(false)
        setError("暂时连不上，点赞状态待确认。")
      }
    } finally {
      saving.current = false
      setBusy(false)
      window.setTimeout(() => setFresh(false), 220)
    }
  }

  return (
    <span className="like-wrap">
      <button
        type="button"
        className="like"
        aria-pressed={liked}
        data-inked={inked ? "true" : "false"}
        data-bloom={blooming ? "true" : "false"}
        disabled={!ready}
        onClick={() => void like()}
      >
        <span className="like-well" aria-hidden="true">
          <span className="like-prints">
            {bursts.map((burst) => {
              const tilt = ((burst.n % 5) - 2) * 11
              const angle = ((burst.n * 52 - 90) * Math.PI) / 180
              const dist = 26 + (burst.n % 4) * 8
              return (
                <span
                  key={burst.id}
                  className="like-ghost"
                  style={{
                    ["--dx" as string]: `${Math.cos(angle) * dist}px`,
                    ["--dy" as string]: `${Math.sin(angle) * dist}px`,
                    ["--spin" as string]: `${tilt + (burst.n % 2 ? 16 : -12)}deg`,
                  }}
                >
                  <Thumb />
                </span>
              )
            })}
          </span>
          <span className="like-seal" ref={sealRef}>
            <Thumb />
            {bursts.map((burst) => {
              const bloom = burst.n % 5 === 0
              const drops = bloom ? 9 : 6
              const tilt = ((burst.n % 5) - 2) * 11
              const angle = ((burst.n * 52 - 90) * Math.PI) / 180
              return (
                  <span className="like-burst" key={burst.id}>
                  <span className="like-spray">
                    {Array.from({ length: drops }, (_, i) => (
                      <i
                        key={i}
                        style={sprayStyle(burst.n, i, drops, bloom)}
                        data-warm={i % 3 === 0 ? "true" : undefined}
                        data-hot={bloom && i === drops - 1 ? "true" : undefined}
                      />
                    ))}
                  </span>
                  <span className="like-dent" style={{ ["--spin" as string]: `${tilt}deg` }} />
                  {burst.repeat ? (
                    <span
                      className="like-plus like-echo"
                      style={{ ["--drift" as string]: `${Math.round(Math.cos(angle) * 12)}px` }}
                    >
                      ✦
                    </span>
                  ) : (
                    <span className="like-plus" style={{ ["--drift" as string]: `${Math.round(Math.cos(angle) * 12)}px` }}>
                      +1
                    </span>
                  )}
                </span>
              )
            })}
          </span>
        </span>
        <span className="like-label">{busy ? "正在保存" : liked ? "已点赞" : "觉得有用"}</span>
        {shown !== undefined ? (
          <span className="like-count">
            <span className={fresh ? "like-num is-fresh" : "like-num"}>{shown.toLocaleString("zh-Hans")}</span>
          </span>
        ) : null}
      </button>
      {error ? <small role="status">{error}</small> : null}
    </span>
  )
}
