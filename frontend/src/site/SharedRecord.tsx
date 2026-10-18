import { useEffect, useMemo, useState } from "react"
import { loadSharedRecord } from "../api/shares"
import { ApiError } from "../api/error"
import type { Citation, Frame, SharedRecordResponse } from "../contract/types"
import { formatDuration, platformLabel } from "../contract/text"
import { SiteHeader, SiteFooter } from "../ui/SiteHeader"
import { ActivityList } from "../ui/ActivityList"
import { BlockView } from "../ui/Blocks"
import { ExecutionPlan } from "../ui/ExecutionPlan"
import { Lightbox } from "../ui/Lightbox"
import { MindMap } from "../ui/MindMap"
import { ModelIdentity } from "../ui/ModelIdentity"

function statusLabel(status: string): string {
  return ({ waiting_source: "准备来源", generating: "处理中", completed: "已完成", failed: "未完成", canceled: "已停止" } as Record<string, string>)[status] || "状态未知"
}

export function SharedRecord() {
  const token = location.pathname.match(/^\/s\/([^/]+)\/?$/)?.[1] || ""
  const [data, setData] = useState<SharedRecordResponse | null>(null)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [unavailable, setUnavailable] = useState(false)
  const [retry, setRetry] = useState(0)

  useEffect(() => {
    const abort = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    let reading = false
    let revoked = false
    setLoading(true)
    setUnavailable(false)
    async function read() {
      if (reading || revoked || abort.signal.aborted) return
      if (timer) clearTimeout(timer)
      if (document.hidden) { timer = setTimeout(read, 12000); return }
      reading = true
      try {
        const value = await loadSharedRecord(token, abort.signal)
        if (abort.signal.aborted) return
        setData(value)
        setError("")
        document.title = `${value.article?.article.title || value.view.source?.title || "分享的处理记录"} · VidLens`
      } catch (cause) {
        if (abort.signal.aborted) return
        if (cause instanceof ApiError && cause.status === 404) {
          setData(null)
          setUnavailable(true)
          revoked = true
          document.title = "分享不可用 · VidLens"
        }
        setError(cause instanceof Error ? cause.message : "暂时无法读取分享内容。")
      } finally {
        reading = false
        if (!abort.signal.aborted) {
          setLoading(false)
          if (!revoked) timer = setTimeout(read, 12000)
        }
      }
    }
    const onVisible = () => { if (!document.hidden) void read() }
    void read()
    document.addEventListener("visibilitychange", onVisible)
    window.addEventListener("focus", onVisible)
    return () => { abort.abort(); clearTimeout(timer); document.removeEventListener("visibilitychange", onVisible); window.removeEventListener("focus", onVisible) }
  }, [token, retry])

  return <>
    <a className="skip" href="#shared-main">跳到内容</a>
    <SiteHeader />
    <main className="shared-page" id="shared-main">
      {error ? <div className="shared-state" role="alert"><h1>{unavailable ? "这条分享已不可用" : "读取分享时遇到问题"}</h1><p>{error}</p><button type="button" className="btn quiet" onClick={() => setRetry((value) => value + 1)} disabled={loading}>{loading ? "正在读取…" : "重新读取"}</button></div> : null}
      {loading && !data && !error ? <p className="shared-state" role="status">正在读取分享内容…</p> : null}
      {data ? <SharedContent key={token} data={data} /> : null}
    </main>
    <SiteFooter />
  </>
}

export function SharedContent({ data }: { data: SharedRecordResponse }) {
  const [lightbox, setLightbox] = useState<{ frames: Frame[]; index: number; origin: HTMLElement | null }>({ frames: [], index: 0, origin: null })
  const [broken, setBroken] = useState<Set<string>>(() => new Set())
  const article = data.article?.article
  const frames = useMemo(() => {
    const map = new Map<string, Frame>()
    for (const frame of [...data.steps.flatMap((step) => step.frames || []), ...(data.article?.frames || [])]) {
      map.set(frame.id, broken.has(frame.id) ? { ...frame, availability: "expired", urls: { full: null, thumbnail: null } } : frame)
    }
    return map
  }, [data, broken])
  function openFrames(items: Frame[], index: number, origin: HTMLElement) { setLightbox({ frames: items, index, origin }) }
  function onBroken(frame: Frame) { setBroken((current) => new Set([...current, frame.id])) }
  function cite(citation: Citation) {
    const frame = citation.frame_id ? frames.get(citation.frame_id) : undefined
    if (frame) setLightbox({ frames: [frame], index: 0, origin: null })
  }
  function jump(id: string) { document.getElementById(id)?.scrollIntoView({ behavior: "auto", block: "start" }) }
  const status = data.view.request.status
  const running = status === "waiting_source" || status === "generating"
  const source = article?.source || data.view.source
  const meta = [platformLabel(source?.platform), formatDuration(source?.duration_ms), source?.part?.label].filter(Boolean).join(" · ")
  const process = <>
    <ExecutionPlan activity={data.view.activity} completed={status === "completed"} />
    <ActivityList steps={data.steps} resourceStatus={status} detailed open onOpenFrame={openFrames} onBroken={onBroken} />
    {data.view.activity.history_status === "expired" || data.view.activity.history_status === "partial" ? <p className="history-note">部分处理过程已超过保留期限；已有图文仍可阅读。</p> : null}
    {!data.steps.length && data.view.activity.history_status !== "expired" ? <p className="history-note">{running ? "处理步骤会在这里自动更新。" : "这条记录没有保留新的处理步骤。"}</p> : null}
  </>

  return <>
    <div className="share-banner"><span>分享的处理记录 <span className="shared-status" data-status={status}>{statusLabel(status)}</span></span><span>只读 · 图文与处理过程</span></div>
    <article className="sheet shared-sheet" aria-readonly="true">
      <header className="article-head"><h1>{article?.title || source?.title || "视频处理记录"}</h1><div className="article-tools"><p className="meta">{meta}</p>{source?.canonical_url ? <a className="text-button" href={source.canonical_url} target="_blank" rel="noopener noreferrer">观看原视频 ↗</a> : null}</div><ModelIdentity model={data.article?.generation_model || data.view.request.generation_model} detailLevel={data.article?.detail_level || data.view.request.detail_level} showHistoryNote={false} /></header>
      {article ? <>
        <details className="shared-process"><summary>查看处理过程{data.steps.length ? ` · ${data.steps.length} 个步骤` : ""}</summary>{process}</details>
        {article.mind_map ? <MindMap map={article.mind_map} current="" onJump={jump} /> : null}
        <div className="prose">{article.sections.map((section) => <section key={section.id}><h2 id={section.id} tabIndex={-1}>{section.title}</h2>{section.blocks.map((block) => <BlockView key={block.id} block={block} sectionId={section.id} citations={article.citations || {}} frames={frames} onOpenFrame={openFrames} onAskFrame={() => {}} onCite={cite} onBroken={onBroken} readOnly />)}</section>)}
          {article.limitations?.length ? <div className="limits"><h2>还没确认的部分</h2><ul>{article.limitations.map((item, index) => <li key={index}>{item.message}</li>)}</ul></div> : null}
        </div>
      </> : <div className="shared-pending"><p role="status">{running ? "视频仍在处理中，图文完成后会自动显示。" : "这次处理没有生成可阅读的图文。"}</p>{process}</div>}
    </article>
    <Lightbox frames={lightbox.frames} index={lightbox.index} open={lightbox.frames.length > 0} origin={lightbox.origin} canAsk={false} readOnly onClose={() => setLightbox({ frames: [], index: 0, origin: null })} onIndex={(index) => setLightbox((current) => ({ ...current, index }))} onAsk={() => {}} onBroken={onBroken} />
  </>
}
