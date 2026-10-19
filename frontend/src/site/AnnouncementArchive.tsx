import { useEffect, useState } from "react"
import type { AnnouncementResponse, PublicNotice } from "../contract/announcement"
import { SiteFooter, SiteHeader } from "../ui/SiteHeader"

function stamp(value: string | undefined) {
  if (!value) return ""
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", month: "long", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false })
}

export function AnnouncementArchive() {
  const [feed, setFeed] = useState<AnnouncementResponse | null>(null)
  const [failed, setFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    const abort = new AbortController()
    setFailed(false)
    void fetch("/api/v1/site/announcement", { signal: abort.signal, headers: { Accept: "application/json" } })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<AnnouncementResponse> })
      .then((body) => { if (!abort.signal.aborted) setFeed({ announcement: body?.announcement ?? null, timeline: Array.isArray(body?.timeline) ? body.timeline : [] }) })
      .catch(() => { if (!abort.signal.aborted) setFailed(true) })
    return () => abort.abort()
  }, [attempt])
  const timeline = feed?.timeline ?? []
  const current = feed?.announcement?.version
  return <>
    <SiteHeader />
    <main className="announcement-archive">
      <header>
        <p className="eyebrow">VIDLENS · 公告</p>
        <h1>网站公告</h1>
        <p className="fine">按时间从新到旧。进入网站时只弹出最新一条还没读过的公告。</p>
      </header>
      {failed ? <p className="announcement-archive-error" role="alert">公告暂时没能读出来。<button type="button" className="btn" onClick={() => setAttempt((value) => value + 1)}>重新读取</button></p> : null}
      {feed && !timeline.length && !failed ? <p className="fine">还没有公告。</p> : null}
      <ol>
        {timeline.map((item) => <AnnouncementItem key={item.version || item.title} item={item} current={item.version === current} />)}
      </ol>
    </main>
    <SiteFooter />
  </>
}

function AnnouncementItem({ item, current }: { item: PublicNotice; current: boolean }) {
  return <li data-current={current ? "true" : undefined}>
    <div className="announcement-archive-rail" aria-hidden="true"><i /></div>
    <article>
      <div className="announcement-archive-meta">
        <time dateTime={item.published_at}>{stamp(item.published_at)}</time>
        {current ? <span className="announcement-now">当前</span> : null}
      </div>
      <h2>{item.title}</h2>
      <p>{item.body}</p>
    </article>
  </li>
}
