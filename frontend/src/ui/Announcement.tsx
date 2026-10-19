import { useEffect, useId, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import type { AnnouncementResponse, PublicNotice } from "../contract/announcement"
import { announcementMemory } from "./announcementMemory"

const memory = announcementMemory(() => localStorage)

export function AnnouncementDialog({ announcement, preview = false, earlier = false, onDismiss }: {
  announcement: PublicNotice
  preview?: boolean
  earlier?: boolean
  onDismiss: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const title = useRef<HTMLHeadingElement>(null)
  const titleId = useId()
  const bodyId = useId()

  useEffect(() => {
    const node = dialog.current
    if (!node) return
    const previous = document.activeElement
    const overflow = document.documentElement.style.overflow
    document.documentElement.style.overflow = "hidden"
    node.showModal()
    title.current?.focus({ preventScroll: true })
    return () => {
      node.close()
      document.documentElement.style.overflow = overflow
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus({ preventScroll: true })
    }
  }, [])

  return <dialog ref={dialog} className="announcement-dialog" aria-labelledby={titleId} aria-describedby={bodyId}
    onCancel={(event) => { event.preventDefault(); onDismiss() }}
    onKeyDown={(event) => {
      if (event.key !== "Tab") return
      const first = event.currentTarget.querySelector<HTMLButtonElement>(".announcement-close")
      const last = event.currentTarget.querySelector<HTMLButtonElement>(".announcement-footer button")
      if (event.shiftKey && (document.activeElement === first || document.activeElement === title.current)) {
        event.preventDefault(); last?.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault(); first?.focus()
      }
    }}>
    <header className="announcement-head">
      <div><p className="eyebrow">VIDLENS · {preview ? "公告预览" : "网站公告"}</p><h2 id={titleId} ref={title} tabIndex={-1}>{announcement.title}</h2></div>
      <button type="button" className="announcement-close" aria-label={preview ? "关闭公告预览" : "关闭公告"} onClick={onDismiss}>×</button>
    </header>
    <div id={bodyId} className="announcement-body" tabIndex={0}>{announcement.body}</div>
    <footer className="announcement-footer">
      <p>{preview ? "预览不会发布，也不会影响访客的已读状态。" : "同一版公告，读过便不再打扰。"}</p>
      <div className="announcement-footer-actions">
        {earlier && !preview ? <a className="btn quiet" href="/announcements">更早的公告</a> : null}
        <button type="button" className="btn primary" onClick={onDismiss}>{preview ? "返回编辑" : "知道了，继续"}<span aria-hidden="true"> →</span></button>
      </div>
    </footer>
  </dialog>
}

export function VisitorAnnouncement() {
  const [announcement, setAnnouncement] = useState<PublicNotice | null>(null)
  const [earlier, setEarlier] = useState(false)
  useEffect(() => {
    const abort = new AbortController()
    const timeout = window.setTimeout(() => abort.abort(), 8000)
    const api = new HttpApi()
    void api.request<AnnouncementResponse>("GET", "/api/v1/site/announcement", undefined, undefined, abort.signal)
      .then(({ body }) => {
        if (abort.signal.aborted) return
        setEarlier((body?.timeline?.length || 0) > 1)
        const value = body?.announcement
        if (value?.version && value.title && value.body && !memory.hasRead(value.version)) setAnnouncement(value)
      })
      .catch(() => { /* The rest of the site remains usable if the notice cannot load. */ })
      .finally(() => window.clearTimeout(timeout))
    return () => { abort.abort(); window.clearTimeout(timeout) }
  }, [])

  if (!announcement) return null
  return <AnnouncementDialog earlier={earlier} announcement={announcement} onDismiss={() => { memory.markRead(announcement.version); setAnnouncement(null) }} />
}
