import { useEffect, useRef, useState, type CSSProperties, type RefObject } from "react"
import type { RequestHistoryItem } from "../contract/types"
import { HttpApi } from "../api/http"
import { ShareControl } from "./ShareControl"
import { displayDate, displayUrl, isRunning, statusName } from "./historyText"
import { rememberTitleCarry } from "./motion"

const phaseNames: Record<string, string> = {
  prepare_source: "准备来源",
  overview: "梳理全片",
  investigate: "查看画面",
  compose: "组织文章",
  validate: "核对文章",
  publish: "发布文章",
}

function mergeLatest(latest: RequestHistoryItem[], current: RequestHistoryItem[]): RequestHistoryItem[] {
  const ids = new Set(latest.map((item) => item.id))
  return [...latest, ...current.filter((item) => !ids.has(item.id))]
}

export function HistoryView({
  active,
  viewRef,
  onOpen,
  onNew,
  csrf,
}: {
  active: boolean
  viewRef: RefObject<HTMLElement>
  onOpen: (id: string) => void
  onNew: () => void
  csrf: string
}) {
  const [items, setItems] = useState<RequestHistoryItem[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState("")
  const [stagger, setStagger] = useState(false)
  const itemsRef = useRef(items)
  const refreshing = useRef(false)
  const firstList = useRef(true)
  const api = useRef(new HttpApi())
  const refreshRef = useRef<(silent?: boolean) => Promise<void>>(async () => undefined)
  itemsRef.current = items

  refreshRef.current = async (silent = false) => {
    if (refreshing.current) return
    refreshing.current = true
    if (!silent && itemsRef.current.length === 0) setLoading(true)
    try {
      const page = await api.current.requestHistory(0)
      setItems((current) => mergeLatest(page.items, current))
      setHasMore(page.has_more)
      setError("")
      if (firstList.current && page.items.length) {
        firstList.current = false
        setStagger(true)
      }
    } catch {
      setError("暂时无法读取处理记录，请稍后重试。")
    } finally {
      refreshing.current = false
      setLoading(false)
    }
  }

  useEffect(() => {
    if (!stagger) return
    const timer = window.setTimeout(() => setStagger(false), 900)
    return () => window.clearTimeout(timer)
  }, [stagger])

  useEffect(() => {
    if (active) void refreshRef.current()
  }, [active])

  const hasRunning = items.some((item) => isRunning(item.status))
  useEffect(() => {
    if (!active || !hasRunning) return
    const timer = window.setInterval(() => void refreshRef.current(true), 12000)
    return () => window.clearInterval(timer)
  }, [active, hasRunning])

  async function loadMore() {
    if (loadingMore) return
    setLoadingMore(true)
    try {
      const page = await api.current.requestHistory(itemsRef.current.length)
      setItems((current) => {
        const existing = new Set(current.map((item) => item.id))
        return [...current, ...page.items.filter((item) => !existing.has(item.id))]
      })
      setHasMore(page.has_more)
      setError("")
    } catch {
      setError("暂时无法读取更早的处理记录，请稍后重试。")
    } finally {
      setLoadingMore(false)
    }
  }

  return (
    <section className="view-input history-view" ref={viewRef}>
      <div className="history-heading">
        <div>
          <p className="kicker"><span aria-hidden="true" />我的视频</p>
          <h1>处理记录</h1>
          <p className="history-description">只显示此浏览器匿名会话提交的视频；更换浏览器或清除会话 Cookie 后无法找回。</p>
        </div>
        <button
          className="history-refresh"
          type="button"
          onClick={() => void refreshRef.current()}
          disabled={loading || loadingMore}
          aria-label="刷新处理记录"
        >
          刷新
        </button>
      </div>

      {error ? <p className="history-error" role="alert">{error}</p> : null}
      {loading && items.length === 0 ? (
        <p className="history-state" role="status">正在读取处理记录…</p>
      ) : items.length === 0 && !error ? (
        <div className="history-empty">
          <p>这里还没有处理记录。</p>
          <button type="button" className="btn primary" onClick={onNew}>开始解读视频</button>
        </div>
      ) : (
        <ol className="history-list" data-stagger={stagger ? "true" : undefined} aria-label="视频处理记录">
          {items.map((item, index) => (
            <li key={item.id} style={{ "--row": Math.min(index, 12) } as CSSProperties}>
              <a
                className="history-row"
                href={`/r/${encodeURIComponent(item.id)}`}
                onClick={(event) => {
                  event.preventDefault()
                  const title = event.currentTarget.querySelector("strong")
                  if (title instanceof HTMLElement) rememberTitleCarry(title)
                  onOpen(item.id)
                }}
              >
                <span className="history-row-main">
                  <strong title={item.title}>{item.title || displayUrl(item.canonical_url)}</strong>
                  <span title={item.canonical_url}>
                    {item.platform === "youtube" ? "YouTube" : item.platform === "bilibili" ? "B 站" : item.platform} · {displayUrl(item.canonical_url)}
                  </span>
                </span>
                <span className="history-row-meta">
                  <span className="history-status" data-status={item.status}>
                    <i aria-hidden="true" />{statusName(item.status)}
                  </span>
                  {isRunning(item.status) && item.phase ? <span className="history-phase">{phaseNames[item.phase] || "处理中"}</span> : null}
                  <time dateTime={item.updated_at}>{displayDate(item.updated_at)}</time>
                </span>
                <span className="history-row-arrow" aria-hidden="true">→</span>
              </a>
              <ShareControl requestId={item.id} csrf={csrf} />
            </li>
          ))}
        </ol>
      )}

      {hasMore ? (
        <div className="history-more">
          <button type="button" className="btn stop" onClick={() => void loadMore()} disabled={loadingMore}>
            {loadingMore ? "正在读取" : "加载更早记录"}
          </button>
        </div>
      ) : null}
    </section>
  )
}
