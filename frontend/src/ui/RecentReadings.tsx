import { useEffect, useRef, useState, type CSSProperties } from "react"
import type { RequestHistoryItem } from "../contract/types"
import { HttpApi } from "../api/http"
import { sessionLost } from "../api/error"
import { displayDate, isRunning, statusName } from "./historyText"
import { rememberTitleCarry } from "./motion"

const cookieNote = "清除 Cookie 或换浏览器后无法找回。"

export function RecentReadings({
  active,
  onOpen,
  onHistory,
}: {
  active: boolean
  onOpen: (id: string) => void
  onHistory: () => void
}) {
  const [items, setItems] = useState<RequestHistoryItem[]>([])
  const [status, setStatus] = useState<"loading" | "empty" | "error" | "ready">("loading")
  const [stagger, setStagger] = useState(false)
  const staggered = useRef(false)
  const api = useRef(new HttpApi())
  const itemsRef = useRef(items)
  itemsRef.current = items

  useEffect(() => {
    if (!stagger) return
    const timer = window.setTimeout(() => setStagger(false), 800)
    return () => window.clearTimeout(timer)
  }, [stagger])

  useEffect(() => {
    if (!active) return
    let live = true
    const load = () => {
      void api.current
        .requestHistory(0, 3)
        .then((page) => {
          if (!live) return
          const next = page.items.slice(0, 3)
          setItems(next)
          setStatus(next.length ? "ready" : "empty")
          if (next.length && !staggered.current) {
            staggered.current = true
            setStagger(true)
          }
        })
        .catch((error: unknown) => {
          if (!live) return
          if (sessionLost(error)) {
            setItems([])
            setStatus("empty")
            return
          }
          setStatus(itemsRef.current.length ? "ready" : "error")
        })
    }
    load()
    return () => {
      live = false
    }
  }, [active])

  useEffect(() => {
    if (!active || !items.some((item) => isRunning(item.status))) return
    const timer = window.setInterval(() => {
      void api.current
        .requestHistory(0, 3)
        .then((page) => {
          const next = page.items.slice(0, 3)
          setItems(next)
          setStatus(next.length ? "ready" : "empty")
        })
        .catch(() => undefined)
    }, 12000)
    return () => window.clearInterval(timer)
  }, [active, items])

  function openHistory(event: { preventDefault: () => void }) {
    event.preventDefault()
    onHistory()
  }

  if (status === "loading") return null
  if (status === "empty") {
    return (
      <p className="recent-empty">
        <a href="/history" onClick={openHistory}>
          解读会留在这个浏览器里，无需登录。
        </a>
        <span>{cookieNote}</span>
      </p>
    )
  }
  if (status === "error") {
    return (
      <p className="recent-empty">
        <a href="/history" onClick={openHistory}>
          处理记录暂时没有读出来。
        </a>
        <span>{cookieNote}</span>
      </p>
    )
  }
  return (
    <section className="recent-readings" aria-labelledby="recent-label">
      <div className="recent-head">
        <h2 id="recent-label">此浏览器的解读</h2>
        <a href="/history" onClick={openHistory}>
          全部记录
        </a>
      </div>
      <ol className="recent-list" data-stagger={stagger ? "true" : undefined} aria-label="最近的解读">
        {items.map((item, index) => (
          <li key={item.id} style={{ "--row": Math.min(index, 12) } as CSSProperties}>
            <a
              className="recent-row"
              href={`/r/${encodeURIComponent(item.id)}`}
              onClick={(event) => {
                event.preventDefault()
                const title = event.currentTarget.querySelector("strong")
                if (title instanceof HTMLElement) rememberTitleCarry(title)
                onOpen(item.id)
              }}
            >
              <strong title={item.title}>{item.title || "未命名视频"}</strong>
              <span className="recent-meta">
                <span className="history-status" data-status={item.status}>
                  <i aria-hidden="true" />
                  {statusName(item.status)}
                </span>
                <time dateTime={item.updated_at}>{displayDate(item.updated_at)}</time>
              </span>
            </a>
          </li>
        ))}
      </ol>
      <p className="recent-note">{cookieNote}</p>
    </section>
  )
}
