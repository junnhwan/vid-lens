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
  return <aside className="limits article-review" id="article-review" aria-label="文章审核意见">
    <h2>审核意见</h2>
    <p role="status">{pending ? "文章已生成，审核正在进行。" : view?.review?.status === "incomplete" ? "本次审核未完成，文章可正常阅读和导出。" : view?.review?.status === "completed" ? "以下意见供你核对，正文保持原样。" : "文章已生成，可以按需发起审核。"}</p>
    {view?.review?.result ? <>
      <p>{view.review.result.summary}</p>
      {view.review.result.findings.length ? <ol>{view.review.result.findings.map((finding, index) => <li key={index}>
        <p>{finding.problem}</p><p>{finding.suggestion}</p>
        <button type="button" className="time" onClick={() => onJump(finding.section_id)}>定位章节</button>
      </li>)}</ol> : null}
    </> : null}
    {error ? <p className="form-error" role="alert">{error}</p> : null}
    <button type="button" className="btn quiet" disabled={busy || Boolean(pending)} onClick={() => void review()}>
      {busy ? "正在发起审核…" : pending ? "审核中…" : view?.review ? "再次审核" : "审核这篇文章"}
    </button>
  </aside>
}
