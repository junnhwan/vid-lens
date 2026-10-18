import { useEffect, useMemo, useState } from "react"
import type { ActivityStep, Citation, Frame } from "../contract/types"
import { formatClock, formatDuration, platformLabel } from "../contract/text"
import { SiteHeader, SiteFooter } from "../ui/SiteHeader"
import { ActivityList } from "../ui/ActivityList"
import { BlockView } from "../ui/Blocks"
import { Lightbox } from "../ui/Lightbox"
import { MindMap } from "../ui/MindMap"
import { ModelIdentity } from "../ui/ModelIdentity"
import { prefersReduced } from "../ui/motion"
import { catalogChoice, exampleArchive, exampleRoute, replaySteps, replayTimeline, type CatalogItem, type ExampleData, type ExampleMode, type ExampleQuestion } from "./exampleReplay"
import "../styles/example.css"

type OpenFrame = (frames: Frame[], index: number, origin: HTMLElement) => void

function useReplay(steps: ActivityStep[], initiallyComplete = false) {
  const timeline = useMemo(() => replayTimeline(steps), [steps])
  const [position, setPosition] = useState(initiallyComplete ? timeline.duration : 0)
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(1)
  const complete = position >= timeline.duration
  useEffect(() => { if (complete) setPlaying(false) }, [complete])
  useEffect(() => {
    if (!playing) return
    let previous = performance.now()
    const timer = window.setInterval(() => {
      const now = performance.now(), elapsed = now - previous
      previous = now
      setPosition((value) => Math.min(timeline.duration, value + elapsed * speed))
    }, 100)
    const onVisibility = () => { if (document.hidden) setPlaying(false) }
    document.addEventListener("visibilitychange", onVisibility)
    return () => { window.clearInterval(timer); document.removeEventListener("visibilitychange", onVisibility) }
  }, [playing, speed, timeline.duration])
  function restart() { setPosition(1); setPlaying(true) }
  return {
    position, playing, speed, complete, duration: timeline.duration,
    visible: replaySteps(steps, position),
    seek: (value: number) => setPosition(Math.max(0, Math.min(timeline.duration, value))),
    toggle: () => complete ? restart() : setPlaying((value) => !value), restart,
    finish: () => { setPosition(timeline.duration); setPlaying(false) }, setSpeed,
  }
}

function PlayIcon({ pause = false }: { pause?: boolean }) {
  return <svg viewBox="0 0 20 20" aria-hidden="true">{pause
    ? <path d="M6 4v12M14 4v12" fill="none" stroke="currentColor" strokeWidth="2.5" />
    : <path d="m7 4 9 6-9 6Z" fill="currentColor" />}</svg>
}
function ReplayControls({ replay, label }: { replay: ReturnType<typeof useReplay>; label: string }) {
  return <div className="example-player">
    <div className="example-player-actions">
      <button type="button" className="btn primary" onClick={replay.toggle}><PlayIcon pause={replay.playing} />{replay.playing ? "暂停" : replay.complete ? label : replay.position > 0 ? "继续播放" : label}</button>
      <button type="button" className="example-text-button" onClick={replay.restart} aria-label="从头播放">↺ 从头</button>
      <button type="button" className="example-speed" aria-label={`回放速度 ${replay.speed} 倍`} onClick={() => replay.setSpeed(replay.speed === 1 ? 2 : 1)}>{replay.speed}×</button>
      {!replay.complete ? <button type="button" className="example-text-button example-skip" onClick={replay.finish}>直接看结果 <span aria-hidden="true">↗</span></button> : null}
    </div>
    <label className="example-scrubber"><span className="sr-only">回放进度</span>
      <input type="range" min={0} max={Math.ceil(replay.duration)} value={Math.round(replay.position)} onChange={(event) => replay.seek(Number(event.target.value))} aria-valuetext={`${Math.round(replay.position / replay.duration * 100)}%`} />
      <span>{formatClock(replay.position)} / {formatClock(replay.duration)}</span>
    </label>
  </div>
}

export function Example() {
  const [route, setRoute] = useState(() => exampleRoute(location.search))
  const [data, setData] = useState<ExampleData | null>(null)
  const [error, setError] = useState("")
  const [retry, setRetry] = useState(0)
  const [lightbox, setLightbox] = useState<{ frames: Frame[]; index: number; origin: HTMLElement | null }>({ frames: [], index: 0, origin: null })
  const [published, setPublished] = useState<ReturnType<typeof catalogChoice>[]>([])
  const [catalogState, setCatalogState] = useState<"loading" | "ready" | "error">("loading")
  const [catalogRetry, setCatalogRetry] = useState(0)
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
    query.delete("video")
    query.set("video", published[0].id)
    const search = query.toString()
    history.replaceState(null, "", `/example?${search}`)
    setRoute(exampleRoute(`?${search}`))
  }, [catalogState, published, route.id])
  useEffect(() => {
    if (!route.id) { setData(null); setError(""); return }
    const abort = new AbortController()
    setError("")
    void fetch(exampleArchive(route.id), { signal: abort.signal })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<ExampleData> })
      .then((value) => {
        if (abort.signal.aborted) return
        if (value.id !== route.id || !value.article?.sections || !Array.isArray(value.questions)) throw new Error()
        setData(value)
        document.title = `${value.article.title} · 真实示例 · VidLens`
      })
      .catch(() => { if (!abort.signal.aborted) setError("这篇示例暂时没能读出来，请再试一次。") })
    return () => abort.abort()
  }, [route.id, retry])
  const current = data?.id === route.id ? data : null
  const selectedQuestion = current?.questions.find((question) => question.id === route.question) || current?.questions[0]
  const frames = useMemo(() => new Map((current?.frames || []).map((frame) => [frame.id, frame])), [current])
  function navigate(id: string, mode: ExampleMode, question: string | null = null, scroll = false) {
    const query = new URLSearchParams({ video: id })
    if (mode !== "article") query.set("view", mode)
    if (question) query.set("question", question)
    history.pushState(null, "", `/example?${query}`)
    setRoute({ id, mode, question }); setLightbox({ frames: [], index: 0, origin: null })
    if (scroll) document.getElementById("example-stage")?.scrollIntoView({ behavior: prefersReduced() ? "auto" : "smooth", block: "start" })
  }
  function cite(citation: Citation) {
    if (!current) return
    const url = new URL(citation.kind === "external_reference" && citation.url ? citation.url : current.article.source.canonical_url)
    if (!/^https?:$/.test(url.protocol)) return
    if (citation.kind !== "external_reference") url.searchParams.set("t", String(Math.floor((citation.time_ms ?? citation.range?.start_ms ?? 0) / 1000)))
    window.open(url.toString(), "_blank", "noopener,noreferrer")
  }
  const openFrame: OpenFrame = (items, index, origin) => setLightbox({ frames: items, index, origin })
  const articleId = route.id
  return <>
    <SiteHeader />
    <main className="examples-page">
      <header className="examples-intro"><div><p className="eyebrow">THE READING ROOM <span aria-hidden="true">/</span> 真实示例</p>
        <h1>从一条视频，<span>读到一个答案。</span></h1><p>看图文如何生成，也看看带着问题，能从画面里再读出什么。</p>
      </div><span className="examples-archive-note"><span aria-hidden="true">◌</span> 真实生成 · 公开图文</span></header>
      {catalogState === "error" && !route.id ? <div className="example-loading" role="alert"><p>公开示例暂时没能读出来。</p><button type="button" className="btn" onClick={() => setCatalogRetry((value) => value + 1)}>重新读取</button></div>
        : published.length ? <div className="example-choices" role="group" aria-label="选择真实示例">{published.map((choice, index) =>
        <button key={choice.id} type="button" className="example-choice" aria-pressed={choice.id === route.id} onClick={() => { if (choice.id !== route.id) navigate(choice.id, route.mode) }}>
          <span className="example-cover">{choice.cover ? <img src={choice.cover} alt="" /> : null}<span>{choice.meta}</span></span>
          <span className="example-choice-copy"><span className="example-choice-kicker">{String(index + 1).padStart(2, "0")}<span>{choice.tag}</span></span><strong>{choice.title}</strong><span className="example-choice-description">{choice.description}</span><span className="example-choice-state">{choice.id === route.id ? "正在浏览" : "浏览这一篇"} <span aria-hidden="true">{choice.id === route.id ? "●" : "↗"}</span></span></span>
        </button>)}</div>
        : <p className="example-loading" role="status">{catalogState === "ready" ? "还没有公开示例。选定已发布的图文之后，会出现在这里。" : "正在读取公开示例…"}</p>}
      {articleId ? <section id="example-stage" className="example-stage" aria-label="示例体验">
        <nav className="example-modes" aria-label="示例内容"><div>{([["article", "阅读图文"], ["process", "处理回放"], ["qa", "体验问答"]] as const).map(([mode, label], index) =>
          <button type="button" key={mode} aria-current={route.mode === mode ? "page" : undefined} onClick={() => navigate(articleId, mode)}><span aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>{label}</button>)}
        </div><p>浏览与回放不会发起新的生成任务</p></nav>
        {error ? <div className="example-loading" role="alert"><p>{error}</p><button type="button" className="btn" onClick={() => setRetry((value) => value + 1)}>重新读取</button></div>
          : !current ? <p className="example-loading" role="status">正在打开这份真实记录…</p>
          : <div key={current.id}>
            {route.mode === "article" ? <ExampleArticle data={current} frames={frames} onOpenFrame={openFrame} onCite={cite} onNavigate={(mode, question) => navigate(articleId, mode, question ?? null, true)} /> : null}
            {route.mode === "process" ? <GenerationReplay data={current} onOpenFrame={openFrame} onRead={() => navigate(articleId, "article", null, true)} /> : null}
            {route.mode === "qa" ? <div className="example-qa-layout"><aside className="example-question-list"><p className="eyebrow">从一个问题开始</p><h2>读完之后，<br />再多问一句。</h2><p>{current.questions.length ? "选一个真实问过的问题，直接读答案，或回看它如何找到依据。" : "这篇还没有保存过追问。图文和画面仍然可以读。"}</p>
              {current.questions.map((question) => <button type="button" key={question.id} aria-pressed={selectedQuestion?.id === question.id} onClick={() => navigate(articleId, "qa", question.id)}><span>{question.label}</span><strong>{question.question}</strong><span className="example-question-arrow" aria-hidden="true">↗</span></button>)}
              <a href="/" className="example-own-question">带着自己的视频来提问 <span aria-hidden="true">↗</span></a>
            </aside>{selectedQuestion ? <SavedQuestion key={selectedQuestion.id} data={current} question={selectedQuestion} frames={frames} onOpenFrame={openFrame} onCite={cite} /> : <article className="sheet example-saved-question"><p className="example-quality-note">这篇图文还没有保存过追问。</p></article>}</div> : null}
          </div>}
      </section> : null}
      <footer className="examples-next"><div><p className="eyebrow">YOUR NEXT READ</p><h2>下一条，换成你的视频。</h2><p>贴入链接，带上你的关注点。</p></div><a href="/" className="btn primary">开始自己的解读 <span aria-hidden="true">↗</span></a></footer>
    </main><SiteFooter />
    <Lightbox canAsk={false} readOnly open={lightbox.frames.length > 0} frames={lightbox.frames} index={lightbox.index} origin={lightbox.origin} onClose={() => setLightbox({ frames: [], index: 0, origin: null })} onIndex={(index) => setLightbox((value) => ({ ...value, index }))} onAsk={() => {}} onBroken={() => {}} />
  </>
}

function ExampleArticle({ data, frames, onOpenFrame, onCite, onNavigate }: { data: ExampleData; frames: Map<string, Frame>; onOpenFrame: OpenFrame; onCite: (citation: Citation) => void; onNavigate: (mode: ExampleMode, question?: string) => void }) {
  const [section, setSection] = useState("article-top")
  function jump(id: string) { setSection(id); document.getElementById(id)?.scrollIntoView({ behavior: prefersReduced() ? "auto" : "smooth", block: "start" }) }
  return <div className="example-reading-layout"><article className="sheet example-sheet">
    <header className="article-head"><p className="example-record-label">已发布的真实图文 <span> / {new Date(data.article.published_at).toLocaleDateString("zh-Hans")}</span></p><h1 id="article-top">{data.article.title}</h1><div className="example-article-meta"><p className="meta">{platformLabel(data.article.source.platform)} · {formatDuration(data.article.source.duration_ms)}</p><a href={data.article.source.canonical_url} target="_blank" rel="noopener noreferrer">打开原视频 ↗</a></div><ModelIdentity model={data.generation_model} showHistoryNote={false} /></header>
    {data.article.mind_map ? <MindMap map={data.article.mind_map} current={section} onJump={jump} /> : null}
    <div className="prose">{data.article.sections.map((item) => <section key={item.id}><h2 id={item.id}>{item.title}</h2>{item.blocks.map((block) => {
      const question = data.questions.find((saved) => saved.context_frame_id && block.frame_ids?.includes(saved.context_frame_id))
      return <div key={block.id}><BlockView block={block} sectionId={item.id} frames={frames} citations={data.article.citations} onCite={onCite} onOpenFrame={onOpenFrame} onAskFrame={() => {}} onBroken={() => {}} readOnly />{question ? <button type="button" className="example-figure-question" onClick={() => onNavigate("qa", question.id)}>这张图，还有一个真实追问 <span aria-hidden="true">↗</span></button> : null}</div>
    })}</section>)}{data.article.limitations.length ? <aside className="limits"><h2>还没确认的部分</h2>{data.article.limitations.map((item, index) => <p key={index}>{item.message}</p>)}</aside> : null}</div>
  </article><aside className="example-reading-aside"><div className="example-aside-sticky"><p className="eyebrow">这一篇，如何读</p><p className="example-aside-lead">文字有出处，<br />画面能细看，<br />问题还能接着问。</p>
    <button type="button" className="example-aside-action" onClick={() => onNavigate("process")}><PlayIcon /><span><strong>看一遍生成过程</strong><small>从视频处理到图文发布</small></span><span aria-hidden="true">↗</span></button>
    <button type="button" className="example-aside-action" onClick={() => onNavigate("qa")}><span aria-hidden="true">＋</span><span><strong>看看真实的追问</strong><small>文字提问与看图问答</small></span><span aria-hidden="true">↗</span></button>
    <div className="example-outline">{data.article.sections.map((item, index) => <button type="button" key={item.id} onClick={() => jump(item.id)}><span>{String(index + 1).padStart(2, "0")}</span>{item.title}</button>)}</div>
  </div></aside></div>
}

function GenerationReplay({ data, onOpenFrame, onRead }: { data: ExampleData; onOpenFrame: OpenFrame; onRead: () => void }) {
  const replay = useReplay(data.generation_steps)
  if (data.generation_steps.length === 0) {
    return <section className="example-process sheet"><header className="example-replay-head"><div><p className="eyebrow">从链接，到图文</p><h2>这篇图文已经发布。</h2><p>这次没有留下可回放的处理步骤。正文、配图和已保存的追问仍然可以读。</p></div></header><button type="button" className="example-text-button" onClick={onRead}>阅读完整图文 <span aria-hidden="true">↗</span></button></section>
  }
  const stills = [...replay.visible].reverse().find((step) => step.frames?.length)?.frames || []
  return <section className="example-process sheet"><header className="example-replay-head"><div><p className="eyebrow">从链接，到图文</p><h2>一篇解读，是怎样生成的。</h2><p>真实步骤的加速回放；等待时间已压缩，图片与结果均来自这次处理。</p></div><span className="example-record-pill">已保存的真实记录</span></header>
    <ReplayControls replay={replay} label="播放处理过程" />
    <div className="example-process-grid"><div className="example-process-log"><p className="example-process-status" role="status">{replay.complete ? "图文已发布" : replay.playing ? "正在回放" : replay.position > 0 ? "回放已暂停" : "准备好，跟着它读一遍"}<span>已回放 {replay.visible.filter((step) => step.status !== "running").length} / {data.generation_steps.length} 步</span></p>
      {replay.visible.length ? <ActivityList steps={replay.visible} resourceStatus={replay.complete ? "completed" : "running"} detailed open onOpenFrame={onOpenFrame} onBroken={() => {}} /> : <div className="example-replay-idle"><span aria-hidden="true">01 — 02 — 03</span><p>准备视频与文字<br />查看画面、寻找依据<br />整理成文并发布</p><small>点击播放，逐步展开这次真实记录。</small></div>}
    </div><div className="example-evidence-stage"><p className="eyebrow">{replay.complete ? "留下可读的结果" : "沿着画面寻找依据"}</p>
      {stills.length ? <div className="example-evidence-grid">{stills.slice(0, 6).map((frame, index) => <button type="button" key={frame.id} onClick={(event) => onOpenFrame(stills, index, event.currentTarget)}><img src={frame.urls.thumbnail || frame.urls.full || ""} alt={`视频 ${formatClock(frame.time_ms)} 的画面`} /><span>{formatClock(frame.time_ms)} <span aria-hidden="true">↗</span></span></button>)}</div>
        : <div className="example-evidence-placeholder"><span className="example-lens-outline" aria-hidden="true">◉</span><p>视频里的画面，<br />会在取证完成后出现在这里。</p></div>}
      {replay.complete ? <div className="example-process-result"><h3>{data.article.title}</h3><p>{data.article.sections.length} 个章节 · {data.article.sections.flatMap((item) => item.blocks).filter((block) => block.type === "figure").length} 组配图</p><button type="button" className="example-text-button" onClick={onRead}>阅读完整图文 <span aria-hidden="true">↗</span></button></div> : null}
    </div></div>
  </section>
}

function SavedQuestion({ data, question, frames, onOpenFrame, onCite }: { data: ExampleData; question: ExampleQuestion; frames: Map<string, Frame>; onOpenFrame: OpenFrame; onCite: (citation: Citation) => void }) {
  const replay = useReplay(question.steps, true)
  const frame = question.context_frame_id ? frames.get(question.context_frame_id) : null
  return <article className="sheet example-saved-question"><header className="example-saved-head"><p className="eyebrow">{question.label} <span> / 已保存的真实问答</span></p><span>{new Date(question.recorded_at).toLocaleDateString("zh-Hans")}</span></header>
    <h2 className="example-saved-question-title">{question.question}</h2>
    {frame ? <button type="button" className="example-question-frame" onClick={(event) => onOpenFrame([frame], 0, event.currentTarget)}><img src={frame.urls.thumbnail || frame.urls.full || ""} alt="这次提问选中的视频画面" /><span><strong>从这张画面问起</strong><span>{formatClock(frame.time_ms)} · 点击放大 ↗</span></span></button> : null}
    <div className="example-qa-replay"><ReplayControls replay={replay} label="播放问答过程" /><p>答案来自真实模型调用；这里重放已保存的过程与结果。</p></div>
    {!replay.complete ? <div className="example-question-progress"><p className="example-process-status" role="status">{replay.playing ? "正在回放这次问答" : replay.position > 0 ? "回放已暂停" : "准备播放"}</p>{replay.visible.length ? <ActivityList steps={replay.visible} resourceStatus="running" detailed open onOpenFrame={onOpenFrame} onBroken={() => {}} /> : <p className="meta">{question.steps.length ? "点击播放，回看这次问答保存的处理步骤。" : "这份早期记录保留了完整问答，没有保存中间步骤。回放结束后展示原始答案。"}</p>}</div>
      : <div className="example-saved-answer"><div className="example-answer-label"><span>回答</span><span>{question.duration_ms > 0 ? `本次实际用时 ${Math.round(question.duration_ms / 1000)} 秒` : ""}</span></div><ModelIdentity model={question.generation_model || data.generation_model} showHistoryNote={false} />
        <div className="prose">{question.answer.blocks.map((block) => <BlockView key={block.id} block={block} sectionId={block.id} frames={frames} citations={question.answer.citations} onOpenFrame={onOpenFrame} onAskFrame={() => {}} onCite={onCite} onBroken={() => {}} readOnly />)}{question.answer.limitations.map((item, index) => <p className="history-note" key={index}>{item.message}</p>)}</div>
        {question.steps.length ? <details className="example-answer-history"><summary>查看这次问答的 {question.steps.length} 个真实步骤</summary><ActivityList steps={question.steps} resourceStatus="completed" detailed open onOpenFrame={onOpenFrame} onBroken={() => {}} /></details> : <p className="example-quality-note">这份早期记录未保存中间步骤，以上为当时已发布的完整答案。</p>}
      </div>}
  </article>
}
