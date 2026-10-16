import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"

export function LikeButton({ count, ready, csrf }: { count?: number; ready: boolean; csrf: string }) {
  const [liked, setLiked] = useState(false)
  const [total, setTotal] = useState(count)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const api = useRef(new HttpApi())
  api.current.csrfToken = csrf
  useEffect(() => {
    setTotal(count)
  }, [count])
  useEffect(() => {
    if (!ready) return
    let active = true
    void api.current
      .like()
      .then((r) => {
        if (active) {
          setLiked(r.liked)
          setTotal(r.count)
        }
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [ready])
  async function like() {
    if (liked || busy) return
    setBusy(true)
    setError("")
    try {
      const r = await api.current.like(true)
      setLiked(r.liked)
      setTotal(r.count)
    } catch {
      try {
        const r = await api.current.like()
        setLiked(r.liked)
        setTotal(r.count)
        if (!r.liked) setError("暂时没能保存，稍后可以再试。")
      } catch {
        setError("暂时连不上，点赞状态待确认。")
      }
    } finally {
      setBusy(false)
    }
  }
  return (
    <span className="like-wrap">
      <button type="button" className="like" aria-pressed={liked} disabled={!ready || busy || liked} onClick={() => void like()}>
        <svg viewBox="0 0 20 20" aria-hidden="true">
          <path
            d="M7 17H4V9h3m0 8h8l2-8h-6l1-5c-2-1-2 4-5 5z"
            fill={liked ? "currentColor" : "none"}
            stroke="currentColor"
            strokeWidth="1.3"
            strokeLinejoin="round"
          />
        </svg>
        <span>{busy ? "正在保存" : liked ? "已点赞" : "觉得有用"}</span>
        {total !== undefined ? <span className="like-count">{total.toLocaleString("zh-Hans")}</span> : null}
      </button>
      {error ? <small role="status">{error}</small> : null}
    </span>
  )
}
