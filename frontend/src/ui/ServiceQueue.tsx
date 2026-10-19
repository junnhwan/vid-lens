import { useEffect, useState, type CSSProperties } from "react"
import { HttpApi } from "../api/http"
import type { SiteQueue } from "../contract/queue"

type QueueState = "loading" | "ready" | "error"

export function ServiceQueue({ active }: { active: boolean }) {
  const [queue, setQueue] = useState<SiteQueue | null>(null)
  const [state, setState] = useState<QueueState>("loading")
  useEffect(() => {
    if (!active) return
    let disposed = false
    let timer: number | undefined
    let controller: AbortController | undefined
    const api = new HttpApi()
    async function refresh() {
      if (disposed || document.hidden) return
      controller?.abort()
      const current = new AbortController()
      controller = current
      const timeout = window.setTimeout(() => current.abort(), 8000)
      let delay = 5000
      try {
        const next = await api.queue(current.signal)
        if (disposed || current !== controller || document.hidden) return
        setQueue(next)
        setState("ready")
      } catch {
        if (disposed || current !== controller || document.hidden) return
        setState("error")
        delay = 15000
      } finally {
        window.clearTimeout(timeout)
        if (!disposed && current === controller && !document.hidden) timer = window.setTimeout(refresh, delay)
      }
    }
    function visibility() {
      window.clearTimeout(timer)
      controller?.abort()
      if (!document.hidden) {
        setState("loading")
        void refresh()
      }
    }
    // Counts from a previous visit are not presented as a fresh live snapshot.
    setState("loading")
    void refresh()
    document.addEventListener("visibilitychange", visibility)
    return () => {
      disposed = true
      window.clearTimeout(timer)
      controller?.abort()
      document.removeEventListener("visibilitychange", visibility)
    }
  }, [active])
  return <QueueDisplay queue={queue} state={state} active={active} />
}

export function QueueDisplay({ queue, state, active = true }: { queue: SiteQueue | null; state: QueueState; active?: boolean }) {
  const live = state === "ready" && queue !== null
  const seats = live ? Math.max(queue.max_concurrent, queue.processing) : 0
  const moving = live && active
  const summary = !live ? state === "error" ? "暂时无法更新" : "正在读取状态" :
    queue.processing > 0 ? `${queue.processing} 个进行中` : "现在有空位"
  return (
    <section className="service-queue" aria-label="全站处理队列" data-live={moving} data-state={state} data-many={seats > 2} data-compact={seats > 12}>
      <div className="queue-heading">
        <span className="queue-title"><i aria-hidden="true" />全站处理队列</span>
        <span className="queue-summary" role="status" aria-live="polite" aria-atomic="true">{summary}</span>
      </div>
      <div className="queue-seats" aria-hidden="true">
        {live ? Array.from({ length: seats }, (_, index) => (
          <div className="queue-seat" data-occupied={index < queue.processing} key={index} style={{ "--seat-delay": `${index * -0.9}s` } as CSSProperties}>
            <div className="queue-film">
              <span className="queue-frame"><i /><i /><i /><b /></span>
              <span className="queue-paper"><i /><i /><i /></span>
            </div>
            <span>{index < queue.processing ? "进行中" : "空闲"}</span>
          </div>
        )) : <div className="queue-unavailable"><span />{state === "error" ? "稍后自动重连" : "连接服务中"}</div>}
      </div>
      <div className="queue-footer">
        <span>{live ? queue.processing > queue.max_concurrent ? <>当前上限 <strong>{queue.max_concurrent}</strong> 个 · 已开始的继续完成</> : <>最多同时 <strong>{queue.max_concurrent}</strong> 个</> : "以最新服务状态为准"}</span>
        <span>{live ? queue.waiting > 0 ? <><strong>{queue.waiting}</strong> 个等待中</> : "暂无等待" : state === "error" ? "状态暂时读不到" : "正在同步"}</span>
      </div>
      {live ? <span className="sr-only">最多同时处理 {queue.max_concurrent} 个，当前 {queue.processing} 个进行中，{queue.waiting} 个等待中。</span> : null}
    </section>
  )
}
