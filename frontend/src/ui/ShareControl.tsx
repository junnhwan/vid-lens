import { useEffect, useId, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import type { RequestShare } from "../contract/types"

export function ShareControl({ requestId, csrf, disabled = false }: { requestId?: string; csrf: string; disabled?: boolean }) {
  const [open, setOpen] = useState(false)
  const [share, setShare] = useState<RequestShare | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [readTick, setReadTick] = useState(0)
  const dialog = useRef<HTMLDialogElement>(null)
  const field = useRef<HTMLInputElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const api = useRef(new HttpApi())
  const generation = useRef(0)
  const titleId = useId()
  api.current.csrfToken = csrf
  const link = share?.enabled && share.url ? new URL(share.url, location.origin).href : ""

  useEffect(() => {
    if (!open) return
    const node = dialog.current
    node?.showModal()
    return () => { node?.close(); button.current?.focus() }
  }, [open])

  useEffect(() => { setOpen(false); setShare(null) }, [requestId])

  useEffect(() => {
    if (!open || !requestId) return
    const current = ++generation.current
    setBusy(true)
    setShare(null)
    setError("")
    setNotice("")
    void api.current.requestShare(requestId)
      .then((value) => { if (generation.current === current) setShare(value) })
      .catch(() => { if (generation.current === current) setError("暂时无法读取分享状态，请重试。") })
      .finally(() => { if (generation.current === current) setBusy(false) })
    return () => { generation.current++ }
  }, [open, requestId, readTick])

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      setNotice("分享链接已复制。")
    } catch {
      setNotice("请选中下方链接并复制。")
      field.current?.focus()
      field.current?.select()
    }
  }

  async function change(enabled: boolean) {
    if (!requestId || busy || !share) return
    const current = generation.current
    setBusy(true)
    setError("")
    setNotice("")
    try {
      const value = await api.current.requestShare(requestId, enabled)
      if (generation.current !== current) return
      setShare(value)
      if (value.enabled && value.url) await copy(new URL(value.url, location.origin).href)
      else setNotice("已取消分享，原链接已失效。")
    } catch {
      if (generation.current === current) {
        setShare(null)
        setError("操作结果待确认，请重新读取分享状态。")
      }
    } finally {
      if (generation.current === current) setBusy(false)
    }
  }

  return <div className="share-control">
    <button ref={button} type="button" className="btn quiet" disabled={disabled || !requestId || !csrf} aria-haspopup="dialog" onClick={() => setOpen(true)}>分享</button>
    <dialog ref={dialog} className="share-dialog" aria-labelledby={titleId} onCancel={() => setOpen(false)} onClose={() => setOpen(false)}>
      <div className="share-dialog-head"><h2 id={titleId}>分享这条记录</h2><button type="button" className="text-button" aria-label="关闭分享窗口" onClick={() => setOpen(false)}>关闭</button></div>
      <p className="share-description">获得链接的人可以查看这一次的图文和处理过程。私人追问不会公开。</p>
      {busy && !share ? <p role="status">正在读取分享状态…</p> : null}
      {link ? <>
        <label className="share-link-label">只读分享链接<input ref={field} value={link} readOnly onFocus={(event) => event.currentTarget.select()} /></label>
        <div className="share-actions"><button type="button" className="btn primary" disabled={busy} onClick={() => void copy(link)}>复制链接</button><a className="btn quiet" href={link} target="_blank" rel="noopener noreferrer">打开分享页 ↗</a></div>
        <button type="button" className="text-button share-revoke" disabled={busy} onClick={() => void change(false)}>{busy ? "正在取消…" : "取消分享"}</button>
      </> : share ? <button type="button" className="btn primary" disabled={busy} onClick={() => void change(true)}>{busy ? "正在创建…" : "创建分享链接"}</button> : null}
      {error ? <p className="form-error" role="alert">{error} <button type="button" className="text-button" disabled={busy} onClick={() => setReadTick((value) => value + 1)}>重新读取</button></p> : null}
      {notice ? <p className="share-notice" role="status">{notice}</p> : null}
      <p className="share-footnote">取消后原链接失效；重新分享会生成新链接。处理过程沿用原有保留期限。</p>
    </dialog>
  </div>
}
