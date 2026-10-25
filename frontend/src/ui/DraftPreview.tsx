import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import type { Article, Citation, Frame } from "../contract/types"
import { BlockView } from "./Blocks"

interface SavedDraft { draft: { version: number; article: Article; frames: Frame[]; complete: false } | null }

export function DraftPreview({ requestId, status, onOpenFrame, onBroken }: {
  requestId: string
  status: string
  onOpenFrame: (frames: Frame[], index: number, origin: HTMLElement) => void
  onBroken: (frame: Frame) => void
}) {
  const [draft, setDraft] = useState<SavedDraft["draft"]>(null)
  const api = useRef(new HttpApi())
  useEffect(() => {
    let disposed = false
    let timer: ReturnType<typeof setTimeout> | undefined
    async function poll() {
      try {
        if (!document.hidden) {
          const r = await api.current.request<SavedDraft>("GET", `/api/v1/requests/${requestId}/draft`)
          if (disposed) return
          setDraft((old) => old && r.body.draft && old.version > r.body.draft.version ? old : r.body.draft)
        }
      } catch { /* The saved preview is optional; generation keeps its own error state. */ }
      if (!disposed && ["waiting_source", "generating"].includes(status)) timer = setTimeout(poll, 5000)
    }
    void poll()
    return () => { disposed = true; clearTimeout(timer) }
  }, [requestId, status])
  if (!draft) return null
  const frames = new Map(draft.frames.map((frame) => [frame.id, frame]))
  function cite(citation: Citation) {
    const url = new URL(draft!.article.source.canonical_url)
    if (!["https:", "http:"].includes(url.protocol)) return
    const start = citation.range?.start_ms ?? citation.time_ms
    if (start != null) url.searchParams.set("t", String(Math.floor(start / 1000)))
    window.open(url.href, "_blank", "noopener,noreferrer")
  }
  return <details className="saved-draft">
    <summary>查看已保存的 {draft.article.sections.length} 章草稿（尚未完成）</summary>
    <p className="history-note">后续内容仍在生成；中断后可以从已保存进度继续。</p>
    <div className="prose saved-draft-body">
      <h2>{draft.article.title}</h2>
      {draft.article.sections.map((section) => <section key={section.id}>
        <h3>{section.title}</h3>
        {section.blocks.map((block) => <BlockView key={block.id} block={block} sectionId={section.id} citations={draft.article.citations} frames={frames} onOpenFrame={onOpenFrame} onAskFrame={() => {}} onCite={cite} onBroken={onBroken} readOnly />)}
      </section>)}
    </div>
  </details>
}
