import { useEffect, useRef, useState } from "react"
import { ApiError } from "../api/error"
import type { HttpApi } from "../api/http"
import type { AnnouncementSettings as Settings } from "../contract/announcement"
import { AnnouncementDialog } from "../ui/Announcement"

const empty: Settings = { revision: 0, enabled: false, title: "", body: "", version: "" }

export function AnnouncementSettings({ api, onDirty, onAuthLost }: { api: HttpApi; onDirty: (dirty: boolean) => void; onAuthLost: () => void }) {
  const [saved, setSaved] = useState<Settings | null>(null)
  const [draft, setDraft] = useState<Settings>(empty)
  const [busy, setBusy] = useState<"" | "load" | "save">("load")
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [preview, setPreview] = useState(false)
  const [readTick, setReadTick] = useState(0)
  const reportDirty = useRef(onDirty)
  const authLost = useRef(onAuthLost)
  reportDirty.current = onDirty
  authLost.current = onAuthLost
  const dirty = saved !== null && (draft.enabled !== saved.enabled || draft.title !== saved.title || draft.body !== saved.body)
  const hasContent = Boolean(draft.title.trim() && draft.body.trim())

  useEffect(() => {
    reportDirty.current(dirty)
    return () => reportDirty.current(false)
  }, [dirty])

  function failed(reason: unknown) {
    if (reason instanceof ApiError && reason.status === 401) authLost.current()
    setError(reason instanceof ApiError ? reason.detail : "未能确认公告状态，请重新读取后再试。")
  }

  useEffect(() => {
    const abort = new AbortController()
    setBusy("load"); setError(""); setNotice("")
    void api.request<Settings>("GET", "/api/v1/admin/announcement", undefined, undefined, abort.signal)
      .then(({ body }) => { if (!abort.signal.aborted) { setSaved(body); setDraft(body) } })
      .catch((reason: unknown) => { if (!abort.signal.aborted) failed(reason) })
      .finally(() => { if (!abort.signal.aborted) setBusy("") })
    return () => abort.abort()
  }, [api, readTick])

  function edit(change: Partial<Settings>) {
    setDraft((value) => ({ ...value, ...change }))
    setNotice("")
  }

  async function save() {
    if (!saved || busy || !dirty) return
    setBusy("save"); setError(""); setNotice("")
    try {
      const { body } = await api.request<Settings>("PUT", "/api/v1/admin/announcement", {
        expected_revision: saved.revision, enabled: draft.enabled, title: draft.title, body: draft.body,
      })
      setSaved(body); setDraft(body)
      setNotice(body.enabled ? "公告已发布。访客进入页面时会看到，已读的同一版不会重复弹出。" : "公告已停用，内容已保存，访客不会看到。")
    } catch (reason) { failed(reason) }
    finally { setBusy("") }
  }

  return <section className="announcement-settings" aria-labelledby="announcement-settings-title">
    <header className="storage-heading">
      <div><p className="eyebrow">SITE ANNOUNCEMENT</p><h2 id="announcement-settings-title">公告</h2>
        <p className="fine">让访客进入时先看到一条消息。修改已发布内容或重新启用后，会再次提醒。</p></div>
      {saved ? <span className="announcement-state" data-enabled={saved.enabled}>{saved.enabled ? "展示中" : saved.revision ? "已停用" : "尚未发布"}</span> : null}
    </header>
    {error ? <p className="management-message error" role="alert">{error}</p> : null}
    {notice ? <p className="management-message" role="status">{notice}</p> : null}
    {!saved ? <div className="announcement-loading"><p className="fine">{busy === "load" ? "正在读取公告…" : "公告尚未读取。"}</p>
      {error ? <button type="button" className="btn quiet" disabled={Boolean(busy)} onClick={() => setReadTick((value) => value + 1)}>重新读取</button> : null}</div> :
      <form onSubmit={(event) => { event.preventDefault(); void save() }}>
        <fieldset disabled={Boolean(busy)}>
          <div className="announcement-toggle"><label className="ink-check"><input type="checkbox" checked={draft.enabled} onChange={(event) => edit({ enabled: event.target.checked })} /><span />向访客展示公告</label>
            <p className="fine">开启并保存后发布；关闭并保存后停用，保留内容。</p></div>
          <label className="desk-field"><span>公告标题</span><input value={draft.title} maxLength={120} required={draft.enabled} placeholder="例如：欢迎使用 VidLens" onChange={(event) => edit({ title: event.target.value })} /></label>
          <label className="desk-field"><span>公告正文 <small>{Array.from(draft.body).length} / 8000</small></span>
            <textarea value={draft.body} maxLength={8000} rows={9} required={draft.enabled} placeholder="写下想告诉访客的事，支持换行。" onChange={(event) => edit({ body: event.target.value })} /></label>
          <p className="fine">正文按纯文本展示，保留换行。同一浏览器关闭后不再展示同一版公告。</p>
          <div className="form-actions announcement-actions">
            <button type="submit" className="btn primary" disabled={!dirty || (draft.enabled && !hasContent)}>{busy === "save" ? "正在保存…" : draft.enabled ? "保存并发布" : "保存公告"}</button>
            <button type="button" className="btn quiet" disabled={!hasContent} onClick={() => setPreview(true)}>预览弹窗</button>
            <button type="button" className="btn quiet desk-pill" onClick={() => { setDraft(saved); setError(""); setNotice(""); setReadTick((value) => value + 1) }}>{dirty ? "撤销并重新读取" : "重新读取"}</button>
          </div>
        </fieldset>
      </form>}
    {preview ? <AnnouncementDialog preview announcement={{ ...draft, title: draft.title.trim(), body: draft.body.trim() }} onDismiss={() => setPreview(false)} /> : null}
  </section>
}
