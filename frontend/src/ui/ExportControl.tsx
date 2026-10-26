import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { canReplay, forgetWrite, memoryStore, newIdempotencyKey, pendingWrite, rememberWrite } from "../api/writes"
import { ChoiceMenu } from "./ChoiceMenu"
import { ReadingDialog } from "./ReadingDialog"

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
export function ExportControl({ requestId, csrf, open, onOpenChange }: { requestId?: string; csrf: string; open: boolean; onOpenChange: (open: boolean) => void }) {
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
    if (!requestId || !open) return
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
  }, [requestId, format, busy, readTick, open])
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
    <div className="export-control reading-export">
      <button type="button" className="btn reading-export-trigger" aria-haspopup="dialog" disabled={!requestId} onClick={() => onOpenChange(true)}>导出 <span aria-hidden="true">↓</span></button>
      <ReadingDialog open={open} title="导出图文" onClose={() => onOpenChange(false)} className="export-dialog">
      <p className="export-description">选择适合你的保存方式，准备完成后即可下载。</p>
      <p className="export-format-label">保存格式</p>
      <ChoiceMenu
        label="导出格式"
        align="end"
        value={format}
        placeholder="选择格式"
        options={[
          { id: "markdown_zip", label: "完整图文 ZIP", hint: "Markdown 正文与已有插图，打包保存" },
          { id: "markdown", label: "Markdown 正文", hint: "仅保存文字内容，适合继续编辑" },
        ]}
        onChange={setFormat}
      />
      <div className="export-download">
      <p role="status">{pending ? "正在准备文件，完成后可以在这里下载。" : current?.export?.status === "ready" && current.export.download_url ? "文件已准备好，可以下载。" : "保存当前图文的已发布内容。"}</p>
      {current?.export?.status === "ready" && current.export.download_url ? (
        <a className="btn primary" href={current.export.download_url} download>
          下载 ↓
        </a>
      ) : (
        <button type="button" className="btn primary" disabled={!requestId || busy || pending} onClick={() => void create()}>
          {busy ? "正在提交" : pending ? "正在准备" : "准备下载 ↓"}
        </button>
      )}
      </div>
      {error || current?.failure ? (
        <div className="reading-notice export-error" data-tone="error" role="alert"><h3>{error ? "暂时无法读取导出状态" : "文件准备未完成"}</h3><p>
          {error || current?.failure?.message}
          {error ? (
            <button type="button" className="text-button" onClick={() => setReadTick((v) => v + 1)}>
              重新读取状态
            </button>
          ) : null}
        </p></div>
      ) : null}
      </ReadingDialog>
    </div>
  )
}
