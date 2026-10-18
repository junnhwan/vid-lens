import { useEffect, useMemo, useState } from "react"
import type { Citation, Frame } from "../contract/types"
import { citationURL, formatDuration, platformLabel } from "../contract/text"
import { SiteHeader, SiteFooter } from "../ui/SiteHeader"
import { ArticleOutline } from "../ui/ArticleOutline"
import { BlockView } from "../ui/Blocks"
import { Lightbox } from "../ui/Lightbox"
import { MindMap } from "../ui/MindMap"
import { ModelIdentity } from "../ui/ModelIdentity"
import { prefersReduced } from "../ui/motion"
import { catalogChoice, exampleArchive, exampleRoute, exampleShelf, type CatalogItem, type ExampleData } from "./exampleReplay"
import "../styles/example.css"

type OpenFrame = (frames: Frame[], index: number, origin: HTMLElement) => void
type Choice = ReturnType<typeof catalogChoice>

function Cover({ src }: { src: string }) {
  const [broken, setBroken] = useState(false)
  if (!src || broken) return <span className="example-cover-fallback" aria-hidden="true" />
  return <img src={src} alt="" onError={() => setBroken(true)} />
}

export function Example() {
  const [route, setRoute] = useState(() => exampleRoute(location.search))
  const [data, setData] = useState<ExampleData | null>(null)
  const [error, setError] = useState("")
  const [retry, setRetry] = useState(0)
  const [lightbox, setLightbox] = useState<{ frames: Frame[]; index: number; origin: HTMLElement | null }>({ frames: [], index: 0, origin: null })
  const [published, setPublished] = useState<Choice[]>([])
  const [catalogState, setCatalogState] = useState<"loading" | "ready" | "error">("loading")
  const [catalogRetry, setCatalogRetry] = useState(0)
  const [moreOpen, setMoreOpen] = useState(false)
  useEffect(() => {
    const abort = new AbortController()
    setCatalogState("loading")
    void fetch("/api/v1/examples", { signal: abort.signal })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<{ items?: CatalogItem[] }> })
      .then((body) => {
        if (abort.signal.aborted) return
        const items = Array.isArray(body.items) ? body.items : []
        setPublished(items.filter((item) => item && /^[a-f0-9]{32}$/.test(item.id) && item.title).map(catalogChoice))
        setCatalogState("ready")
      })
      .catch(() => { if (!abort.signal.aborted) setCatalogState("error") })
    return () => abort.abort()
  }, [catalogRetry])
  useEffect(() => {
    const onPop = () => { setRoute(exampleRoute(location.search)); setLightbox({ frames: [], index: 0, origin: null }) }
    window.addEventListener("popstate", onPop)
    return () => window.removeEventListener("popstate", onPop)
  }, [])
  useEffect(() => {
    if (route.id || catalogState !== "ready" || published.length === 0) return
    const query = new URLSearchParams(location.search)
    query.delete("view")
    query.delete("question")
    query.set("video", published[0].id)
    const search = query.toString()
    history.replaceState(null, "", `/example?${search}`)
    setRoute(exampleRoute(`?${search}`))
  }, [catalogState, published, route.id])
  useEffect(() => {
    const query = new URLSearchParams(location.search)
    if (!query.has("view") && !query.has("question")) return
    query.delete("view")
    query.delete("question")
    const search = query.toString()
    history.replaceState(null, "", search ? `/example?${search}` : "/example")
  }, [route.id])
  useEffect(() => {
    if (!route.id) { setData(null); setError(""); return }
    const abort = new AbortController()
    setError("")
    void fetch(exampleArchive(route.id), { signal: abort.signal })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<ExampleData> })
      .then((value) => {
        if (abort.signal.aborted) return
        if (value.id !== route.id || !value.article?.sections) throw new Error()
        setData(value)
        document.title = `${value.article.title} · 真实示例 · VidLens`
      })
      .catch(() => { if (!abort.signal.aborted) setError("这篇示例暂时没能读出来，请再试一次。") })
    return () => abort.abort()
  }, [route.id, retry])
  const shelf = exampleShelf(published, route.id)
  useEffect(() => {
    if (exampleShelf(published, route.id).rest.some((item) => item.id === route.id)) setMoreOpen(true)
  }, [route.id, published])
  const current = data?.id === route.id ? data : null
  const choice = published.find((item) => item.id === route.id)
  const frames = useMemo(() => new Map((current?.frames || []).map((frame) => [frame.id, frame])), [current])
  function navigate(id: string) {
    if (id === route.id) return
    history.pushState(null, "", `/example?video=${id}`)
    setRoute({ id })
    setLightbox({ frames: [], index: 0, origin: null })
    document.getElementById("example-stage")?.scrollIntoView({ behavior: prefersReduced() ? "auto" : "smooth", block: "start" })
  }
  function cite(citation: Citation) {
    if (!current) return
    const url = citationURL(citation, current.article.source.canonical_url)
    if (url) window.open(url, "_blank", "noopener,noreferrer")
  }
  const openFrame: OpenFrame = (items, index, origin) => setLightbox({ frames: items, index, origin })
  return <>
    <SiteHeader />
    <main className="examples-page">
      <header className="examples-intro">
        <p className="eyebrow"><span aria-hidden="true" />真实示例</p>
        {shelf.rest.length ? <button type="button" className="example-more" aria-expanded={moreOpen} onClick={() => setMoreOpen((value) => !value)}>{moreOpen ? "收起" : "更多"}</button> : null}
      </header>
      {catalogState === "error" && !route.id ? <div className="example-status" role="alert"><p>公开示例暂时没能读出来。</p><button type="button" className="btn" onClick={() => setCatalogRetry((value) => value + 1)}>重新读取</button></div>
        : catalogState === "ready" && published.length === 0 && !route.id ? <div className="example-empty"><h1>这里还没有放出来的示例。</h1><a href="/" className="btn primary">开始自己的解读</a></div>
        : catalogState === "loading" && !route.id ? <p className="example-status" role="status">正在读取公开示例…</p>
        : null}
      {shelf.strip.length ? <ul className="example-strip" aria-label="其他公开示例">{shelf.strip.map((item) =>
        <li key={item.id}><button type="button" className="example-strip-item" aria-current={item.id === route.id ? "true" : undefined} onClick={() => navigate(item.id)}>
          <Cover src={item.cover} />
          <strong>{item.title}</strong>
          <span>{item.meta}</span>
        </button></li>)}</ul> : null}
      {moreOpen && shelf.rest.length ? <ul className="example-more-list">{shelf.rest.map((item) =>
        <li key={item.id}><button type="button" aria-current={item.id === route.id ? "true" : undefined} onClick={() => navigate(item.id)}><strong>{item.title}</strong><span>{item.meta}</span></button></li>)}</ul> : null}
      {route.id ? <section id="example-stage" className="example-stage" aria-label="示例图文">
        {error ? <div className="example-status" role="alert"><p>{error}</p><button type="button" className="btn" onClick={() => setRetry((value) => value + 1)}>重新读取</button></div>
          : !current ? <p className="example-status" role="status">正在打开这份图文…</p>
          : <ExampleArticle key={current.id} data={current} choice={choice} frames={frames} onOpenFrame={openFrame} onCite={cite} />}
      </section> : null}
      <footer className="examples-next"><div><p className="eyebrow">YOUR NEXT READ</p><h2>下一条，换成你的视频。</h2><p>贴入链接，带上你的关注点。</p></div><a href="/" className="btn primary">开始自己的解读 <span aria-hidden="true">↗</span></a></footer>
    </main>
    <SiteFooter />
    <Lightbox canAsk={false} readOnly open={lightbox.frames.length > 0} frames={lightbox.frames} index={lightbox.index} origin={lightbox.origin} onClose={() => setLightbox({ frames: [], index: 0, origin: null })} onIndex={(index) => setLightbox((value) => ({ ...value, index }))} onAsk={() => {}} onBroken={() => {}} />
  </>
}

function ExampleArticle({ data, choice, frames, onOpenFrame, onCite }: { data: ExampleData; choice?: Choice; frames: Map<string, Frame>; onOpenFrame: OpenFrame; onCite: (citation: Citation) => void }) {
  const [section, setSection] = useState("article-top")
  const [coverBroken, setCoverBroken] = useState(false)
  function jump(id: string) {
    setSection(id)
    document.getElementById(id)?.scrollIntoView({ behavior: prefersReduced() ? "auto" : "smooth", block: "start" })
  }
  const date = new Date(data.article.published_at).toLocaleDateString("zh-Hans")
  const source = choice?.meta || [platformLabel(data.article.source.platform), formatDuration(data.article.source.duration_ms)].filter(Boolean).join(" · ")
  const caption = [source, date].filter(Boolean).join(" · ")
  const cover = choice?.cover && !coverBroken ? choice.cover : ""
  return <div className="example-reading example-paper">
    <ArticleOutline sections={data.article.sections} current={section} onJump={jump} />
    <article className="sheet example-sheet">
      <header className="article-head">
        <h1 id="article-top">{data.article.title}</h1>
        {cover ? <figure className="example-lead"><img src={cover} alt="" onError={() => setCoverBroken(true)} />
          <figcaption><span>{caption}</span><a href={data.article.source.canonical_url} target="_blank" rel="noopener noreferrer">打开原视频 <span aria-hidden="true">↗</span></a></figcaption>
        </figure> : <div className="example-article-meta"><p className="meta">{caption}</p><a href={data.article.source.canonical_url} target="_blank" rel="noopener noreferrer">打开原视频 <span aria-hidden="true">↗</span></a></div>}
        <ModelIdentity model={data.generation_model} showHistoryNote={false} />
      </header>
      {data.article.mind_map ? <MindMap map={data.article.mind_map} current={section} onJump={jump} /> : null}
      <div className="prose">{data.article.sections.map((item) => <section key={item.id}><h2 id={item.id}>{item.title}</h2>{item.blocks.map((block) =>
        <BlockView key={block.id} block={block} sectionId={item.id} frames={frames} citations={data.article.citations} onCite={onCite} onOpenFrame={onOpenFrame} onAskFrame={() => {}} onBroken={() => {}} readOnly />
      )}</section>)}{data.article.limitations.length ? <aside className="limits"><h2>还没确认的部分</h2>{data.article.limitations.map((item, index) => <p key={index}>{item.message}</p>)}</aside> : null}</div>
    </article>
  </div>
}
