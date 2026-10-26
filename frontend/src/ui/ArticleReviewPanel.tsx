import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { canReplay, forgetWrite, memoryStore, newIdempotencyKey, pendingWrite, rememberWrite } from "../api/writes"

export interface ArticleReviewView {
  review: {
    id: string
    article_id: string
    trigger: "automatic" | "manual"
    status: "queued" | "running" | "completed" | "incomplete"
    result: { summary: string; findings: { section_id: string; problem: string; suggestion: string }[] } | null
  } | null
}

export function ArticleReviewPanel({ requestId, csrf, onJump }: { requestId: string; csrf: string; onJump: (id: string) => void }) {
  const [view, setView] = useState<ArticleReviewView | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [tick, setTick] = useState(0)
  const [loaded, setLoaded] = useState(false)
  const api = useRef(new HttpApi())
  api.current.csrfToken = csrf
  const current = useRef(requestId)
  current.current = requestId
  const storage = useRef((() => { try { return sessionStorage } catch { return memoryStore() } })())
  useEffect(() => {
    let disposed = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let failures = 0
    async function poll() {
      if (document.hidden) { timer = setTimeout(poll, 2000); return }
      try {
        const r = await api.current.request<ArticleReviewView>("GET", `/api/v1/requests/${requestId}/reviews`)
        if (disposed) return
        setView(r.body)
        setLoaded(true)
        setError("")
        failures = 0
        if (r.body.review && ["queued", "running"].includes(r.body.review.status)) timer = setTimeout(poll, 2000)
      } catch (e) {
        if (disposed) return
        setError(e instanceof Error ? e.message : "暂时读不到审核意见。")
        if (++failures <= 4) timer = setTimeout(poll, 4000)
      }
    }
    void poll()
    return () => { disposed = true; clearTimeout(timer) }
  }, [requestId, tick])
  async function review() {
    if (busy) return
    const id = requestId
    const path = `/api/v1/requests/${id}/reviews`
    const slot = `article-review:${id}`
    let pending = pendingWrite(storage.current, slot)
    if (!pending || !canReplay(pending, Date.now())) {
      pending = { slot, path, body: "{}", key: newIdempotencyKey(), startedAt: Date.now(), expiresAt: Date.now() + 86400000 }
      rememberWrite(storage.current, pending)
    }
    setBusy(true)
    setError("")
    try {
      const r = await api.current.request<ArticleReviewView>("POST", path, {}, pending.key)
      forgetWrite(storage.current, slot)
      if (current.current !== id) return
      setView(r.body)
      setTick((n) => n + 1)
    } catch (e) {
      if (current.current === id) setError(e instanceof Error ? e.message : "这次审核未能开始，文章仍可阅读和导出。")
    } finally { if (current.current === id) setBusy(false) }
  }
  const pending = view?.review && ["queued", "running"].includes(view.review.status)
  const status = !loaded ? error ? "暂不可用" : "读取中" : view?.review ? ({ queued: "等待审核", running: "审核中", completed: "已完成", incomplete: "未完成" } as const)[view.review.status] : "尚未审核"
  return <aside className="article-review" id="article-review" aria-label="文章审核意见">
    <div className="reading-section-head"><h3>文章审核 <span className="reading-review-state">{status}</span></h3>
    <button type="button" className="btn reading-review-button" disabled={!loaded || busy || Boolean(pending)} onClick={() => void review()}>
      {busy ? "正在发起审核…" : pending ? "审核中…" : view?.review ? "再次审核" : "审核这篇文章"}
    </button></div>
    <p className="reading-review-summary" role="status">{!loaded ? error ? "审核状态暂时无法读取，稍后可以重试。" : "正在读取最近一次审核状态。" : pending ? "文章已生成，审核正在进行。" : view?.review?.status === "incomplete" ? "本次审核未完成，文章可正常阅读和导出。" : view?.review?.status === "completed" ? "以下意见供你核对，正文保持原样。" : "文章已生成，可以按需发起审核。"}</p>
    {view?.review?.result ? <details className="reading-review-details">
      <summary>查看审核意见<span>{view.review.result.findings.length ? `${view.review.result.findings.length} 条核对建议` : "审核摘要"}</span></summary>
      <div className="reading-review-result"><p>{view.review.result.summary}</p>
      {view.review.result.findings.length ? <ol>{view.review.result.findings.map((finding, index) => <li key={index}>
        <p>{finding.problem}</p><p className="reading-review-suggestion"><span>建议</span>{finding.suggestion}</p>
        <button type="button" className="reading-note-link" onClick={() => onJump(finding.section_id)}>查看对应章节 <span aria-hidden="true">↗</span></button>
      </li>)}</ol> : null}</div>
    </details> : null}
    {error ? <div className="reading-notice" data-tone="error" role="alert"><p>{error}</p><button type="button" className="reading-note-link" onClick={() => setTick((value) => value + 1)}>重新读取状态</button></div> : null}
  </aside>
}
