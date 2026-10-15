import { useEffect, useRef, useState, type Ref } from "react"
import type { ActivityStep, ArticleResponse, Citation, Frame, RequestView, TurnView } from "../contract/types"
import { formatDuration, platformLabel } from "../contract/text"
import { prefersReduced } from "./motion"
import { ActivityList } from "./ActivityList"
import { BlockView } from "./Blocks"
import { MindMap } from "./MindMap"
import { QaPanel } from "./QaPanel"

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
  passageChip: { sectionId: string; blockId: string; quote: string } | null
  suggesting: string
  qaBusy: boolean
  qaError: string
  expanded: Record<string, boolean | undefined>
  focusToken: number
  active: boolean
  viewRef?: Ref<HTMLElement>
  onToggleQa: () => void
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
  onSuggest: () => void
  onRemoveFrame: () => void
  onRemovePassage: () => void
}) {
  const [genOpen, setGenOpen] = useState(false)
  const [current, setCurrent] = useState("article-top")
  const [locked, setLocked] = useState<string | null>(null)
  const [arrive, setArrive] = useState(false)
  const seenArticle = useRef<string | null>(null)
  const article = props.article?.article
  const firstFigure = article?.sections.flatMap((section) => section.blocks).find((block) => block.type === "figure")?.id
  const meta = [platformLabel(article?.source.platform), formatDuration(article?.source.duration_ms), article?.source.part?.label].filter(Boolean).join(" · ")

  useEffect(() => {
    if (!props.active || !article || prefersReduced()) return
    if (seenArticle.current === article.id) return
    seenArticle.current = article.id
    setArrive(true)
    const timer = window.setTimeout(() => setArrive(false), 720)
    return () => window.clearTimeout(timer)
  }, [props.active, article?.id])

  useEffect(() => {
    if (!article) return
    const nodes = article.sections.map((section) => document.getElementById(section.id)).filter((node): node is HTMLElement => !!node)
    const observer = new IntersectionObserver((entries) => {
      if (locked) return
      const visible = entries.filter((entry) => entry.isIntersecting).sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0]
      if (visible) setCurrent(visible.target.id)
    }, { rootMargin: "-12% 0px -55% 0px", threshold: [0.15, 0.4, 0.7] })
    for (const node of nodes) observer.observe(node)
    return () => observer.disconnect()
  }, [article, locked])

  function jump(id: string) {
    const target = document.getElementById(id)
    if (!target) return
    setLocked(id)
    setCurrent(id)
    target.scrollIntoView({ behavior: prefersReduced() ? "auto" : "smooth", block: "start" })
    if (target instanceof HTMLElement) target.focus({ preventScroll: true })
    window.setTimeout(() => setLocked((value) => (value === id ? null : value)), 900)
  }

  return (
    <section className="view-read" ref={props.viewRef}>
      <div className="workspace" data-qa={props.qaOpen ? "open" : "closed"}>
        <div className="article-col">
          <article className="sheet" aria-readonly="true">
            <header className="article-head">
              <h1 id="article-top" tabIndex={-1}>{article?.title || "正在读取文章"}</h1>
              <div className="article-tools">
                <p className="meta">{meta}</p>
                <button type="button" className="disclosure" aria-expanded={genOpen} onClick={() => setGenOpen((value) => !value)}>{genOpen ? "收起生成过程" : "查看生成过程"}</button>
                <button type="button" id="toggle-qa" className="btn toggle" aria-pressed={props.qaOpen} onClick={props.onToggleQa}>问这条视频</button>
              </div>
            </header>
            <div className="disclose" data-open={genOpen ? "true" : "false"}>
              <div className="disclose-inner">
                <ActivityList
                  steps={props.steps}
                  resourceStatus={props.view?.request.status || "completed"}
                  detailed
                  open
                  onOpenFrame={props.onOpenFrame}
                  onBroken={props.onBroken}
                />
                {props.view?.activity.history_status === "expired" ? <p className="history-note">更早的过程已经过期，文章仍然可以读。</p> : null}
                {props.view?.activity.history_status === "none" && props.view.request.reuse === "published_article" ? <p className="history-note">这次直接打开了已有文章，没有新的生成过程。</p> : null}
              </div>
            </div>
            {article?.mind_map ? <MindMap map={article.mind_map} current={current} onJump={jump} /> : null}
            <div className="prose" id="prose" data-arrive={arrive ? "true" : undefined}>
              {props.article && !props.article.media.can_inspect_now ? (
                <p className="history-note">
                  {props.article.media.can_restore_for_question
                    ? "现在不能直接查看新的画面。提问时可以再准备这条视频。"
                    : "现在不能查看新的画面。已有文章仍然可以读。"}
                </p>
              ) : null}
              {props.articleError ? <p className="form-error">{props.articleError} <button type="button" className="btn quiet" onClick={props.onRetryArticle}>重新读取</button></p> : null}
              {article?.sections.map((section, index) => (
                <section key={section.id} style={arrive ? { animationDelay: `${Math.min(index, 5) * 48}ms` } : undefined}>
                  <h2 id={section.id} tabIndex={-1}>{section.title}</h2>
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
                          <button type="button" className="time" key={id} onClick={() => jump(id)}>定位</button>
                        ))}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </div>
          </article>
        </div>
        <div className="qa-slot">
          <QaPanel
            open={props.qaOpen}
            turns={props.turns}
            stepsFor={props.stepsFor}
            frames={props.frames}
            draft={props.draft}
            frameChip={props.frameChip}
            passageChip={props.passageChip}
            suggesting={props.suggesting}
            busy={props.qaBusy}
            error={props.qaError}
            expanded={props.expanded}
            focusToken={props.focusToken}
            onDraft={props.onDraft}
            onSend={props.onSend}
            onStop={props.onStop}
            onRetry={props.onRetryTurn}
            onClose={props.onCloseQa}
            onToggle={props.onToggleTurn}
            onSuggest={props.onSuggest}
            onRemoveFrame={props.onRemoveFrame}
            onRemovePassage={props.onRemovePassage}
            onOpenFrame={props.onOpenFrame}
            onCite={props.onCite}
            onBroken={props.onBroken}
          />
        </div>
      </div>
    </section>
  )
}
