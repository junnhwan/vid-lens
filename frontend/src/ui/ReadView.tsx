import { useEffect, useRef, useState, type Ref } from "react"
import type { ActivityStep, ArticleResponse, Citation, Frame, RequestView, TurnView } from "../contract/types"
import { formatDuration, platformLabel, type PassageChoice } from "../contract/text"
import { prefersReduced } from "./motion"
import { ActivityList } from "./ActivityList"
import { BlockView } from "./Blocks"
import { MindMap } from "./MindMap"
import { QaPanel } from "./QaPanel"
import { ExportControl } from "./ExportControl"
import { ShareControl } from "./ShareControl"
import { ModelIdentity } from "./ModelIdentity"
import { ExecutionPlan } from "./ExecutionPlan"
import { ArticleOutline } from "./ArticleOutline"
import { ArticleReviewPanel } from "./ArticleReviewPanel"
import { ReadingDialog } from "./ReadingDialog"
import { ArticleGallery } from "./ArticleGallery"
import { articleImages } from "./articleImages"
import { useReadingLayout } from "./useReadingLayout"

export function ReadView(props: {
  view: RequestView | null
  article: ArticleResponse | null
  articleError: string
  steps: ActivityStep[]
  frames: Map<string, Frame>
  qaOpen: boolean
  turns: TurnView[]
  stepsFor: (turn: TurnView) => ActivityStep[]
  draft: string
  frameChip: { id: string; label: string } | null
  passageChip: PassageChoice | null
  prompts: string[]
  qaBusy: boolean
  qaError: string
  expanded: Record<string, boolean | undefined>
  focusToken: number
  active: boolean
  preview?: boolean
  csrf: string
  viewRef?: Ref<HTMLElement>
  onToggleQa: () => void
  onLoadHistory: () => void
  historyState?: Record<string, string>
  onRetryArticle: () => void
  onOpenFrame: (frames: Frame[], index: number, origin: HTMLElement) => void
  onAskFrame: (frame: Frame) => void
  onCite: (citation: Citation) => void
  onBroken: (frame: Frame) => void
  onDraft: (value: string) => void
  onSend: () => void
  onStop: () => void
  onRetryTurn: (turn: TurnView) => void
  onCloseQa: () => void
  onToggleTurn: (id: string, open: boolean) => void
  onSuggest: (prompt: string) => void
  onRemoveFrame: () => void
  onRemovePassage: () => void
  externalAllowed: boolean
  externalAvailable: boolean
  searchAvailable: boolean
  referenceLinks: string
  onExternal: (v: boolean) => void
  onReferenceLinks: (v: string) => void
}) {
  const [genOpen, setGenOpen] = useState(false)
  const [current, setCurrent] = useState("article-top")
  const [locked, setLocked] = useState<string | null>(null)
  const [arrive, setArrive] = useState(false)
  const [stage, setStage] = useState(false)
  const [paperOpen, setPaperOpen] = useState(false)
  const [progress, setProgress] = useState(0)
  const [pastTitle, setPastTitle] = useState(false)
  const seenArticle = useRef<string | null>(null)
  const colRef = useRef<HTMLDivElement>(null)
  const closePaperRef = useRef<HTMLButtonElement>(null)
  const wasPaper = useRef(false)
  const focusFromPaper = useRef(false)
  const qaSlotRef = useRef<HTMLDivElement>(null)
  const layout = !props.qaOpen ? "closed" : stage ? "stage" : "open"
  const motion = useReadingLayout(colRef, qaSlotRef, layout, props.active, props.article?.article.id)
  useEffect(() => {
    if (!props.qaOpen) {
      setStage(false)
      setPaperOpen(false)
    }
  }, [props.qaOpen])
  useEffect(() => {
    const col = colRef.current
    if (col) col.inert = stage && !paperOpen
  }, [stage, paperOpen])
  useEffect(() => {
    if (paperOpen) {
      motion.restorePaper()
      closePaperRef.current?.focus({ preventScroll: true })
    } else if (wasPaper.current) document.getElementById(focusFromPaper.current ? "composer" : "qa-open-article")?.focus({ preventScroll: true })
    focusFromPaper.current = false
    wasPaper.current = paperOpen
  }, [paperOpen])
  useEffect(() => {
    if (!paperOpen) return
    motion.rememberPaper()
    focusFromPaper.current = true
    setPaperOpen(false)
  }, [props.focusToken])
  useEffect(() => {
    if (!stage || !paperOpen) return
    function onKey(event: KeyboardEvent) {
      if (document.querySelector("dialog[open]")) return
      if (event.key === "Escape") {
        if (document.querySelector(".choice-menu")) return
        event.preventDefault()
        closePaper()
        return
      }
      if (event.key !== "Tab") return
      const col = colRef.current
      if (!col) return
      const nodes = [...col.querySelectorAll<HTMLElement>("a[href], button, input, textarea, select, [tabindex]")].filter(
        (node) => !node.hasAttribute("disabled") && node.tabIndex >= 0 && node.getClientRects().length > 0,
      )
      if (!nodes.length) return
      const first = nodes[0]
      const last = nodes[nodes.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [stage, paperOpen])
  function closePaper() {
    motion.rememberPaper()
    setPaperOpen(false)
  }
  const article = props.article?.article
  const gallery = articleImages(article?.sections ?? [], props.frames)
  const imageCounts = Object.fromEntries(gallery.map((group) => [group.sectionId, group.images.length]))
  const firstFigure = article?.sections.flatMap((section) => section.blocks).find((block) => block.type === "figure")?.id
  const meta = [platformLabel(article?.source.platform), formatDuration(article?.source.duration_ms), article?.source.part?.label]
    .filter(Boolean)
    .join(" · ")

  useEffect(() => {
    if (!props.active || !article) return
    const onScroll = () => {
      const sheet = colRef.current?.querySelector(".sheet")
      const title = document.getElementById("article-top")
      if (sheet instanceof HTMLElement) {
        const start = sheet.getBoundingClientRect().top + window.scrollY
        const total = Math.max(sheet.offsetHeight - window.innerHeight, 1)
        const scrolled = window.scrollY - start + 72
        setProgress(Math.min(100, Math.max(0, (scrolled / total) * 100)))
      }
      setPastTitle(!!title && title.getBoundingClientRect().bottom < 72)
    }
    onScroll()
    window.addEventListener("scroll", onScroll, { passive: true })
    window.addEventListener("resize", onScroll)
    return () => {
      window.removeEventListener("scroll", onScroll)
      window.removeEventListener("resize", onScroll)
    }
  }, [props.active, article?.id])

  useEffect(() => {
    if (!props.active || !article || prefersReduced()) return
    if (seenArticle.current === article.id) return
    seenArticle.current = article.id
    setArrive(true)
    const timer = window.setTimeout(() => setArrive(false), 280)
    return () => window.clearTimeout(timer)
  }, [props.active, article?.id])

  useEffect(() => {
    if (!article) return
    const nodes = article.sections.map((section) => document.getElementById(section.id)).filter((node): node is HTMLElement => !!node)
    const observer = new IntersectionObserver(
      (entries) => {
        if (locked) return
        const visible = entries.filter((entry) => entry.isIntersecting).sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0]
        if (visible) setCurrent(visible.target.id)
      },
      { rootMargin: "-12% 0px -55% 0px", threshold: [0.15, 0.4, 0.7] },
    )
    for (const node of nodes) observer.observe(node)
    return () => observer.disconnect()
  }, [article, locked])

  function jump(id: string, blockId?: string) {
    const target = blockId ? [...(colRef.current?.querySelectorAll<HTMLElement>("[data-block-id]") ?? [])].find((node) => node.dataset.blockId === blockId) : document.getElementById(id)
    if (!target) return
    setLocked(id)
    setCurrent(id)
    target.scrollIntoView({
      behavior: prefersReduced() ? "auto" : "smooth",
      block: "start",
    })
    if (target instanceof HTMLElement) {
      if (!target.hasAttribute("tabindex")) target.tabIndex = -1
      target.focus({ preventScroll: true })
    }
    window.setTimeout(() => setLocked((value) => (value === id ? null : value)), 900)
  }

  return (
    <section className="view-read" ref={props.viewRef}>
      {props.active && article ? (
        <>
          <div className="read-progress" aria-hidden="true">
            <span style={{ width: `${progress}%` }} />
          </div>
          <p className="read-chapter" data-on={pastTitle && current !== "article-top" ? "true" : "false"}>
            {article.sections.find((section) => section.id === current)?.title || ""}
          </p>
        </>
      ) : null}
      <div className="workspace" data-qa={layout} data-paper={paperOpen ? "open" : "closed"} style={motion.style}>
        {stage && !paperOpen ? (
          <button type="button" className="paper-tab" onClick={() => setPaperOpen(true)}>
            图文
          </button>
        ) : null}
        <div
          className="article-col"
          ref={colRef}
          onClick={() => {
            if (stage && paperOpen) closePaper()
          }}
        >
          <article
            className="sheet"
            aria-readonly="true"
            role={stage && paperOpen ? "dialog" : undefined}
            aria-modal={stage && paperOpen ? true : undefined}
            aria-label={stage && paperOpen ? article?.title || "图文" : undefined}
            onClick={(event) => event.stopPropagation()}
          >
            {stage && paperOpen ? (
              <div className="paper-bar">
                <button type="button" className="paper-close" ref={closePaperRef} onClick={closePaper}>
                  关闭图文
                </button>
              </div>
            ) : null}
            <header className="article-head">
              <h1 id="article-top" tabIndex={-1}>
                {article?.title || "正在读取文章"}
              </h1>
              <div className="article-tools">
                <p className="meta">{meta}</p>
                {!props.preview ? <ShareControl requestId={props.view?.request.id} csrf={props.csrf} /> : null}
                {props.preview ? (
                  <small className="meta">开发预览不生成导出</small>
                ) : (
                  <ExportControl requestId={props.view?.request.id} csrf={props.csrf} />
                )}
                <ArticleGallery key={article?.id} groups={gallery} active={props.active} onOpenFrame={props.onOpenFrame} onLocate={jump} onBroken={props.onBroken} />
                <button type="button" className="disclosure" aria-haspopup="dialog" onClick={() => { props.onLoadHistory(); setGenOpen(true) }}>
                  查看生成过程
                </button>
                <button type="button" id="toggle-qa" className="btn toggle" aria-pressed={props.qaOpen} onClick={(event) => { motion.prepare(event.detail === 0); props.onToggleQa() }}>
                  问这条视频
                </button>
              </div>
              <ModelIdentity
                model={props.article?.generation_model || props.view?.request.generation_model}
                purpose={props.article?.purpose || props.view?.request.purpose}
                detailLevel={props.article?.detail_level || props.view?.request.detail_level}
              />
            </header>
            <ReadingDialog open={props.active && genOpen} title="生成过程" onClose={() => setGenOpen(false)} className="generation-dialog">
                <ExecutionPlan activity={props.view?.activity} completed={props.view?.request.status === "completed"} />
                {props.view && props.historyState?.[props.view.request.id] ? <p className="history-note" role="status">{props.historyState[props.view.request.id]}</p> : null}
                <ActivityList
                  steps={props.steps}
                  resourceStatus={props.view?.request.status || "completed"}
                  detailed
                  open
                  onOpenFrame={props.onOpenFrame}
                  onBroken={props.onBroken}
                />
                {props.view?.activity.history_status === "expired" ? (
                  <p className="history-note">更早的过程已经过期，文章仍然可以读。</p>
                ) : null}
                {props.view?.request.reuse === "published_article" ? (
                  <p className="history-note">这次直接打开了已有文章，没有新的生成过程。</p>
                ) : null}
            </ReadingDialog>
            {article?.mind_map ? <MindMap key={article.id} map={article.mind_map} current={current} onJump={jump} /> : null}
            <div className="article-reading">
              {article ? <ArticleOutline key={article.id} sections={article.sections} current={current} onJump={jump} imageCounts={imageCounts} /> : null}
              <div className="prose" id="prose" data-arrive={arrive ? "true" : undefined}>
                {props.article && !props.article.media.can_inspect_now ? (
                  <p className="history-note">
                    {props.article.media.can_restore_for_question
                      ? "现在不能直接查看新的画面。提问时可以再准备这条视频。"
                      : "现在不能查看新的画面。已有文章仍然可以读。"}
                  </p>
                ) : null}
                {props.articleError ? (
                  <p className="form-error">
                    {props.articleError}{" "}
                    <button type="button" className="btn quiet" onClick={props.onRetryArticle}>
                      重新读取
                    </button>
                  </p>
                ) : null}
                {article?.sections.map((section, index) => (
                  <section key={section.id} style={arrive ? { animationDelay: `${Math.min(index, 5) * 48}ms` } : undefined}>
                    <h2 id={section.id} tabIndex={-1}>
                      <span className="chapter-no" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
                      <span className="article-chapter-title">{section.title}</span>
                    </h2>
                    {section.blocks.map((block) => (
                      <BlockView
                        key={block.id}
                        block={block}
                        sectionId={section.id}
                        carry={block.id === firstFigure}
                        citations={article.citations ?? {}}
                        frames={props.frames}
                        onOpenFrame={props.onOpenFrame}
                        onAskFrame={props.onAskFrame}
                        onCite={props.onCite}
                        onBroken={props.onBroken}
                      />
                    ))}
                  </section>
                ))}
                {(article?.limitations ?? []).length ? (
                  <div className="limits">
                    <h2>还没确认的部分</h2>
                    <ul>
                      {(article?.limitations ?? []).map((item) => (
                        <li key={`${item.code}-${item.message}`}>
                          {item.message}
                          {(item.section_ids ?? []).map((id) => (
                            <button type="button" className="time" key={id} onClick={() => jump(id)}>
                              定位
                            </button>
                          ))}
                        </li>
                      ))}
                    </ul>
                  </div>
                ) : null}
                {article && props.view?.request.id && !props.preview ? <ArticleReviewPanel key={article.id} requestId={props.view.request.id} csrf={props.csrf} onJump={jump} /> : null}
              </div>
            </div>
          </article>
        </div>
        <div className="qa-slot" ref={qaSlotRef}>
          <QaPanel
            open={props.qaOpen}
            turns={props.turns}
            stepsFor={props.stepsFor}
            frames={props.frames}
            draft={props.draft}
            frameChip={props.frameChip}
            passageChip={props.passageChip}
            prompts={props.prompts}
            busy={props.qaBusy}
            error={props.qaError}
            expanded={props.expanded}
            focusToken={props.focusToken}
            onDraft={props.onDraft}
            onSend={props.onSend}
            onStop={props.onStop}
            onRetry={props.onRetryTurn}
            onClose={() => { motion.prepare(); props.onCloseQa() }}
            onToggle={props.onToggleTurn}
            historyState={props.historyState}
            onSuggest={props.onSuggest}
            onRemoveFrame={props.onRemoveFrame}
            onRemovePassage={props.onRemovePassage}
            onOpenFrame={props.onOpenFrame}
            onCite={props.onCite}
            onBroken={props.onBroken}
            externalAllowed={props.externalAllowed}
            externalAvailable={props.externalAvailable}
            searchAvailable={props.searchAvailable}
            referenceLinks={props.referenceLinks}
            onExternal={props.onExternal}
            onReferenceLinks={props.onReferenceLinks}
            modelUnavailable={props.article?.qa_available === false}
            staged={stage}
            onStage={() => {
              motion.prepare(document.activeElement?.matches(":focus-visible"))
              setPaperOpen(false)
              setStage(true)
            }}
            onPair={() => {
              motion.prepare(document.activeElement?.matches(":focus-visible"))
              setPaperOpen(false)
              setStage(false)
            }}
            onOpenArticle={() => setPaperOpen(true)}
          />
        </div>
      </div>
    </section>
  )
}
