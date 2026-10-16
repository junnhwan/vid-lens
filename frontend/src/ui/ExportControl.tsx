import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { canReplay, forgetWrite, memoryStore, newIdempotencyKey, pendingWrite, rememberWrite } from "../api/writes"
import { ChoiceMenu } from "./ChoiceMenu"

interface ExportView {
  export: {
    id: string
    request_id: string
    revision: number
    status: string
    format: string
    download_url: string | null
  } | null
  failure: { message: string } | null
}
export function ExportControl({ requestId, csrf }: { requestId?: string; csrf: string }) {
  const [view, setView] = useState<ExportView | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [format, setFormat] = useState("markdown_zip")
  const [readTick, setReadTick] = useState(0)
  const api = useRef(new HttpApi())
  api.current.csrfToken = csrf
  const identity = useRef("")
  identity.current = `${requestId || ""}:${format}`
  const generation = useRef(0)
  const store = useRef(
    (() => {
      try {
        return sessionStorage
      } catch {
        return memoryStore()
      }
    })(),
  )
  useEffect(() => {
    if (!requestId) return
    const gen = ++generation.current
    let cancelled = false,
      timer: ReturnType<typeof setTimeout> | undefined
    let failures = 0
    async function poll() {
      if (document.hidden) {
        timer = setTimeout(poll, 2000)
        return
      }
      try {
        const r = await api.current.request<ExportView>("GET", `/api/v1/requests/${requestId}/exports/latest?format=${format}`)
        if (cancelled || generation.current !== gen) return
        failures = 0
        setError("")
        setView((prev) =>
          prev?.export?.id === r.body.export?.id && (prev?.export?.revision || 0) > (r.body.export?.revision || 0) ? prev : r.body,
        )
        if (r.body.export && ["queued", "running", "retry_wait"].includes(r.body.export.status)) timer = setTimeout(poll, 2000)
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : "暂时读不到导出状态。")
          if (++failures <= 6) timer = setTimeout(poll, Math.min(15000, 1000 * 2 ** (failures - 1)))
        }
      }
    }
    void poll()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [requestId, format, busy, readTick])
  async function create() {
    if (!requestId || busy) return
    setBusy(true)
    setError("")
    const path = `/api/v1/requests/${requestId}/exports`,
      slot = `export:${requestId}:${format}`,
      body = JSON.stringify({ format })
    const originalIdentity = identity.current
    const old = pendingWrite(store.current, slot)
    const key = old?.body === body && canReplay(old, Date.now()) ? old.key : newIdempotencyKey()
    rememberWrite(store.current, {
      slot,
      key,
      path,
      body,
      startedAt: Date.now(),
      expiresAt: Date.now() + 86400000,
    })
    try {
      const r = await api.current.request<ExportView>("POST", path, { format }, key)
      if (identity.current === originalIdentity) setView(r.body)
      forgetWrite(store.current, slot)
    } catch (e) {
      if (identity.current === originalIdentity) setError(e instanceof Error ? e.message : "导出结果待确认，请先读取原资源。")
    } finally {
      setBusy(false)
    }
  }
  const current = view?.export?.format === format && view.export.request_id === requestId ? view : null
  const pending = !!current?.export && ["queued", "running", "retry_wait"].includes(current.export.status)
  return (
    <div className="export-control">
      <ChoiceMenu
        label="导出格式"
        align="end"
        value={format}
        placeholder="选择格式"
        options={[
          { id: "markdown_zip", label: "完整图文 ZIP" },
          { id: "markdown", label: "Markdown 正文" },
        ]}
        onChange={setFormat}
      />
      {current?.export?.status === "ready" && current.export.download_url ? (
        <a className="btn quiet" href={current.export.download_url} download>
          下载 ↓
        </a>
      ) : (
        <button type="button" className="btn quiet" disabled={!requestId || busy || pending} onClick={() => void create()}>
          {busy ? "正在提交" : pending ? "正在准备" : "准备下载 ↓"}
        </button>
      )}
      {error || current?.failure ? (
        <small role="status">
          {error || current?.failure?.message}
          {error ? (
            <button type="button" className="text-button" onClick={() => setReadTick((v) => v + 1)}>
              重新读取状态
            </button>
          ) : null}
        </small>
      ) : null}
    </div>
  )
}
