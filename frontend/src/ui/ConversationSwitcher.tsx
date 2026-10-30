import { useEffect, useRef, useState } from "react"
import type { ConversationHistoryControls } from "../app/useConversationHistory"

export function ConversationSwitcher({ history, currentID, title, disabled }: {
  history: ConversationHistoryControls
  currentID: string | undefined
  title: string
  disabled: boolean
}) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null), toggle = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    const dismiss = (event: PointerEvent) => { if (root.current && event.target instanceof Node && !root.current.contains(event.target)) setOpen(false) }
    window.addEventListener("pointerdown", dismiss)
    return () => window.removeEventListener("pointerdown", dismiss)
  }, [open])
  useEffect(() => { setOpen(false) }, [currentID])
  return <div className="qa-thread-bar" ref={root} onKeyDown={event => { if (event.key === "Escape" && open) { event.stopPropagation(); setOpen(false); toggle.current?.focus() } }}>
    <span className="qa-thread-title" title={title}>{title}</span>
    <button type="button" className="qa-thread-action" ref={toggle} aria-expanded={open} aria-controls="qa-conversation-history" disabled={disabled || !currentID} onClick={() => { if (!open) history.refresh(); setOpen(value => !value) }}>历史会话</button>
    <button type="button" className="qa-thread-action" disabled={disabled || !currentID} onClick={() => { setOpen(false); history.create() }}>＋ 新会话</button>
    {open ? <section id="qa-conversation-history" className="qa-thread-history" aria-label="当前视频的历史会话">
      <div className="qa-thread-history-head"><strong>当前视频的会话</strong><span>仅当前浏览器可见</span></div>
      {history.items.map(item => <button key={item.id} type="button" className="qa-thread-item" aria-current={item.id === currentID ? "true" : undefined} disabled={disabled} onClick={() => { setOpen(false); history.select(item.id) }}>
        <span>{item.title || "新会话"}</span>
        <small>{item.active_turn_id ? "回答中 · " : ""}{item.turn_count} 轮 · <time dateTime={item.created_at}>{new Date(item.created_at).toLocaleString("zh-CN", {month:"numeric",day:"numeric",hour:"2-digit",minute:"2-digit"})}</time>{item.id === currentID ? " · 当前" : ""}</small>
      </button>)}
      {history.loading ? <p className="meta" role="status">正在读取会话…</p> : null}
      {!history.items.length && !history.error && !history.loading ? <p className="meta">暂无其他会话。</p> : null}
      {history.hasMore ? <button type="button" className="qa-thread-action" disabled={history.loading} onClick={history.more}>更早的会话</button> : null}
      {history.error ? <p className="form-error" role="alert">{history.error}<button type="button" className="qa-thread-action" onClick={history.refresh}>重新读取</button></p> : null}
    </section> : null}
    {history.busy ? <span className="sr-only" role="status">正在切换会话…</span> : null}
    {!open && history.error ? <p className="qa-thread-error form-error" role="alert">{history.error}</p> : null}
  </div>
}
