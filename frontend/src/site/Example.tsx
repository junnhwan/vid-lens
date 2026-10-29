import { useEffect, useMemo, useRef, useState } from "react"
import type { ActivityStep, Citation, Frame } from "../contract/types"
import { choosePassage, citationURL, formatDuration, platformLabel } from "../contract/text"
import { SiteHeader, SiteFooter } from "../ui/SiteHeader"
import { ActivityList } from "../ui/ActivityList"
import { BlockView } from "../ui/Blocks"
import { ExecutionPlan } from "../ui/ExecutionPlan"
import { Lightbox } from "../ui/Lightbox"
import { ArticleOutline } from "../ui/ArticleOutline"
import { ArticleHeader } from "../ui/ArticleHeader"
import { ArticleNotes } from "../ui/ArticleNotes"
import { ReadingDialog } from "../ui/ReadingDialog"
import { ArticleGallery } from "../ui/ArticleGallery"
import { articleImages } from "../ui/articleImages"
import { MindMap } from "../ui/MindMap"
import { ModelIdentity } from "../ui/ModelIdentity"
import { QaPanel } from "../ui/QaPanel"
import { hitsInSelection, selectionPopover } from "../ui/selection"
import { prefersReduced } from "../ui/motion"
import { catalogChoice, exampleArchive, exampleRoute, exampleShelf, type CatalogItem, type ExampleData } from "./exampleReplay"
import { useExampleQuestions } from "./useExampleQuestions"
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
  const questions = useExampleQuestions(current?.id ?? null, current?.article.title, current?.article.sections[0]?.title, current?.article.sections[0]?.id)
  const frames = useMemo(() => {
    const map = new Map((current?.frames || []).map((frame) => [frame.id, frame]))
    for (const frame of questions.frames) map.set(frame.id, frame)
    for (const step of current?.generation_steps || []) for (const frame of step.frames || []) map.set(frame.id, frame)
    return map
  }, [current, questions.frames])
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
    <main className="examples-page" data-qa={questions.open ? "open" : "closed"}>
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
          : <ExampleArticle key={current.id} data={current} choice={choice} frames={frames} questions={questions} onOpenFrame={openFrame} onCite={cite} />}
      </section> : null}
      <footer className="examples-next"><div><p className="eyebrow">YOUR NEXT READ</p><h2>下一条，换成你的视频。</h2><p>贴入链接，带上你的关注点。</p></div><a href="/" className="btn primary">开始自己的解读 <span aria-hidden="true">↗</span></a></footer>
    </main>
    <SiteFooter />
    <Lightbox canAsk={current?.qa_available === true} open={lightbox.frames.length > 0} frames={lightbox.frames} index={lightbox.index} origin={lightbox.origin} onClose={() => setLightbox({ frames: [], index: 0, origin: null })} onIndex={(index) => setLightbox((value) => ({ ...value, index }))} onAsk={(frame) => { setLightbox({ frames: [], index: 0, origin: null }); questions.askFrame(frame) }} onBroken={() => {}} />
  </>
}

function processActivity(steps: ActivityStep[]) {
  const plan = [...steps].reverse().find((step) => step.plan)?.plan ?? null
  const calls = steps.filter((step) => step.kind === "model_response").length
  const sends = steps.filter((step) => step.kind === "model_submission").length
  return {
    current_step_id: null,
    recent_steps: steps,
    has_more: false,
    history_url: "",
    history_status: "available" as const,
    history_expires_at: null,
    model_calls: calls,
    model_submissions: sends || null,
    plan,
  }
}

export function ExampleArticle({ data, choice, frames, questions, onOpenFrame, onCite }: { data: ExampleData; choice?: Choice; frames: Map<string, Frame>; questions: ReturnType<typeof useExampleQuestions>; onOpenFrame: OpenFrame; onCite: (citation: Citation) => void }) {
  const sections = data.article.sections
  const sectionKey = sections.map((item) => item.id).join("\n")
  const [section, setSection] = useState("")
  const [genOpen, setGenOpen] = useState(false)
  const [pop, setPop] = useState<{ left: number; top: number; label: string; pick: () => void } | null>(null)
  const lock = useRef<string | null>(null)
  useEffect(() => {
    const nodes = sectionKey.split("\n").filter(Boolean).map((id) => document.getElementById(id)).filter((node): node is HTMLElement => !!node)
    const observer = new IntersectionObserver((entries) => {
      if (lock.current) return
      const visible = entries.filter((entry) => entry.isIntersecting).sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0]
      if (visible) setSection(visible.target.id)
    }, { rootMargin: "-18% 0px -55% 0px", threshold: [0.15, 0.4, 0.7] })
    for (const node of nodes) observer.observe(node)
    return () => observer.disconnect()
  }, [sectionKey])
  function jump(id: string, blockId?: string) {
    setSection(id)
    lock.current = id
    const target = blockId ? [...document.querySelectorAll<HTMLElement>("#prose [data-block-id]")].find((node) => node.dataset.blockId === blockId) : document.getElementById(id)
    target?.scrollIntoView({ behavior: prefersReduced() ? "auto" : "smooth", block: "start" })
    if (target) {
      if (!target.hasAttribute("tabindex")) target.tabIndex = -1
      target.focus({ preventScroll: true })
    }
    window.setTimeout(() => { if (lock.current === id) lock.current = null }, prefersReduced() ? 0 : 900)
  }
  useEffect(() => {
    function hide() { setPop(null) }
    function sync(event: Event) {
      const target = event.target
      if (target instanceof Element && target.closest(".passage-pop")) return
      window.setTimeout(() => {
        const prose = document.getElementById("prose")
        if (!prose || data.qa_available === false) { setPop(null); return }
        const found = hitsInSelection(prose)
        if (!found.rect || !found.hits.length) { setPop(null); return }
        const result = choosePassage(found.hits, sections, questions.selectionMax)
        const { left, top } = selectionPopover(found.rect, window.innerWidth, window.innerHeight)
        if (!result.ok && result.reason === "empty") { setPop(null); return }
        const label = !result.ok && result.reason === "long" ? "选中文字过长" : !result.ok ? "选文对不上正文" : "问选中内容"
        setPop({
          left, top, label, pick: () => {
            setPop(null)
            if (!result.ok) return
            document.getSelection()?.removeAllRanges()
            questions.askPassage(result.passage)
          },
        })
      }, 0)
    }
    document.addEventListener("mouseup", sync)
    document.addEventListener("scroll", hide, true)
    return () => {
      document.removeEventListener("mouseup", sync)
      document.removeEventListener("scroll", hide, true)
    }
  }, [data.qa_available, questions, sections])
  const date = new Date(data.article.published_at).toLocaleDateString("zh-Hans")
  const source = choice?.meta || [platformLabel(data.article.source.platform), formatDuration(data.article.source.duration_ms)].filter(Boolean).join(" · ")
  const caption = [source, date].filter(Boolean).join(" · ")
  const steps = data.generation_steps || []
  const processNote = data.process_status === "partial" ? "更早的步骤已经不在记录里。" : !steps.length ? "这份示例发布时，生成过程已经不在记录里。" : ""
  const canAsk = data.qa_available !== false
  const gallery = articleImages(sections, frames)
  const imageCounts = Object.fromEntries(gallery.map((group) => [group.sectionId, group.images.length]))
  return <div className="workspace example-workspace example-paper" data-qa={questions.open ? "open" : "closed"}>
    <ArticleOutline label="目录" sections={sections} current={section} onJump={jump} imageCounts={imageCounts} />
    <div className="article-col">
    <article className="sheet example-sheet">
      <ArticleHeader title={data.article.title} meta={caption} sourceUrl={data.article.source.canonical_url} onDetails={() => setGenOpen(true)}>
          <div className="article-browse"><ArticleGallery groups={gallery} onOpenFrame={onOpenFrame} onLocate={jump} onBroken={() => {}} /></div>
          <div className="article-actions">
          <button type="button" id="toggle-qa" className="btn toggle" aria-pressed={questions.open} onClick={questions.toggle}>问这条视频</button>
          </div>
      </ArticleHeader>
      <ReadingDialog open={genOpen} title="生成信息与过程" onClose={() => setGenOpen(false)} className="generation-dialog">
          <section className="reading-generation" aria-label="本次生成信息"><h3>本次生成</h3><ModelIdentity model={data.generation_model} purpose={data.purpose} detailLevel={data.detail_level} showHistoryNote={false} /></section>
          <ExecutionPlan activity={processActivity(steps)} completed />
          <ActivityList steps={steps} resourceStatus="completed" detailed open onOpenFrame={onOpenFrame} onBroken={() => {}} />
          {processNote ? <p className="history-note">{processNote}</p> : null}
      </ReadingDialog>
      {!data.media.can_inspect_now ? <aside className="reading-notice"><h3>视频画面暂不可用</h3><p>{data.media.can_restore_for_question ? "现在不能直接查看新的画面。提问时可以再准备这条视频。" : "现在不能查看新的画面。已有文章仍然可以读。"}</p></aside> : null}
      {data.article.mind_map ? <MindMap map={data.article.mind_map} current={section} onJump={jump} /> : null}
      <div className="prose" id="prose">{sections.map((item) => <section key={item.id}><h2 id={item.id}>{item.title}</h2>{item.blocks.map((block) =>
        <BlockView key={block.id} block={block} sectionId={item.id} frames={frames} citations={data.article.citations} onCite={onCite} onOpenFrame={onOpenFrame} onAskFrame={questions.askFrame} onBroken={() => {}} readOnly={!canAsk} />
      )}</section>)}</div>
      <ArticleNotes limitations={data.article.limitations} sections={sections} onJump={jump}>
        <div className="reading-finish-heading"><h2>对文中的内容还有疑问？</h2><button type="button" className="reading-note-link" onClick={() => jump("article-top")}>回到开头 <span aria-hidden="true">↑</span></button></div>
        <div className="reading-finish-actions"><button type="button" className="btn toggle" onClick={() => { if (!questions.open) questions.toggle(); else document.getElementById("composer")?.focus({ preventScroll: true }) }}>问这条视频</button></div>
      </ArticleNotes>
    </article>
    </div>
    <div className="qa-slot">
      <QaPanel
        open={questions.open}
        turns={questions.turns}
        pendingQuestion={questions.pendingQuestion}
        onRestoreQuestion={questions.restoreQuestion}
        stepsFor={questions.stepsFor}
        frames={frames}
        draft={questions.draft}
        frameChip={questions.frameChip}
        passageChip={questions.passageChip}
        prompts={questions.prompts}
        busy={questions.busy}
        error={questions.error}
        expanded={questions.expanded}
        focusToken={questions.focusToken}
        onDraft={questions.setDraft}
        onSend={questions.send}
        onStop={questions.stop}
        onRetry={questions.retry}
        onClose={questions.close}
        onToggle={questions.toggleTurn}
        onSuggest={questions.suggest}
        onRemoveFrame={questions.removeFrame}
        onRemovePassage={questions.removePassage}
        onOpenFrame={onOpenFrame}
        onCite={onCite}
        onBroken={() => {}}
        externalAllowed={questions.externalAllowed}
        externalAvailable={questions.externalAvailable}
        searchAvailable={questions.searchAvailable}
        referenceLinks={questions.referenceLinks}
        onExternal={questions.setExternal}
        onReferenceLinks={questions.setReferenceLinks}
        modelUnavailable={data.qa_available === false}
        unavailableNote="原模型暂不可用，问题草稿已保留。这篇图文仍可阅读。"
      />
    </div>
    <button type="button" className="passage-pop" hidden={!pop} style={pop ? { left: pop.left, top: pop.top } : undefined} onMouseDown={(event) => { event.preventDefault(); pop?.pick() }}>{pop?.label || "问这段"}</button>
  </div>
}
