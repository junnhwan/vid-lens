import { useEffect, useRef, useState } from "react"
import { ApiError } from "../api/error"
import type { HttpApi } from "../api/http"
import type { AnnouncementEntry, AnnouncementList } from "../contract/announcement"
import { AnnouncementDialog } from "../ui/Announcement"
import { DeskStatus } from "../ui/DeskStatus"

const tones = { current: "ready", published: "dim", withdrawn: "wait", draft: "dim" } as const
const labels = { current: "展示中", published: "已发布", withdrawn: "已撤回", draft: "尚未发布" }

function stamp(value: string | null) {
  if (!value) return "尚未发布"
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false })
}

export function AnnouncementSettings({ api, onDirty, onAuthLost }: { api: HttpApi; onDirty: (dirty: boolean) => void; onAuthLost: () => void }) {
  const [list, setList] = useState<AnnouncementList | null>(null)
  const [selected, setSelected] = useState("")
  const [title, setTitle] = useState("")
  const [body, setBody] = useState("")
  const [busy, setBusy] = useState<"" | "load" | "save">("load")
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [preview, setPreview] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const reportDirty = useRef(onDirty)
  const authLost = useRef(onAuthLost)
  reportDirty.current = onDirty
  authLost.current = onAuthLost
  const current = list?.items.find((item) => item.id === selected)
  const dirty = list !== null && (current ? title !== current.title || body !== current.body : Boolean(title.trim() || body.trim()))
  const hasContent = Boolean(title.trim() && body.trim())

  useEffect(() => {
    reportDirty.current(dirty)
    return () => reportDirty.current(false)
  }, [dirty])

  function failed(reason: unknown) {
    if (reason instanceof ApiError && reason.status === 401) authLost.current()
    setError(reason instanceof ApiError ? reason.detail : "未能确认公告状态，请重新读取后再试。")
  }

  function apply(next: AnnouncementList, prefer = selected) {
    setList(next)
    const item = next.items.find((entry) => entry.id === prefer) || next.items.find((entry) => entry.status === "current") || next.items[0]
    setSelected(item?.id || "")
    setTitle(item?.title || "")
    setBody(item?.body || "")
  }

  useEffect(() => {
    const abort = new AbortController()
    setBusy("load"); setError(""); setNotice("")
    void api.request<AnnouncementList>("GET", "/api/v1/admin/announcement", undefined, undefined, abort.signal)
      .then(({ body: next }) => { if (!abort.signal.aborted) apply({ revision: next.revision, items: Array.isArray(next.items) ? next.items : [] }, "") })
      .catch((reason: unknown) => { if (!abort.signal.aborted) failed(reason) })
      .finally(() => { if (!abort.signal.aborted) setBusy("") })
    return () => abort.abort()
  }, [api, attempt])

  function choose(id: string) {
    if (dirty) {
      setError("请先保存或撤销当前公告。")
      return
    }
    setError(""); setNotice("")
    if (!id) {
      setSelected(""); setTitle(""); setBody("")
      return
    }
    const item = list?.items.find((entry) => entry.id === id)
    if (!item) return
    setSelected(item.id); setTitle(item.title); setBody(item.body)
  }

  async function send(action: "publish" | "correct" | "archive" | "withdraw") {
    if (!list || busy) return
    setBusy("save"); setError(""); setNotice("")
    try {
      const { body: next } = await api.request<AnnouncementList>("PUT", "/api/v1/admin/announcement", {
        expected_revision: list.revision, action, id: action === "publish" && current?.status === "draft" ? current.id : action === "publish" ? "" : selected, title, body,
      })
      apply(next, action === "publish" ? "" : selected)
      const unchanged = action === "publish" && next.revision === list.revision
      setNotice(unchanged ? "这条内容已经是正在展示的公告，没有另发一条。" : action === "publish" ? "新公告已发布。访客会看到这一条，更早的公告留在时间线上。" : action === "correct" ? "已改正这条公告，不会因此再次弹出。" : action === "archive" ? "已结束弹出。这条公告仍留在时间线上。" : "已撤回。访客的时间线里不再显示这条公告。")
    } catch (reason) { failed(reason) }
    finally { setBusy("") }
  }

  const visitorState = list?.items.some((item) => item.status === "current") ? "展示中" : list?.items.length ? "没有需要弹出的公告" : "尚未发布"
  return <section className="announcement-settings" aria-labelledby="announcement-settings-title">
    <header className="storage-heading">
      <div>
        <p className="eyebrow">SITE ANNOUNCEMENT</p>
        <h2 id="announcement-settings-title">公告</h2>
        <p className="fine">每次发布追加一条。改正错字不会再次弹出；换成另一件事就写一条新公告。同一条已读后不再打断访客。</p>
      </div>
      {list ? <span className="announcement-state-row">
        <DeskStatus tone={visitorState === "展示中" ? "ready" : "dim"}>{visitorState}</DeskStatus>
        {dirty ? <DeskStatus tone="wait">有未保存修改</DeskStatus> : null}
      </span> : null}
    </header>
    {error ? <p className="management-message error" role="alert">{error}</p> : null}
    {notice ? <p className="management-message" role="status">{notice}</p> : null}
    {!list ? <div className="announcement-loading"><p className="fine">{busy === "load" ? "正在读取公告…" : "公告尚未读取。"}</p>
      {error ? <button type="button" className="btn quiet" disabled={Boolean(busy)} onClick={() => setAttempt((value) => value + 1)}>重新读取</button> : null}</div> :
      <div className="announcement-admin">
        <div className="announcement-timeline">
          <div className="announcement-timeline-head"><h3>时间线</h3><button type="button" className="btn quiet desk-pill" disabled={Boolean(busy)} onClick={() => choose("")}>写一条新公告</button></div>
          {list.items.length ? <ol>
            {list.items.map((item) => <li key={item.id}><button type="button" aria-current={item.id === selected ? "true" : undefined} onClick={() => choose(item.id)}>
              <DeskStatus tone={tones[item.status]}>{labels[item.status]}</DeskStatus>
              <time dateTime={item.published_at || undefined}>{stamp(item.published_at)}</time>
              <strong>{item.title || "未命名公告"}</strong>
            </button></li>)}
          </ol> : <p className="fine">还没有公告。写一条后，访客进入网站时会看到。</p>}
        </div>
        <form onSubmit={(event) => { event.preventDefault(); void send(current && current.status !== "draft" ? "correct" : "publish") }}>
          <fieldset disabled={Boolean(busy)}>
            {current ? <DeskStatus tone={tones[current.status]}>{labels[current.status]}</DeskStatus> : <DeskStatus tone="dim">新公告</DeskStatus>}
            <label className="desk-field"><span>公告标题</span><input value={title} maxLength={120} placeholder="例如：欢迎使用 VidLens" onChange={(event) => { setTitle(event.target.value); setNotice("") }} /></label>
            <label className="desk-field"><span>公告正文 <small>{Array.from(body).length} / 8000</small></span>
              <textarea value={body} maxLength={8000} rows={8} placeholder="写下想告诉访客的事，支持换行。" onChange={(event) => { setBody(event.target.value); setNotice("") }} /></label>
            <article className="announcement-preview-card" aria-label="公告预览">
              <p className="eyebrow">访客看到的样子</p>
              <h3>{title.trim() || "公告标题"}</h3>
              <p>{body.trim() || "正文会按纯文本展示，并保留换行。"}</p>
            </article>
            <div className="form-actions announcement-actions">
              <button type="submit" className="btn primary" disabled={current && current.status !== "draft" ? !dirty || !hasContent : !hasContent}>{busy === "save" ? "正在保存…" : current && current.status !== "draft" ? "保存改正" : "发布给访客"}</button>
              {current?.status === "current" ? <button type="button" className="btn quiet" disabled={dirty} onClick={() => void send("archive")}>结束展示</button> : null}
              {current && current.status !== "withdrawn" ? <button type="button" className="btn quiet" disabled={dirty} onClick={() => void send("withdraw")}>撤回</button> : null}
              <button type="button" className="btn quiet" disabled={!hasContent} onClick={() => setPreview(true)}>预览弹窗</button>
              <button type="button" className="btn quiet desk-pill" onClick={() => { setError(""); setNotice(""); if (dirty && current) { setTitle(current.title); setBody(current.body) } else setAttempt((value) => value + 1) }}>{dirty ? "撤销" : "重新读取"}</button>
            </div>
          </fieldset>
        </form>
      </div>}
    {preview ? <AnnouncementDialog preview announcement={{ version: current?.version || "", title: title.trim(), body: body.trim() }} onDismiss={() => setPreview(false)} /> : null}
  </section>
}

export type { AnnouncementEntry }
